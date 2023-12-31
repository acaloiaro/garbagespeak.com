package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"net/smtp"
	"os"
	"strconv"
	"strings"
	"text/template"
	"time"

	"github.com/acaloiaro/garbage_speak/html_parser"
	"github.com/acaloiaro/neoq"
	"github.com/acaloiaro/neoq/backends/postgres"
	"github.com/acaloiaro/neoq/handler"
	"github.com/acaloiaro/neoq/jobs"
	"github.com/alexedwards/scs/pgxstore"
	"github.com/alexedwards/scs/v2"
	"github.com/georgysavva/scany/v2/pgxscan"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/gofrs/uuid"
	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	pgxuuid "github.com/jackc/pgx-gofrs-uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	gmhtml "github.com/yuin/goldmark/renderer/html"

	"golang.org/x/crypto/bcrypt"
)

// Garbage represents 'garbage' records from the database
type Garbage struct {
	ID              uuid.UUID
	OwnerID         uuid.UUID
	Username        string
	Title           string
	Content         string  // the raw, user-supplied content
	RenderedContent *string // the content run through goldmark
	Metadata        map[string]any
	Url             string
	CreatedAt       time.Time
	N               int
}

// User represents 'user' records from the database
type User struct {
	ID           uuid.UUID
	Username     string
	PasswordHash string
	Email        string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// UserEmailVerification models pending user email verifications. If a User has a UserEmailVeritifcation, the account is
// pending verification and is not eligible to log in
type UserEmailVerification struct {
	ID        uuid.UUID
	User      *User
	UserID    uuid.UUID
	CreatedAt time.Time
	UpdatedAt time.Time
}

//go:embed migrations/*.sql
var migrationsFS embed.FS

//go:embed all:public
var publicFS embed.FS

//go:embed all:partials
var partialsFS embed.FS

var (
	md           goldmark.Markdown
	db           *pgxpool.Pool
	sessions     *scs.SessionManager
	sessionStore *pgxstore.PostgresStore
	NQ           neoq.Neoq
	pageSize     = 25
)

// run migrations, acquire a database connection pool, and create the session store
func init() {
	d, err := iofs.New(migrationsFS, "migrations")
	if err != nil {
		log.Fatal(err)
	}

	m, err := migrate.NewWithSourceInstance("iofs", d, os.Getenv("POSTGRES_URL"))
	if err != nil {
		log.Fatal(err)
	}

	err = m.Up()
	if err != nil {
		fmt.Fprintf(os.Stderr, "migrations: %v\n", err)
	}

	dbconfig, err := pgxpool.ParseConfig(os.Getenv("POSTGRES_URL"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Unable to configure database: %v\n", err)
		os.Exit(1)
	}
	dbconfig.AfterConnect = func(_ context.Context, conn *pgx.Conn) error {
		pgxuuid.Register(conn.TypeMap())
		return nil
	}

	db, err = pgxpool.NewWithConfig(context.Background(), dbconfig)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Unable to connect to database: %v\n", err)
		os.Exit(1)
	}

	sessions = scs.New()
	sessions.Lifetime = time.Duration(24 * time.Hour * 365)

	sessionStore = pgxstore.New(db)

	sessions.Cookie.Name = "session_id"
	sessions.Cookie.HttpOnly = true
	sessions.Cookie.Persist = true
	sessions.Cookie.SameSite = http.SameSiteNoneMode
	sessions.Cookie.Secure = true
	sessions.Store = sessionStore

	ctx := context.Background()
	NQ, err = neoq.New(ctx,
		neoq.WithBackend(postgres.Backend),
		postgres.WithConnectionString(os.Getenv("POSTGRES_URL")),
		postgres.WithTransactionTimeout(1000*60*5),
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Unable to initialize background worker: %v\n", err)
		os.Exit(1)
	}

	err = NQ.Start(ctx, "welcome_email", handler.New(welcomeEmailHandler))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Unable to initialize welcome email handler: %v\n", err)
		os.Exit(1)
	}

	md = goldmark.New(
		goldmark.WithExtensions(extension.GFM),
		goldmark.WithParserOptions(
			parser.WithAutoHeadingID(),
		),
		goldmark.WithRendererOptions(
			gmhtml.WithHardWraps(),
		),
	)
}

