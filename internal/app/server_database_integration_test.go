//go:build integration

package app

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/lib/pq"

	"github.com/preston/go-datastar-patient-dashboard/internal/postgres"
	"github.com/preston/go-datastar-patient-dashboard/internal/realtime"
	"github.com/preston/go-datastar-patient-dashboard/internal/security"
	webviews "github.com/preston/go-datastar-patient-dashboard/internal/web"
)

// TestDatabaseBackedHTTPProtocol is a compact wire-level regression snapshot.
// It crosses real TLS/HTTP2, security middleware, Datastar SSE, commands,
// migrations, and PostgreSQL without paying the cost or variability of a DOM.
func TestDatabaseBackedHTTPProtocol(t *testing.T) {
	store := newProtocolIntegrationStore(t)
	renderer, err := webviews.NewRenderer()
	if err != nil {
		t.Fatal(err)
	}
	protector, err := security.New([]byte("a-protocol-integration-secret-at-least-32-bytes"))
	if err != nil {
		t.Fatal(err)
	}
	application := NewServer(
		store,
		renderer,
		realtime.NewHub(),
		protector,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	application.now = func() time.Time { return time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC) }

	server := httptest.NewUnstartedServer(application.Handler())
	server.EnableHTTP2 = true
	server.StartTLS()
	t.Cleanup(server.Close)

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := server.Client()
	client.Jar = jar

	shellResponse, err := client.Get(server.URL + "/patients?status=open")
	if err != nil {
		t.Fatal(err)
	}
	shellCookies := shellResponse.Cookies()
	shell := readResponse(t, shellResponse)
	if !strings.Contains(shell, `<main id="morph"`) {
		t.Fatal("shell response does not contain the morph target")
	}

	origin, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	csrf := cookieValue(jar.Cookies(origin), security.CSRFCookieName)
	if csrf == "" {
		t.Fatal("stateful client did not retain the CSRF cookie")
	}

	createPatient := doProtocolCommand(t, client, server.URL, csrf, "create-patient", "", map[string]any{
		"patientName":     "Protocol Patient",
		"patientDob":      "1991-02-03",
		"patientPronouns": "they/them",
		"patientCareTeam": "HTTP Client",
	})
	if !strings.Contains(createPatient.Event, "datastar-patch-signals") {
		t.Fatalf("create patient response is not a signal patch: %s", createPatient.Event)
	}

	search := initialProtocolSSE(t, client, server.URL+"/patients?status=open&q=Protocol", server.URL, csrf)
	if !strings.Contains(search.Event, "Protocol Patient") || !strings.Contains(search.Event, "1 in view") {
		t.Fatalf("search event does not contain the created patient: %s", search.Event)
	}
	if !strings.Contains(search.Event, "Select or create a patient") {
		t.Fatal("search unexpectedly selected a patient")
	}
	patientID := firstUUID(t, search.Event)
	if patientID[14] != '7' {
		t.Fatalf("created patient ID = %q, want UUIDv7", patientID)
	}

	createTask := doProtocolCommand(t, client, server.URL, csrf, "create-task", "&patient="+patientID, map[string]any{
		"taskTitle":    "Review protocol snapshot",
		"taskDue":      "2026-08-20",
		"taskPriority": "important",
	})
	if !strings.Contains(createTask.Event, "datastar-patch-signals") {
		t.Fatalf("create task response is not a signal patch: %s", createTask.Event)
	}

	selected := initialProtocolSSE(t, client, fmt.Sprintf(
		"%s/patients?patient=%s&status=open&q=Protocol",
		server.URL,
		patientID,
	), server.URL, csrf)
	if !strings.Contains(selected.Event, "Protocol Patient") || !strings.Contains(selected.Event, "Review protocol snapshot") {
		t.Fatalf("selected patient event does not contain persisted patient and task: %s", selected.Event)
	}

	cookieNames := make([]string, 0, len(shellCookies))
	for _, cookie := range shellCookies {
		cookieNames = append(cookieNames, cookie.Name)
	}
	slices.Sort(cookieNames)

	snapshot := fmt.Sprintf(`database-backed HTTP protocol
shell
  protocol: %s
  status: %s
  content-type: %s
  cookies: %s
create patient
  protocol: %s
  status: %s
  event: %s
search created patient
  protocol: %s
  status: %s
  event: %s
  patients in view: 1
  task context: empty
  generated ID version: 7
create task
  protocol: %s
  status: %s
  event: %s
select created patient
  protocol: %s
  status: %s
  event: %s
  patient: Protocol Patient
  task: Review protocol snapshot
`,
		shellResponse.Proto,
		shellResponse.Status,
		shellResponse.Header.Get("Content-Type"),
		strings.Join(cookieNames, ", "),
		createPatient.Protocol,
		createPatient.Status,
		sseEventName(createPatient.Event),
		search.Protocol,
		search.Status,
		sseEventName(search.Event),
		createTask.Protocol,
		createTask.Status,
		sseEventName(createTask.Event),
		selected.Protocol,
		selected.Status,
		sseEventName(selected.Event),
	)
	assertProtocolSnapshot(t, "testdata/database_http_protocol.golden", snapshot)
}

