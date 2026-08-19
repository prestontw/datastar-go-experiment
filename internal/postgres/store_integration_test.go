//go:build integration

package postgres

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/lib/pq"

	"github.com/preston/go-datastar-patient-dashboard/internal/domain"
)

func TestMigratedStoreSupportsPatientTaskLifecycle(t *testing.T) {
	store := newMigratedTestStore(t)
	ctx := context.Background()

	snapshot, err := store.Dashboard(ctx, domain.DashboardQuery{Status: "open"})
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Patients) != 4 {
		t.Fatalf("seeded patients = %d, want 4", len(snapshot.Patients))
	}

	// Searching filters the patient picker and clears a selection that is not
	// among the results. The user explicitly chooses a result before its task
	// pane is shown.
	snapshot, err = store.Dashboard(ctx, domain.DashboardQuery{
		PatientID: "019fbd32-0601-7001-8000-000000000001",
		Status:    "open",
		Search:    "Noor",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Patients) != 1 || snapshot.Patients[0].Name != "Noor Ahmed" {
		t.Fatalf("filtered patients = %#v", snapshot.Patients)
	}
	if snapshot.SelectedPatient != nil {
		t.Fatalf("selected patient after filtering = %#v, want nil", snapshot.SelectedPatient)
	}

	patient := domain.NewPatient{
		Name:        "Integration Patient",
		DateOfBirth: time.Date(1990, 6, 15, 0, 0, 0, 0, time.UTC),
		Pronouns:    "they/them",
		CareTeam:    "Integration Clinician",
	}
	patientID, err := store.CreatePatient(ctx, patient)
	if err != nil {
		t.Fatal(err)
	}
	if len(patientID) != 36 || patientID[14] != '7' {
		t.Fatalf("created patient ID = %q, want UUIDv7", patientID)
	}
	task := domain.NewTask{
		PatientID: patientID,
		Title:     "Review integration result",
		DueDate:   time.Now().UTC(),
		Priority:  "important",
	}
	ownerHash := []byte("integration-test-draft-owner-hash")
	tabID := "019fbd32-0700-7000-8000-000000000001"
	draft := domain.TaskDraft{
		PatientID: patientID,
		Title:     "Review integration",
		DueDate:   "2026-08-20",
		Priority:  "important",
		Revision:  1,
	}
	if err := store.SaveTaskDraft(ctx, ownerHash, tabID, draft); err != nil {
		t.Fatal(err)
	}
	persistedDraft, err := store.TaskDraft(ctx, ownerHash, tabID, patientID)
	if err != nil {
		t.Fatal(err)
	}
	if persistedDraft != draft {
		t.Fatalf("persisted draft = %#v, want %#v", persistedDraft, draft)
	}
	taskID, err := store.CreateTask(ctx, task, ownerHash, tabID, 2)
	if err != nil {
		t.Fatal(err)
	}
	clearedDraft, err := store.TaskDraft(ctx, ownerHash, tabID, patientID)
	if err != nil {
		t.Fatal(err)
	}
	if clearedDraft.Title != "" || clearedDraft.DueDate != "" || clearedDraft.Priority != "routine" || clearedDraft.Revision != 2 {
		t.Fatalf("cleared draft = %#v", clearedDraft)
	}
	// An autosave that began before task submission cannot resurrect the draft.
	if err := store.SaveTaskDraft(ctx, ownerHash, tabID, draft); err != nil {
		t.Fatal(err)
	}
	clearedDraft, err = store.TaskDraft(ctx, ownerHash, tabID, patientID)
	if err != nil {
		t.Fatal(err)
	}
	if clearedDraft.Title != "" || clearedDraft.Revision != 2 {
		t.Fatalf("stale autosave resurrected draft: %#v", clearedDraft)
	}

	snapshot, err = store.Dashboard(ctx, domain.DashboardQuery{PatientID: patientID, Status: "open"})
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.SelectedPatient == nil || snapshot.SelectedPatient.ID != patientID {
		t.Fatalf("selected patient = %#v", snapshot.SelectedPatient)
	}
	if len(snapshot.Tasks) != 1 || snapshot.Tasks[0].Title != task.Title {
		t.Fatalf("open tasks = %#v", snapshot.Tasks)
	}

	if err := store.ToggleTask(ctx, taskID); err != nil {
		t.Fatal(err)
	}
	snapshot, err = store.Dashboard(ctx, domain.DashboardQuery{PatientID: patientID, Status: "done"})
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Tasks) != 1 || snapshot.Tasks[0].Status != "done" {
		t.Fatalf("completed tasks = %#v", snapshot.Tasks)
	}
}

func TestMigrateIsIdempotentAndRecordsEmbeddedHistory(t *testing.T) {
	store := newMigratedTestStore(t)
	ctx := context.Background()
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("second Migrate() call: %v", err)
	}

	var name, checksum string
	if err := store.db.QueryRowContext(ctx, `
		SELECT name, checksum
		FROM schema_migrations
		WHERE name = '001_init.sql'`,
	).Scan(&name, &checksum); err != nil {
		t.Fatal(err)
	}
	if name != "001_init.sql" || len(checksum) != 64 {
		t.Fatalf("initial migration ledger row = name %q, checksum %q", name, checksum)
	}
}

// newMigratedTestStore follows the Zero to Production test pattern: every test
// gets a new PostgreSQL database with the production migration path applied.
// Tests cannot pass because a developer's long-lived database happens to have
// compatible schema or data.
func newMigratedTestStore(t *testing.T) *Store {
	t.Helper()

	adminURL := os.Getenv("TEST_DATABASE_URL")
	if adminURL == "" {
		adminURL = "postgres://dashboard:dashboard@localhost:5432/postgres?sslmode=disable"
	}
	databaseName := "patient_dashboard_test_" + randomSuffix(t)
	databaseURL := databaseURLForName(t, adminURL, databaseName)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	adminDB, err := sql.Open("postgres", adminURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = adminDB.Close() })
	if err := adminDB.PingContext(ctx); err != nil {
		t.Fatalf("connect to test PostgreSQL: %v", err)
	}
	if _, err := adminDB.ExecContext(ctx, "CREATE DATABASE "+pq.QuoteIdentifier(databaseName)); err != nil {
		t.Fatalf("create test database: %v", err)
	}

	store, err := Open(ctx, databaseURL)
	if err != nil {
		dropTestDatabase(t, adminDB, databaseName)
		t.Fatal(err)
	}
	if err := store.Migrate(ctx); err != nil {
		_ = store.Close()
		dropTestDatabase(t, adminDB, databaseName)
		t.Fatalf("migrate test database: %v", err)
	}

	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("close test store: %v", err)
		}
		dropTestDatabase(t, adminDB, databaseName)
	})
	return store
}

func dropTestDatabase(t *testing.T, adminDB *sql.DB, name string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := adminDB.ExecContext(ctx, "DROP DATABASE IF EXISTS "+pq.QuoteIdentifier(name)+" WITH (FORCE)"); err != nil {
		t.Errorf("drop test database %s: %v", name, err)
	}
}

func databaseURLForName(t *testing.T, adminURL, name string) string {
	t.Helper()
	parsed, err := url.Parse(adminURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		t.Fatalf("invalid TEST_DATABASE_URL %q: %v", adminURL, err)
	}
	parsed.Path = "/" + name
	parsed.RawPath = ""
	return parsed.String()
}

func randomSuffix(t *testing.T) string {
	t.Helper()
	value := make([]byte, 8)
	if _, err := rand.Read(value); err != nil {
		t.Fatal(fmt.Errorf("generate test database suffix: %w", err))
	}
	return hex.EncodeToString(value)
}
