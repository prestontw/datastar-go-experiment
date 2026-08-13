package postgres

import (
	"strings"
	"testing"
)

func TestEmbeddedMigrationsAreOrderedAndChecksummed(t *testing.T) {
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	if len(migrations) != 2 {
		t.Fatalf("len(migrations) = %d, want 2", len(migrations))
	}
	migration := migrations[0]
	if migration.Name != "001_init.sql" {
		t.Fatalf("first migration = %q, want 001_init.sql", migration.Name)
	}
	if len(migration.Checksum) != 64 {
		t.Fatalf("checksum length = %d, want 64", len(migration.Checksum))
	}
	if !strings.Contains(migration.SQL, "CREATE TABLE IF NOT EXISTS patients") {
		t.Fatal("initial migration does not contain the patient schema")
	}
	if !strings.Contains(migration.SQL, "DEFAULT uuidv7()") {
		t.Fatal("initial migration does not use PostgreSQL UUIDv7 defaults")
	}
	if migrations[1].Name != "002_task_drafts.sql" || !strings.Contains(migrations[1].SQL, "CREATE TABLE task_drafts") {
		t.Fatalf("second migration does not contain task drafts: %#v", migrations[1])
	}
}