func main() {
	defer db.Close()
	defer sessionStore.StopCleanup()
	defer NQ.Shutdown(context.Background())

	serverRoot, _ := fs.Sub(publicFS, "public")
	staticContentServer := http.FileServer(http.FS(serverRoot))

	r := chi.NewRouter()
	r.Use(sessions.LoadAndSave)
	r.Use(middleware.Logger)
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(cors.Handler(cors.Options{
		AllowCredentials: true,
		AllowedOrigins:   []string{"http://localhost:1313", fmt.Sprintf("https://%s", os.Getenv("SITE_DOMAIN"))},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Cookie", "Authorization", "Content-Type", "X-CSRF-Token", "hx-target", "hx-current-url", "hx-request", "hx-trigger", "hx-trigger-name", "hx-boosted"},
		ExposedHeaders:   []string{"Link", "HX-Location", "Vary", "Access-Control-Allow-Origin"},
		MaxAge:           300, // Maximum value not ignored by any of major browsers
	}))

	// Add any number of handlers for custom endpoints here
	r.Route("/", func(r chi.Router) {
		// any requests for which there are no defined chi routes are sent to the "file system"
		// server, serving static Hugo content
		r.NotFound(func(w http.ResponseWriter, r *http.Request) {
			staticContentServer.ServeHTTP(w, r)
		})

		r.Get("/nav/user_items", navUserItems)
		r.Route("/users", func(users chi.Router) {
			users.Post("/new_user_validation", newUserValidationHandler)
			users.Get("/create", creatAccountPageHandler)
			users.Post("/login", loginHandler)
			users.Post("/create", createAccountHandler)
			users.Get("/logout", logoutHandler)
			users.Get("/email_verification/{uev_id}", emailVerification)
		})
		r.Route("/garbage", func(garbage chi.Router) {
			garbage.Get("/list", listGarbageHandler)
			garbage.Post("/new", createGarbageHandler)
			garbage.Get("/{garbage_id}/edit", editGarbageHandler)
			garbage.Put("/{garbage_id}", editGarbageUpdateHandler)
			garbage.Get("/{garbage_id}", showGarbageHandler)
			garbage.Put("/{garbage_id}/uplevel", addUplevelHandler)
			garbage.Get("/{garbage_id}/uplevel", getUplevelHandler)
		})
	})

	// this environment variable is present in production
	// if the name of the port in job.nomad.hcl changes, this port
	// name will need to change as well
	port := os.Getenv("NOMAD_HOST_PORT_garbage_speak")
	if port == "" {
		port = "1314"
	}

	addr := fmt.Sprintf("%s:%s", "0.0.0.0", port)

	fmt.Println("Starting API server on", addr)
	if err := http.ListenAndServe(addr, r); err != nil {
		log.Fatal(err)
	}
}

func getUplevelHandler(w http.ResponseWriter, r *http.Request) {
	garbageID := chi.URLParam(r, "garbage_id")

	var uplevel int
	db.QueryRow(r.Context(), "SELECT COUNT(*) FROM uplevels WHERE garbage_id = $1", garbageID).Scan(&uplevel)

	w.Write([]byte(strconv.Itoa(uplevel)))
	w.WriteHeader(http.StatusOK)
}

func addUplevelHandler(w http.ResponseWriter, r *http.Request) {
	garbageID := chi.URLParam(r, "garbage_id")
	userID := sessions.GetString(r.Context(), "userID")

	if userID == "" {
		ise(errors.New("not logged in"), w)
		return
	}

	ctx := context.Background()
	_, err := db.Exec(ctx,
		"INSERT INTO uplevels(garbage_id, user_id) VALUES ($1, $2)",
		garbageID,
		userID)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) {
			if pgErr.Code == "23505" { // duplicate key error
				goto render
			}
		}
		ise(err, w)
		return
	}

