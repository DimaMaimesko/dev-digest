// Package migrations holds the database migrations, in Drizzle's format:
// meta/_journal.json lists them in order, and each has a .sql file whose
// statements are separated by "--> statement-breakpoint". internal/migrate
// applies them.
//
// To change the schema, add the next NNNN_name.sql file and its entry at the
// end of the journal, with a later "when" (milliseconds since 1970). Never
// edit an applied migration: databases record each file's SHA-256. Then run
// `make generate`, since sqlc reads the tables from these files too.
//
// The meta/*_snapshot.json files are drizzle-kit's record of the TS schema;
// the Go code doesn't read them.
package migrations

import "embed"

// FS holds the journal and the SQL files, built into the binary.
//
//go:embed meta/_journal.json *.sql
var FS embed.FS
