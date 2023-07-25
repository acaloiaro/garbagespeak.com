package main

import (
	"bytes"
	"context"
	"embed"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"text/template"
	"time"

	"github.com/alexedwards/scs/pgxstore"
	"github.com/alexedwards/scs/v2"
	"github.com/georgysavva/scany/v2/pgxscan"
	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrations embed.FS

//go:embed all:public
var content embed.FS

var db *pgxpool.Pool
var sessions *scs.SessionManager
var sessionStore *pgxstore.PostgresStore

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
		log.Println(err)
	}

	db, err = pgxpool.New(context.Background(), os.Getenv("POSTGRES_URL"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Unable to connect to database: %v\n", err)
		os.Exit(1)
	}

	sessions = scs.New()

	sessionStore = pgxstore.NewWithCleanupInterval(db, 10*time.Second)
	sessions.Store = sessionStore
}

func main() {
	defer db.Close()
	defer sessionStore.StopCleanup()

	mux := http.NewServeMux()
	serverRoot, _ := fs.Sub(content, "public")

	// Serve all hugo content (the 'public' directory) at the root url
	mux.Handle("/", http.FileServer(http.FS(serverRoot)))

	// Add any number of handlers for custom endpoints here
	mux.HandleFunc("/garbage_bin", cors(http.HandlerFunc(helloWorld)))
	mux.HandleFunc("/hello_world_form", cors(http.HandlerFunc(helloWorldForm)))

	fmt.Printf("Starting API server on port 1314\n")
	if err := http.ListenAndServe("0.0.0.0:1314", sessions.LoadAndSave(mux)); err != nil {
		log.Fatal(err)
	}
}

// the handler accepts GET requests to /hello_world
// It checks the URL params for the "name" param and populates the html/template variable with its value
// if no "name" url parameter is present, "name" is defaulted to "World"
//
// It responds with the the HTML partial `partials/helloworld.html`
func helloWorld(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	if name == "null" || name == "" {
		name = "World"
	}
	sessions.Put(r.Context(), "message", "Hello from a session!")
	tmpl := template.Must(template.ParseFiles("partials/posts.html"))
	var buff = bytes.NewBufferString("")
	type Garbage struct {
		ID        int64
		OwnerID   *int64
		Title     string
		Content   string
		Metadata  map[string]any
		CreatedAt time.Time
	}
	ctx := context.Background()
	posts := []*Garbage{}
	err := pgxscan.Select(ctx, db, &posts, `SELECT id,owner_id,title, content, metadata, created_at FROM garbages`)
	if err != nil {
		log.Println("error fetching garbage", err)
		return
	}
	log.Println(posts)
	posts = append(posts, &Garbage{
		Title:     "What's the ask here?",
		Content:   "If it's not too much of a reach, we can implement a solve by EOD.",
		CreatedAt: time.Now(),
		Metadata: map[string]any{
			"Author": "Adriano Caloiaro",
			"tags":   []string{"ask", "reach", "solve"},
		}})

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

// this handler accepts POST requests to /hello_world_form
// It checks the post request body for the form value "name" and populates the html/template
// variable with its value
//
// It responds with a simple greeting HTML partial
func helloWorldForm(w http.ResponseWriter, r *http.Request) {
	// The name is not in the query param, let's see if it was submitted as a form
	if err := r.ParseForm(); err != nil {
		ise(err, w)
		return
	}

	// TODO: Unimplemented
	w.WriteHeader(http.StatusInternalServerError)

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
