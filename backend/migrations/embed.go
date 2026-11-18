// Package migrations embeds the versioned SQL migration files so the migrate
// command and integration tests always use the same source of truth.
package migrations

import "embed"

// FS contains all *.sql migration files in golang-migrate naming format:
// {version}_{title}.up.sql and {version}_{title}.down.sql.
//
//go:embed *.sql
var FS embed.FS
