// Package migrations embeds the SQL schema migrations applied by
// store.Migrate. Files are named NNNN_description.sql and applied in
// lexical order, each inside one transaction.
package migrations

import "embed"

// FS holds the *.sql migration files.
//
//go:embed *.sql
var FS embed.FS
