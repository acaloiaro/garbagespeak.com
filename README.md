# Garbage Speak

A fun, dumb, and ultimately useless website inspired by this [tour de force on corporate garabe speak](https://www.vulture.com/2020/02/spread-of-corporate-speak.html).

## About

This site started as a more robust proof of concept for creating dynamic Hugo sites with htmx and Go.

Write up detailing the concept https://adriano.fyi/posts/2023/2023-07-04-making-hugo-static-sites-dynamic-with-htmx-and-go/

The template from which this site is built https://github.com/acaloiaro/hugo-htmx-go-template/

## Dev

This README is very much still a WIP.

### Getting Started

To get started, fetch dependencies and build the dev tools.

```
bin/fetch-deps.sh
go build -o bin/develop internal/cmd/develop/main.go
go build -o bin/build internal/cmd/build/main.go
mkdir public && touch public/.empty
```

**Running in dev**

`bin/develop`

**Build fat binary**

`bin/build`

### Migrations

Migrations require `go-migrate` to be run locally:

`go install github.com/golang-migrate/migrate@v4`

#### Adding migrations

`migrate create -ext sql -dir migrations -seq name_of_migration`

#### Running migrations

Migrate up

`migrate -database ${POSTGRESQL_URL} -path migrations up`

Migrate down

`migrate -database ${POSTGRESQL_URL} -path migrations down`
