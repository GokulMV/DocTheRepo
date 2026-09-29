// Package migrations embeds the SQL migrations so the binary can migrate its own database on start.
package migrations

import "embed"

// FS holds every NNNN_name.{up,down}.sql file.
//
//go:embed *.sql
var FS embed.FS
