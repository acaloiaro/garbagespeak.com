package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"embed"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/smtp"
	"os"
	"strings"
	"text/template"
	"time"

	"github.com/acaloiaro/neoq"
	"github.com/acaloiaro/neoq/backends/postgres"
	"github.com/acaloiaro/neoq/handler"
	"github.com/acaloiaro/neoq/jobs"
	"github.com/acaloiaro/neoq/types"
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
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

// Garbage represents 'garbage' records from the database
type Garbage struct {
	ID        uuid.UUID
	OwnerID   *uuid.UUID
	Title     string
	Content   string
	Metadata  map[string]any
	CreatedAt time.Time
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

// embed all migrations with the binary
//
//go:embed migrations/*.sql
var migrations embed.FS

var db *pgxpool.Pool
var sessions *scs.SessionManager
var sessionStore *pgxstore.PostgresStore
var NQ types.Backend

// run migrations, acquire a database connection pool, and create the session store
func init() {
	d, err := iofs.New(migrations, "migrations")
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
	dbconfig.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		pgxuuid.Register(conn.TypeMap())
		return nil
	}

	db, err = pgxpool.NewWithConfig(context.Background(), dbconfig)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Unable to connect to database: %v\n", err)
		os.Exit(1)
	}

	sessions = scs.New()
	sessionStore = pgxstore.New(db)

	sessions.Cookie.Name = "session_id"
	//sessions.Cookie.Domain = siteDomain()
	//sessions.Cookie.HttpOnly = true
	//sessions.Cookie.Persist = true
	sessions.Cookie.SameSite = http.SameSiteNoneMode
	//sessions.Cookie.Secure = true
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
}

func main() {
	defer db.Close()
	defer sessionStore.StopCleanup()
	defer NQ.Shutdown(context.Background())

	r := chi.NewRouter()
	//r.Use(sessions.LoadAndSave)
	r.Use(middleware.Logger)
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(cors.Handler(cors.Options{
		AllowCredentials: true,
		AllowedOrigins:   []string{"http://localhost:1313", fmt.Sprintf("https://%s", os.Getenv("SITE_DOMAIN"))},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Cookie", "Authorization", "Content-Type", "X-CSRF-Token", "hx-target", "hx-current-url", "hx-request"},
		ExposedHeaders:   []string{"Link", "HX-Location"},
		MaxAge:           300, // Maximum value not ignored by any of major browsers
	}))

	// Add any number of handlers for custom endpoints here
	r.Get("/garbage_bin", garbageBin)
	r.Get("/nav/user_items", navUserItems)
	r.Route("/users", func(r chi.Router) {
		r.Get("/create", newAccountHandler)
		r.Post("/login", loginHandler)
		r.Post("/create", createAccount)
		r.Get("/logout", logoutHandler)
		r.Get("/email_verification/{uev_id}", emailVerification)
	})

	fmt.Printf("Starting API server on port 1314\n")
	if err := http.ListenAndServe("0.0.0.0:1314", sessions.LoadAndSave(r)); err != nil {
		log.Fatal(err)
	}
}

// loginHandler renders the login page

func loginHandler(w http.ResponseWriter, r *http.Request) {
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

	var userID string
	var storedPasswordHash string
	err := db.QueryRow(r.Context(), "SELECT id,password FROM users WHERE username = $1", username).Scan(&userID, &storedPasswordHash)
	if err != nil {
		log.Fatalf("can't verify email address: %v", err)
	}

	log.Println("stored password", storedPasswordHash)

	if err = bcrypt.CompareHashAndPassword([]byte(storedPasswordHash), []byte(password)); err == nil {
		err = sessions.RenewToken(r.Context())
		if err != nil {
			ise(err, w)
			return
		}

		sessions.Put(r.Context(), "userID", userID)
		w.Header().Add("hx-location", baseURL())

		return
	}

	log.Println("Passwords don't match")
}

// logoutHandler handles users logout requests
func logoutHandler(w http.ResponseWriter, r *http.Request) {
	sessions.Clear(r.Context())
	http.Redirect(w, r, baseURL(), http.StatusFound)
}

// navUserItems returns a nav items depending on whether the user is logged in
func navUserItems(w http.ResponseWriter, r *http.Request) {
	userID := sessions.GetString(r.Context(), "userID")
	var tmpl *template.Template

	log.Println("user id:", userID)
	if userID == "" {
		tmpl = template.Must(template.ParseFiles("partials/nav/non_user_nav_items.html"))
	} else {
		tmpl = template.Must(template.ParseFiles("partials/nav/user_nav_items.html"))
	}

	var buff = bytes.NewBufferString("")
	err := tmpl.Execute(buff, map[string]any{"ApiURL": apiURL()})
	if err != nil {
		ise(err, w)
		return
	}

	w.Header().Add("Content-Type", "text/html")
	w.WriteHeader(http.StatusOK)
	w.Write(buff.Bytes())
}

