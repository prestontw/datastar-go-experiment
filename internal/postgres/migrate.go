package postgres

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
)

const migrationLockSeed int64 = 724_846_231

// migrationFiles are compiled into both the local and containerized server
// binaries, so every execution mode applies exactly the same schema history.
//
//go:embed migrations/*.sql
var migrationFiles embed.FS

type migration struct {
	Name     string
	SQL      string
	Checksum string
}

func (s *Store) Migrate(ctx context.Context) error {
	migrations, err := loadMigrations()
	if err != nil {
		return err
	}

	// PostgreSQL DDL is transactional. Applying the pending files and recording
	// them in one transaction ensures a failed startup leaves neither a partial
	// schema change nor a false migration record.
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin migrations: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Multiple local/container processes can point at the same logical database.
	// Deriving the transaction-scoped lock from current_database() serializes
	// migrations for that database without blocking independent agent databases
	// in the same PostgreSQL cluster.
	if _, err := tx.ExecContext(ctx,
		"SELECT pg_advisory_xact_lock(hashtextextended(current_database(), $1))",
		migrationLockSeed,
	); err != nil {
		return fmt.Errorf("lock migrations: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			name TEXT PRIMARY KEY,
			checksum TEXT NOT NULL,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`); err != nil {
		return fmt.Errorf("create migration ledger: %w", err)
	}

	applied, err := appliedMigrations(ctx, tx)
	if err != nil {
		return err
	}
	embedded := make(map[string]struct{}, len(migrations))
	for _, migration := range migrations {
		embedded[migration.Name] = struct{}{}
	}
	for name := range applied {
		if _, ok := embedded[name]; !ok {
			return fmt.Errorf("database contains migration %s that this server binary does not know", name)
		}
	}

	for _, migration := range migrations {
		if checksum, ok := applied[migration.Name]; ok {
			if checksum != migration.Checksum {
				return fmt.Errorf(
					"migration %s changed after it was applied (database checksum %s, embedded checksum %s)",
					migration.Name,
					checksum,
					migration.Checksum,
				)
			}
			continue
		}

		if _, err := tx.ExecContext(ctx, migration.SQL); err != nil {
			return fmt.Errorf("apply migration %s: %w", migration.Name, err)
		}
		if _, err := tx.ExecContext(ctx,
			"INSERT INTO schema_migrations (name, checksum) VALUES ($1, $2)",
			migration.Name,
			migration.Checksum,
		); err != nil {
			return fmt.Errorf("record migration %s: %w", migration.Name, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit migrations: %w", err)
	}
	return nil
}

func loadMigrations() ([]migration, error) {
	paths, err := fs.Glob(migrationFiles, "migrations/*.sql")
	if err != nil {
		return nil, fmt.Errorf("list embedded migrations: %w", err)
	}
	sort.Strings(paths)
	if len(paths) == 0 {
		return nil, fmt.Errorf("no embedded database migrations found")
	}

	migrations := make([]migration, 0, len(paths))
	for _, path := range paths {
		contents, err := migrationFiles.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read embedded migration %s: %w", path, err)
		}
		name := filepath.Base(path)
		if !strings.Contains(name, "_") {
			return nil, fmt.Errorf("migration %s must use a numeric_name.sql filename", name)
		}
		digest := sha256.Sum256(contents)
		migrations = append(migrations, migration{
			Name:     name,
			SQL:      string(contents),
			Checksum: fmt.Sprintf("%x", digest),
		})
	}
	return migrations, nil
}

func appliedMigrations(ctx context.Context, tx *sql.Tx) (map[string]string, error) {
	rows, err := tx.QueryContext(ctx, "SELECT name, checksum FROM schema_migrations ORDER BY name")
	if err != nil {
		return nil, fmt.Errorf("query migration ledger: %w", err)
	}
	defer rows.Close()

	applied := make(map[string]string)
	for rows.Next() {
		var name, checksum string
		if err := rows.Scan(&name, &checksum); err != nil {
			return nil, fmt.Errorf("scan migration ledger: %w", err)
		}
		applied[name] = checksum
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate migration ledger: %w", err)
	}
	return applied, nil
}
