package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"embed"
	"fmt"
	"io/fs"
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

// Embed all hugo output as 'public'
//
//go:embed all:public
var content embed.FS

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
	sessionStore = pgxstore.NewWithCleanupInterval(db, 10*time.Second)
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

	mux := http.NewServeMux()
	serverRoot, _ := fs.Sub(content, "public")

	// Serve all hugo content (the 'public' directory) at the root url
	mux.Handle("/", http.FileServer(http.FS(serverRoot)))

	// Add any number of handlers for custom endpoints here
	mux.HandleFunc("/garbage_bin", cors(http.HandlerFunc(garbageBin)))
	mux.HandleFunc("/users/create", cors(http.HandlerFunc(createAccount)))

	fmt.Printf("Starting API server on port 1314\n")
	if err := http.ListenAndServe("0.0.0.0:1314", sessions.LoadAndSave(mux)); err != nil {
		log.Fatal(err)
	}
}

// createAccount creates new accounts
func createAccount(w http.ResponseWriter, r *http.Request) {
	// The name is not in the query param, let's see if it was submitted as a form
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
			"verification_url": fmt.Sprintf("%s/users/email_verification/%s", baseURL(), uevID),
		},
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "unable to queue email veritifcation: %v", err)
		return
	}

}

// garbageBin returns the latest garbage
func garbageBin(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	if name == "null" || name == "" {
		name = "World"
	}
	sessions.Put(r.Context(), "message", "Hello from a session!")
	tmpl := template.Must(template.ParseFiles("partials/posts.html"))
	var buff = bytes.NewBufferString("")
	ctx := context.Background()
	posts := []*Garbage{}
	err := pgxscan.Select(ctx, db, &posts, `SELECT id,owner_id,title, content, metadata, created_at FROM garbages`)
	if err != nil {
		log.Println("error fetching garbage", err)
		return
	}
	err = tmpl.Execute(buff, map[string]any{"Posts": posts})
	if err != nil {
		ise(err, w)
		return
	}
	msg := sessions.GetString(r.Context(), "message")
	w.Header().Add("X-Message", msg)
	w.WriteHeader(http.StatusOK)
	w.Write(buff.Bytes())
}

func ise(err error, w http.ResponseWriter) {
	fmt.Fprintf(w, "error: %v", err)
	w.WriteHeader(http.StatusInternalServerError)
}

// cors middleware
func cors(h http.Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// in development, the Origin is the the Hugo server, i.e. http://localhost:1313
		// but in production, it is the domain name where one's site is deployed
		//
		// CHANGE THIS: You likely do not want to allow any origin (*) in production. The value should be the base URL of
		// where your static content is served
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, hx-target, hx-current-url, hx-request")
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		h.ServeHTTP(w, r)
	}
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

// baseURL returns the server's base URL, e.g. http://localhost:1313 in development, https://<SITE_DOMAIN> in production
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
