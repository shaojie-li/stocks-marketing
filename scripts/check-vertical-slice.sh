#!/bin/sh
set -eu

: "${TEST_DATABASE_URL:?TEST_DATABASE_URL is required}"

DATABASE_URL="$TEST_DATABASE_URL" ./scripts/migrate.sh
go test -race -count=1 -run TestVerticalSliceIsIdempotentAndKeepsWebhookEncrypted ./internal/app