// newAccountHandler serves the new account form
func newAccountHandler(w http.ResponseWriter, r *http.Request) {
	var buff = bytes.NewBufferString("")
	tmpl := template.Must(template.ParseFiles("partials/users/create.html"))
	err := tmpl.Execute(buff, map[string]any{})
	if err != nil {
		ise(err, w)
		return
	}

	w.Header().Add("Content-Type", "text/html")
	w.WriteHeader(http.StatusOK)
	w.Write(buff.Bytes())
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
		log.Fatalf("can't verify email address: %v", err)
	}

	err = sessions.RenewToken(r.Context())
	if err != nil {
		ise(err, w)
		return
	}

	// create the user's session
	sessions.Put(r.Context(), "userID", userID)

	tx.Commit(r.Context())

	http.Redirect(w, r, baseURL(), http.StatusFound)
}

// createAccount creates new accounts
func createAccount(w http.ResponseWriter, r *http.Request) {
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
		log.Println("invalid email:", email)
		w.WriteHeader(400)
		return
	}

	passwordHash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		log.Println("error hashing password", err)
		ise(err, w)
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

	var buff = bytes.NewBufferString("")
	tmpl := template.Must(template.ParseFiles("partials/users/created.html"))
	err = tmpl.Execute(buff, map[string]any{"Email": email})
	if err != nil {
		ise(err, w)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write(buff.Bytes())
}

// garbageBin returns the latest garbage
func garbageBin(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	if name == "null" || name == "" {
		name = "World"
	}

	ctx := context.Background()
	posts := []*Garbage{}
	err := pgxscan.Select(ctx, db, &posts, `SELECT id,owner_id,title, content, metadata, created_at FROM garbages`)
	if err != nil {
		log.Println("error fetching garbage", err)
		return
	}

	var buff = bytes.NewBufferString("")
	tmpl := template.Must(template.ParseFiles("partials/posts.html"))
	err = tmpl.Execute(buff, map[string]any{"Posts": posts})
	if err != nil {
		ise(err, w)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write(buff.Bytes())
}

// sendWelcomeEmail sends an email to recipient containing a special URL that only that can know, for the purpose of
// email address verification
func sendWelcomeEmail(recipient, verificationURL, siteName string) error {
	smtp_host := os.Getenv("SMTP_HOST")
	log.Println("SMTP host:", smtp_host)
	to := []string{recipient}
	msg := []byte(fmt.Sprintf("To: %s\r\n", recipient) +
		fmt.Sprintf("From: %s", os.Getenv("SMTP_FROM_ADDRESS")) + "\r\n" +
		fmt.Sprintf("Subject: Welcome to %s!\r\n", siteName) + "\r\n" +
		fmt.Sprintf("Verify your email address by visiting: %s\r\n", verificationURL))

	log.Println("to:", to, "msg:", msg)
	host, _, _ := net.SplitHostPort(smtp_host)

	auth := smtp.PlainAuth("", os.Getenv("SMTP_USERNAME"), os.Getenv("SMTP_PASSWORD"), host)

	// TLS config
	tlsconfig := &tls.Config{
		InsecureSkipVerify: true,
		ServerName:         host,
	}

	// Here is the key, you need to call tls.Dial instead of smtp.Dial
	// for smtp servers running on 465 that require an ssl connection
	// from the very beginning (no starttls)
	conn, err := tls.Dial("tcp", smtp_host, tlsconfig)
	if err != nil {
		log.Panic(err)
	}

	c, err := smtp.NewClient(conn, host)
	if err != nil {
		log.Panic(err)
	}

	// Auth
	if err = c.Auth(auth); err != nil {
		log.Panic(err)
	}

	// From
	if err = c.Mail(os.Getenv("SMTP_FROM_ADDRESS")); err != nil {
		log.Panic(err)
	}

	//Recipient
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

// siteDomain returns the server's domain
func siteDomain() string {
	d := os.Getenv("SITE_DOMAIN")
	if d != "" {
		return d
	}

	return ""
}

// env returns the server's environment name, e.g. "development"
func env() string {
	e := os.Getenv("GO_ENV")
	if e != "" {
		return e
	}

	return "development"
}

// baseURL returns the site's base URL, e.g. http://localhost:1313 in development, https://<SITE_DOMAIN> in production
func baseURL() string {
	if env() == "development" && siteDomain() == "" {
		addr := os.Getenv("HOSTNAME")
		if addr == "" {
			addr = "localhost"
		}
		return fmt.Sprintf("http://%s:1313", addr)
	} else {
		addr := os.Getenv("SITE_DOMAIN")
		if addr == "" {
			addr = siteDomain()
		}

		return fmt.Sprintf("https://%s", addr)
	}
}

// apiURL returns the server's base URL, e.g. http://localhost:1314 in development, https://<API_HOST> in production
func apiURL() string {
	if env() == "development" && siteDomain() == "" {
		addr := os.Getenv("HOSTNAME")
		if addr == "" {
			addr = "localhost"
		}
		return fmt.Sprintf("http://%s:1314", addr)
	} else {
		addr := os.Getenv("API_HOST")
		if addr == "" {
			addr = siteDomain()
		}

		return fmt.Sprintf("https://%s", addr)
	}

}

func ise(err error, w http.ResponseWriter) {
	fmt.Fprintf(w, "error: %v", err)
	w.WriteHeader(http.StatusInternalServerError)
}
