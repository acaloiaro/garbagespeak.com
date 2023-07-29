# Garbage Speak

A fun and useless website

## Migrations

Migrations required `go-migrate` to be run locally:

`go install github.com/golang-migrate/migrate@v4`

### Adding migrations

`migrate create -ext sql -dir migrations -seq name_of_migration`

### Running migrations

Migrate up

`migrate -database ${POSTGRESQL_URL} -path migrations up`

Migrate down

`migrate -database ${POSTGRESQL_URL} -path migrations down`