type protocolResponse struct {
	Protocol string
	Status   string
	Event    string
}

func doProtocolCommand(
	t *testing.T,
	client *http.Client,
	origin, csrf, command, querySuffix string,
	signals map[string]any,
) protocolResponse {
	t.Helper()
	signals["csrf"] = csrf
	request := browserRequest(
		t,
		context.Background(),
		origin+"/commands?command="+command+querySuffix,
		origin,
		signals,
	)
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	body := readResponse(t, response)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("%s status = %s, body = %s", command, response.Status, body)
	}
	return protocolResponse{Protocol: response.Proto, Status: response.Status, Event: body}
}

func initialProtocolSSE(t *testing.T, client *http.Client, target, origin, csrf string) protocolResponse {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	request := browserRequest(t, ctx, target, origin, map[string]any{"csrf": csrf})
	response, err := client.Do(request)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	event := readSSEEvent(t, response.Body)
	cancel()
	_ = response.Body.Close()
	return protocolResponse{Protocol: response.Proto, Status: response.Status, Event: event}
}

func sseEventName(event string) string {
	line, _, _ := strings.Cut(event, "\n")
	return strings.TrimPrefix(strings.TrimSpace(line), "event: ")
}

var uuidInEvent = regexp.MustCompile(`[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`)

func firstUUID(t *testing.T, event string) string {
	t.Helper()
	id := uuidInEvent.FindString(event)
	if id == "" {
		t.Fatalf("SSE event does not contain a UUID: %s", event)
	}
	return id
}

func assertProtocolSnapshot(t *testing.T, filename, actual string) {
	t.Helper()
	if os.Getenv("UPDATE_SNAPSHOTS") == "1" {
		if err := os.MkdirAll(filepath.Dir(filename), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filename, []byte(actual), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	expected, err := os.ReadFile(filename)
	if err != nil {
		t.Fatalf("read protocol snapshot: %v (run UPDATE_SNAPSHOTS=1 just test-integration to create it)", err)
	}
	if string(expected) != actual {
		t.Fatalf("protocol snapshot changed (-want +got):\n--- want\n%s--- got\n%s\nRun UPDATE_SNAPSHOTS=1 just test-integration after reviewing the wire-contract change.", expected, actual)
	}
}

func newProtocolIntegrationStore(t *testing.T) *postgres.Store {
	t.Helper()
	adminURL := os.Getenv("TEST_DATABASE_URL")
	if adminURL == "" {
		adminURL = "postgres://dashboard:dashboard@localhost:5432/postgres?sslmode=disable"
	}
	databaseName := "patient_dashboard_http_test_" + protocolRandomSuffix(t)
	databaseURL := protocolDatabaseURL(t, adminURL, databaseName)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	adminDB, err := sql.Open("postgres", adminURL)
	if err != nil {
		t.Fatal(err)
	}
	if err := adminDB.PingContext(ctx); err != nil {
		_ = adminDB.Close()
		t.Fatalf("connect to test PostgreSQL: %v", err)
	}
	if _, err := adminDB.ExecContext(ctx, "CREATE DATABASE "+pq.QuoteIdentifier(databaseName)); err != nil {
		_ = adminDB.Close()
		t.Fatalf("create test database: %v", err)
	}

	store, err := postgres.Open(ctx, databaseURL)
	if err != nil {
		dropProtocolDatabase(t, adminDB, databaseName)
		_ = adminDB.Close()
		t.Fatal(err)
	}
	if err := store.Migrate(ctx); err != nil {
		_ = store.Close()
		dropProtocolDatabase(t, adminDB, databaseName)
		_ = adminDB.Close()
		t.Fatal(err)
	}

	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("close protocol store: %v", err)
		}
		dropProtocolDatabase(t, adminDB, databaseName)
		if err := adminDB.Close(); err != nil {
			t.Errorf("close protocol admin database: %v", err)
		}
	})
	return store
}

func dropProtocolDatabase(t *testing.T, adminDB *sql.DB, name string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := adminDB.ExecContext(ctx, "DROP DATABASE IF EXISTS "+pq.QuoteIdentifier(name)+" WITH (FORCE)"); err != nil {
		t.Errorf("drop protocol test database %s: %v", name, err)
	}
}

func protocolDatabaseURL(t *testing.T, adminURL, name string) string {
	t.Helper()
	parsed, err := url.Parse(adminURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		t.Fatalf("invalid TEST_DATABASE_URL %q: %v", adminURL, err)
	}
	parsed.Path = "/" + name
	parsed.RawPath = ""
	return parsed.String()
}

func protocolRandomSuffix(t *testing.T) string {
	t.Helper()
	value := make([]byte, 8)
	if _, err := rand.Read(value); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(value)
}
