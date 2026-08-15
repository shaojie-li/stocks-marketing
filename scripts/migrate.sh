#!/bin/sh
set -eu

: "${DATABASE_URL:?DATABASE_URL is required}"

go run github.com/jackc/tern/v2@v2.4.2 migrate --migrations migrations --conn-string "$DATABASE_URL"
go run ./cmd/migrate-river
