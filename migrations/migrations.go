package migrations

import "embed"

// Files содержит SQL-миграции, применяемые при старте приложения.
//
//go:embed *.sql
var Files embed.FS
