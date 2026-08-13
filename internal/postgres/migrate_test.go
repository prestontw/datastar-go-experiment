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
	if len(migrations) == 0 {
		t.Fatal("no embedded migrations found")
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
	draftMigration := migrationNamed(migrations, "002_task_drafts.sql")
	if draftMigration == nil || !strings.Contains(draftMigration.SQL, "CREATE TABLE task_drafts") {
		t.Fatalf("task draft migration not found: %#v", migrations)
	}
}

func migrationNamed(migrations []migration, name string) *migration {
	for i := range migrations {
		if migrations[i].Name == name {
			return &migrations[i]
		}
	}
	return nil
}
