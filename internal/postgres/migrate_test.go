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
	if len(migrations) != 1 {
		t.Fatalf("len(migrations) = %d, want 1", len(migrations))
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
}