render:
	garbageUUID, _ := uuid.FromString(garbageID)
	garbage := Garbage{ID: garbageUUID}
	tmpl := template.Must(template.ParseFS(partialsFS, "partials/garbage/uplevel_button.tmpl"))
	err = tmpl.ExecuteTemplate(w, "uplevel_button.tmpl", map[string]any{
		"Garbage":    garbage,
		"ApiBaseUrl": apiURL(),
		"UserID":     userID,
	})
	if err != nil {
		ise(err, w)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func editGarbageUpdateHandler(w http.ResponseWriter, r *http.Request) {
	garbageID := chi.URLParam(r, "garbage_id")
	userID := sessions.GetString(r.Context(), "userID")

	if userID == "" {
		ise(errors.New("not logged in"), w)
		return
	}

	if err := r.ParseForm(); err != nil {
		ise(err, w)
		return
	}

	url := r.PostForm.Get("url")
	title := r.PostForm.Get("title")
	content := r.PostForm.Get("garbage")
	// TODO this is an arbitrary length that will likely need to change
	if len(content) == 10 {
		w.WriteHeader(400)
		return
	}

	renderedContent := mdToHtml(content)
	metadata := map[string]any{}
	tags := r.Form["tags"]
	if len(tags) > 0 {
		metadata["tags"] = tags
	}

	ctx := context.Background()
	_, err := db.Exec(ctx,
		"UPDATE garbages SET (title, content, rendered_content, url, metadata) = ($1, $2, $3, $4, $5) WHERE id = $6",
		title,
		content,
		renderedContent,
		url,
		metadata,
		garbageID)
	if err != nil {
		ise(err, w)
		return
	}

	w.Header().Add("hx-location", appURL())
}

func editGarbageHandler(w http.ResponseWriter, r *http.Request) {
	garbageID := chi.URLParam(r, "garbage_id")
	userID := sessions.GetString(r.Context(), "userID")

	garbage := Garbage{}
	ctx := context.Background()
	err := pgxscan.Get(
		ctx,
		db,
		&garbage,
		`SELECT id, owner_id, title, content, rendered_content, metadata, url FROM garbages WHERE id = $1 AND owner_id = $2`, garbageID, userID)
	if err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	if userID == "" || userID != garbage.OwnerID.String() {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	availableTags := []string{
		"Nouned verb",
		"Verbed noun",
		"Nouned adjective",
		"Novel garbage",
		"Standard-issue garbage",
	}
	selectedTags := map[string]bool{}
	if tags, ok := garbage.Metadata["tags"].([]interface{}); ok {
		for _, tag := range tags {
			selectedTags[tag.(string)] = true
		}
	}

	tmplVars := map[string]any{
		"ApiBaseUrl":    apiURL(),
		"Garbage":       garbage,
		"SelectedTags":  selectedTags,
		"AvailableTags": availableTags,
	}
	tmpl := template.Must(template.ParseFS(partialsFS, "partials/garbage/edit.html"))
	err = tmpl.ExecuteTemplate(w, "edit.html", tmplVars)
	if err != nil {
		ise(err, w)
		return
	}

	w.Header().Add("Content-Type", "text/html")
	w.WriteHeader(http.StatusOK)
}

// newUserValidationHandler checks whether a username is available during account creation
func newUserValidationHandler(w http.ResponseWriter, r *http.Request) {
	tmplVars := map[string]any{"ApiBaseUrl": apiURL()}
	tmpl := template.Must(template.ParseFS(partialsFS, "partials/users/new_user_validation.html"))

	errCnt := 0

	if err := r.ParseForm(); err != nil {
		ise(err, w)
		return
	}

	username := r.PostForm.Get("username")
	email := r.PostForm.Get("email")
	password := r.PostForm.Get("password")
	passwordConfirmation := r.PostForm.Get("password_confirmation")

	tmplVars["Username"] = username
	tmplVars["Email"] = email
	tmplVars["Password"] = password
	tmplVars["PasswordConfirmation"] = passwordConfirmation

	if len(username) == 0 {
		tmplVars["UsernameError"] = "Please choose a username"
		errCnt += 1
	} else if len(username) > 0 {
		var userID string
		db.QueryRow(r.Context(), "SELECT id FROM users WHERE username = $1", username).Scan(&userID)
		if userID != "" {
			tmplVars["UsernameError"] = fmt.Sprintf("Username '%s' is unavailable. Choose a different username.", username)
			errCnt += 1
		}
	}

	if (len(email) > 0 && len(email) < 4) || (len(email) >= 4 && !strings.Contains(email, "@")) {
		tmplVars["EmailError"] = "Please enter a valid email address."
		errCnt += 1
	}

	if len(password) > 0 && len(password) < 8 {
		tmplVars["PasswordError"] = "Please choose a password greater than 8 characters"
		errCnt += 1
	}

	if len(password) > 0 && len(passwordConfirmation) > 0 && password != passwordConfirmation {
		tmplVars["PasswordConfirmationError"] = "Passwords do not match"
		errCnt += 1
	}

	tmplVars["ErrorCount"] = errCnt

	err := tmpl.ExecuteTemplate(w, "new_user_validation.html", tmplVars)
	if err != nil {
		ise(err, w)
		return
	}

	w.Header().Add("Content-Type", "text/html")
	w.WriteHeader(http.StatusOK)
}

// loginHandler handles login requests and performs validation
func loginHandler(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		ise(err, w)
		return
	}

	username := r.PostForm.Get("username")
	password := r.PostForm.Get("password")

	var userID string
	var storedPasswordHash string

	// this anti-join prevents users with pending user email verifications from logging in
	antiJoinQuery := `SELECT users.id, password FROM users
LEFT JOIN user_email_verifications ON user_email_verifications.user_id = users.id
WHERE user_email_verifications.id IS NULL
AND username = $1`

	var err error
	err = db.QueryRow(r.Context(), antiJoinQuery, username).Scan(&userID, &storedPasswordHash)
	if err != nil {
		ise(err, w)
		return
	}

	if err = bcrypt.CompareHashAndPassword([]byte(storedPasswordHash), []byte(password)); err == nil {
		err = sessions.RenewToken(r.Context())
		if err != nil {
			ise(err, w)
			return
		}

		sessions.Put(r.Context(), "userID", userID)
		w.Header().Add("hx-location", appURL())

		return
	}

	tmpl := template.Must(template.ParseFS(partialsFS, "partials/users/login_validation.html"))
	err = tmpl.ExecuteTemplate(w, "login_validation.html", map[string]any{
		"ApiBaseURL": apiURL(),
		"LoginError": "Incorrect username or password",
		"Username":   username,
		"Password":   password,
	})
	if err != nil {
		ise(err, w)
		return
	}

	w.Header().Add("Content-Type", "text/html")
	w.WriteHeader(200)
}

// logoutHandler handles users logout requests
func logoutHandler(w http.ResponseWriter, r *http.Request) {
	sessions.Destroy(r.Context())

	w.Header().Add("hx-location", appURL())
	w.WriteHeader(http.StatusOK)
}

// navUserItems returns a nav items depending on whether the user is logged in
func navUserItems(w http.ResponseWriter, r *http.Request) {
	var tmpl *template.Template

	var err error
	tmpl = template.Must(template.ParseFS(partialsFS, "partials/nav/*.html"))
	if isLoggedIn(r) {
		err = tmpl.ExecuteTemplate(w, "user_nav_items.html", map[string]any{"ApiURL": apiURL()})
	} else {
		err = tmpl.ExecuteTemplate(w, "non_user_nav_items.html", map[string]any{"ApiURL": apiURL()})
	}

	if err != nil {
		ise(err, w)
		return
	}

	w.Header().Add("Content-Type", "text/html")
	w.WriteHeader(http.StatusOK)
}

// creatAccountPageHandler serves the new account form
func creatAccountPageHandler(w http.ResponseWriter, r *http.Request) {
	tmpl := template.Must(template.ParseFS(partialsFS, "partials/users/*"))
	err := tmpl.ExecuteTemplate(w, "create.html", map[string]any{})
	if err != nil {
		ise(err, w)
		return
	}

	w.Header().Add("Content-Type", "text/html")
	w.WriteHeader(http.StatusOK)
}

// emailVerification takes a UserEmailVerification ID, and if it exists, verifies the associated User account by
// deleting the UEV record (user login's check if a UEV exists for a User before permitting login)
func emailVerification(w http.ResponseWriter, r *http.Request) {
	// user email verification id
	uevID := chi.URLParam(r, "uev_id")

	tx, err := db.Begin(r.Context())
	if err != nil {
		ise(err, w)
		return
	}
	// Rollback is safe to call even if the tx is already closed, so if
	// the tx commits successfully, this is a no-op
	defer tx.Rollback(r.Context())

	var userID string
	err = tx.QueryRow(r.Context(), "DELETE FROM user_email_verifications WHERE id = $1 RETURNING user_id", uevID).Scan(&userID)
	if err != nil {
		goto render
	}

	err = sessions.RenewToken(r.Context())
	if err != nil {
		ise(err, w)
		return
	}

	// create the user's session
	sessions.Put(r.Context(), "userID", userID)

	tx.Commit(r.Context())

render:
	http.Redirect(w, r, appURL(), http.StatusFound)
}

func mdToHtml(markdown string) (html string) {
	var buf bytes.Buffer
	if err := md.Convert([]byte(markdown), &buf); err != nil {
		panic(err)
	}

	html = buf.String()
	return
}

func createGarbageHandler(w http.ResponseWriter, r *http.Request) {
	if !isLoggedIn(r) {
		w.Header().Add("hx-location", fmt.Sprintf("%s/users/login", appURL()))
		return
	}

	userID := sessions.GetString(r.Context(), "userID")
	if userID == "" {
		ise(errors.New("not logged in"), w)
		return
	}

	if err := r.ParseForm(); err != nil {
		ise(err, w)
		return
	}

	title := r.PostForm.Get("title")
	content := r.PostForm.Get("garbage")
	// TODO this is an arbitrary length that will likely need to change
	if len(content) == 10 {
		w.WriteHeader(400)
		return
	}

	renderedContent := mdToHtml(content)
	url := r.PostForm.Get("url")

	metadata := map[string]any{}
	tags := r.Form["tags"]
	if len(tags) > 0 {
		metadata["tags"] = tags
	}

	ctx := context.Background()
	db.QueryRow(ctx,
		"INSERT INTO garbages(title, content, rendered_content, url, metadata, owner_id) VALUES ($1, $2, $3, $4, $5, $6)",
		title,
		content,
		renderedContent,
		url,
		metadata,
		userID)

	w.Header().Add("hx-location", appURL())
}

// createAccountHandler creates new accounts
func createAccountHandler(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		ise(err, w)
		return
	}

	username := r.PostForm.Get("username")
	if len(username) == 0 {
		w.WriteHeader(400)
		return
	}

	password := r.PostForm.Get("password")
	if len(password) < 8 {
		w.WriteHeader(400)
		return
	}

	email := r.PostForm.Get("email")
	if !strings.Contains(email, "@") {
		w.WriteHeader(400)
		return
	}

	passwordHash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		ise(err, w)
		return
	}

	ctx := context.Background()
	tx, err := db.Begin(ctx)
	if err != nil {
		ise(err, w)
		return
	}
	// Rollback is safe to call even if the tx is already closed, so if
	// the tx commits successfully, this is a no-op
	defer tx.Rollback(ctx)

	var userID string
	err = tx.QueryRow(ctx, "insert into users(username, password, email) values ($1, $2, $3) returning id", username, passwordHash, email).Scan(&userID)
	if err != nil {
		ise(err, w)
		return
	}

	var uevID string
	err = tx.QueryRow(ctx, "insert into user_email_verifications(user_id) values ($1) returning id", userID).Scan(&uevID)
	if err != nil {
		ise(err, w)
		return
	}

	err = tx.Commit(ctx)
	if err != nil {
		ise(err, w)
		return
	}

	_, err = NQ.Enqueue(ctx, &jobs.Job{
		Queue: "welcome_email",
		Payload: map[string]interface{}{
			"recipient":        email,
			"verification_url": fmt.Sprintf("%s/users/email_verification/%s", apiURL(), uevID),
		},
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "unable to queue email veritifcation: %v", err)
		return
	}

	tmpl := template.Must(template.ParseFS(partialsFS, "partials/users/*"))
	err = tmpl.ExecuteTemplate(w, "created.html", map[string]any{"Email": email})
	if err != nil {
		ise(err, w)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// argsfn is a template function to pass arbitrary template variables into sub-templates
func argsfn(kvs ...interface{}) (map[string]interface{}, error) {
	if len(kvs)%2 != 0 {
		return nil, errors.New("args requires even number of arguments")
	}
	m := make(map[string]interface{})
	for i := 0; i < len(kvs); i += 2 {
		s, ok := kvs[i].(string)
		if !ok {
			return nil, errors.New("even args to args must be strings")
		}
		m[s] = kvs[i+1]
	}
	return m, nil
}

// pagedQuery returns a paged query for the given query
func pagedQuery(r *http.Request, query string) (pagedQuery string, firstItem *int) {
	firstItemStr := r.URL.Query().Get("first_item")

	var err error
	var fi int
	fi, err = strconv.Atoi(firstItemStr)
	if err != nil {
		pagedQuery = query + " ORDER BY n DESC LIMIT $1"
		return
	}
	firstItem = &fi

	// newer items have a larger n, since n monotonically increases
	// hence we filter where n < our first item's n
	pagedQuery = query + " WHERE n < $2"
	pagedQuery = pagedQuery + " ORDER BY n DESC LIMIT $1"

	return
}

// listGarbageHandler returns the latest garbage
func listGarbageHandler(w http.ResponseWriter, r *http.Request) {
	userID := sessions.GetString(r.Context(), "userID")

	query := `SELECT
			garbages.id, n, owner_id, username, title, rendered_content, metadata, url, garbages.created_at
			FROM garbages
			JOIN users ON garbages.owner_id = users.id`
	pagedQuery, firstItem := pagedQuery(r, query)

	ctx := context.Background()
	garbage := []*Garbage{}
	var err error
	if firstItem != nil {
		err = pgxscan.Select(ctx, db, &garbage, pagedQuery, pageSize, firstItem)
	} else {
		err = pgxscan.Select(ctx, db, &garbage, pagedQuery, pageSize)
	}

	if err != nil {
		ise(err, w)
		return
	}

	tmpl := template.Must(
		template.New("list.html").
			Funcs(template.FuncMap{"argsfn": argsfn}).
			ParseFS(partialsFS, "partials/garbage/list.html", "partials/garbage/*.tmpl"))

	// pagination
	var lastItem *int
	il := len(garbage) - 1
	if il >= 0 {
		lastItem = &(garbage[il].N)
	}

	buff := bytes.NewBufferString("")
	err = tmpl.ExecuteTemplate(buff, "list.html", map[string]any{
		"Posts":      garbage,
		"ApiBaseUrl": apiURL(),
		"LoggedIn":   isLoggedIn(r),
		"UserID":     userID,
		"LastItem":   lastItem,
	})
	if err != nil {
		ise(err, w)
		return
	}

	if isPartialRequest(r) {
		w.Write(buff.Bytes())
	} else {
		indexFile, _ := publicFS.Open("public/index.html")
		content := html_parser.ParseAndSplice(indexFile, "content", buff.String())
		w.Write([]byte(content))
	}

	w.WriteHeader(http.StatusOK)
}

func isPartialRequest(r *http.Request) bool {
	return r.Header.Get("hx-request") != ""
}

// showGarbageHandler returns the latest garbage
func showGarbageHandler(w http.ResponseWriter, r *http.Request) {
	garbageID := chi.URLParam(r, "garbage_id")
	userID := sessions.GetString(r.Context(), "userID")
	ctx := context.Background()
	garbage := Garbage{}
	err := pgxscan.Get(
		ctx,
		db,
		&garbage,
		`SELECT garbages.id, owner_id, username, title, rendered_content, metadata, url, garbages.created_at
			FROM garbages
			JOIN users ON garbages.owner_id = users.id
			WHERE garbages.id = $1`, garbageID)
	if err != nil {
		ise(err, w)
		return
	}

	buff := bytes.NewBufferString("")
	tmpl := template.Must(
		template.New("list.html").
			Funcs(template.FuncMap{"argsfn": argsfn}).
			ParseFS(partialsFS, "partials/garbage/show.tmpl", "partials/garbage/*.tmpl"))
	err = tmpl.ExecuteTemplate(buff, "show.tmpl", map[string]any{
		"Garbage":    garbage,
		"ApiBaseUrl": apiURL(),
		"LoggedIn":   isLoggedIn(r),
		"UserID":     userID,
	})
	if err != nil {
		ise(err, w)
		return
	}

	if isPartialRequest(r) {
		w.Write(buff.Bytes())
	} else {
		indexFile, _ := publicFS.Open("public/index.html")
		content := html_parser.ParseAndSplice(indexFile, "content", buff.String())
		w.Write([]byte(content))
	}

	w.WriteHeader(http.StatusOK)
}

// sendWelcomeEmail sends an email to recipient containing a special URL that only that can know, for the purpose of
// email address verification
func sendWelcomeEmail(recipient, verificationURL, siteName string) error {
	smtpHost := os.Getenv("SMTP_HOST")
	from := fmt.Sprintf("noreply@%s", os.Getenv("SITE_DOMAIN"))

	msg := []byte(fmt.Sprintf("To: %s\r\n", recipient) +
		fmt.Sprintf("From: %s", from) + "\r\n" +
		fmt.Sprintf("Subject: Welcome to %s!\r\n", siteName) + "\r\n" +
		fmt.Sprintf("Verify your email address by visiting: %s\r\n", verificationURL))

	host, _, _ := net.SplitHostPort(smtpHost)

	auth := smtp.PlainAuth("", os.Getenv("SMTP_USERNAME"), os.Getenv("SMTP_PASSWORD"), host)

	// Here is the key, you need to call tls.Dial instead of smtp.Dial
	// for smtp servers running on 465 that require an ssl connection
	// from the very beginning (no starttls)
	c, err := smtp.Dial(smtpHost)
	if err != nil {
		log.Panic(err)
	}

	// TLS config
	tlsconfig := &tls.Config{
		InsecureSkipVerify: true,
		ServerName:         host,
	}
	c.StartTLS(tlsconfig)

	// Auth
	if err = c.Auth(auth); err != nil {
		log.Panic(err)
	}

	// From
	if err = c.Mail(from); err != nil {
		log.Panic(err)
	}

	// Recipient
	if err = c.Rcpt(recipient); err != nil {
		log.Panic(err)
	}

	// Data
	w, err := c.Data()
	if err != nil {
		log.Panic(err)
	}

	_, err = w.Write(msg)
	if err != nil {
		log.Panic(err)
	}

	err = w.Close()
	if err != nil {
		log.Panic(err)
	}

	c.Quit()

	return nil
}

// welcomeEmailHandler sends a welcome email to new users
func welcomeEmailHandler(ctx context.Context) (err error) {
	var j *jobs.Job
	j, err = jobs.FromContext(ctx)
	if err != nil {
		log.Println("unable to process welcome email:", err)
		return
	}
	recipient := j.Payload["recipient"].(string)
	verificationURL := j.Payload["verification_url"].(string)
	err = sendWelcomeEmail(recipient, verificationURL, "Garbage Speak")

	return
}

// env returns the server's environment name, e.g. "development"
func env() string {
	e := os.Getenv("GO_ENV")
	if e != "" {
		return e
	}

	return "development"
}

// appURL returns the site's base URL, e.g. http://localhost:1313 in development, https://<SITE_DOMAIN> in production
func appURL() string {
	if env() == "development" {
		return fmt.Sprintf("http://%s", os.Getenv("SITE_HOST"))
	}

	addr := os.Getenv("SITE_DOMAIN")
	if addr == "" {
		log.Fatalf("SITE_DOMAIN is not set")
	}

	return fmt.Sprintf("https://%s", addr)
}

// apiURL returns the server's base URL, e.g. http://localhost:1314 in development, https://<API_HOST> in production
func apiURL() string {
	if env() == "development" {
		return fmt.Sprintf("http://%s", os.Getenv("API_HOST"))
	}

	addr := os.Getenv("SITE_DOMAIN")
	if addr == "" {
		log.Fatalf("SITE_DOMAIN is not set")
	}

	return fmt.Sprintf("https://%s", addr)
}

func ise(err error, w http.ResponseWriter) {
	fmt.Fprintf(w, "error: %v", err)
	w.WriteHeader(http.StatusInternalServerError)
}

func isLoggedIn(r *http.Request) bool {
	_, err := r.Cookie("session_id")
	return err == nil
}
