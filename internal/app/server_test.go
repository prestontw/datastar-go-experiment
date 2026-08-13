package app

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/preston/go-datastar-patient-dashboard/internal/domain"
	"github.com/preston/go-datastar-patient-dashboard/internal/realtime"
	"github.com/preston/go-datastar-patient-dashboard/internal/security"
	webviews "github.com/preston/go-datastar-patient-dashboard/internal/web"
)

type fakeRepository struct {
	createdPatient domain.NewPatient
}

func (f *fakeRepository) Ping(context.Context) error { return nil }
func (f *fakeRepository) Dashboard(context.Context, domain.DashboardQuery) (domain.DashboardSnapshot, error) {
	return domain.DashboardSnapshot{}, nil
}
func (f *fakeRepository) Due(context.Context, domain.DueQuery) (domain.DueSnapshot, error) {
	return domain.DueSnapshot{}, nil
}
func (f *fakeRepository) CreatePatient(_ context.Context, patient domain.NewPatient) error {
	f.createdPatient = patient
	return nil
}
func (f *fakeRepository) CreateTask(context.Context, domain.NewTask) error { return nil }
func (f *fakeRepository) ToggleTask(context.Context, string) error         { return nil }

func newTestServer(t *testing.T) (*Server, *fakeRepository) {
	t.Helper()
	renderer, err := webviews.NewRenderer()
	if err != nil {
		t.Fatal(err)
	}
	protector, err := security.New([]byte("a-test-secret-that-is-at-least-thirty-two-bytes"))
	if err != nil {
		t.Fatal(err)
	}
	repository := &fakeRepository{}
	server := NewServer(repository, renderer, realtime.NewHub(), protector, slog.New(slog.NewTextHandler(io.Discard, nil)))
	server.now = func() time.Time { return time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC) }
	return server, repository
}

func TestPageShellSetsSecurityCookiesAndHeaders(t *testing.T) {
	server, _ := newTestServer(t)
	request := httptest.NewRequest(http.MethodGet, "https://localhost/patients", nil)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	cookies := response.Result().Cookies()
	if findCookie(cookies, security.SessionCookieName) == nil || findCookie(cookies, security.CSRFCookieName) == nil {
		t.Fatalf("security cookies not set: %#v", cookies)
	}
	if got := response.Header().Get("Content-Security-Policy"); !strings.Contains(got, "frame-ancestors 'none'") {
		t.Fatalf("Content-Security-Policy = %q", got)
	}
	if !strings.Contains(response.Body.String(), "data-signals:csrf") {
		t.Fatal("shell does not initialize the CSRF signal")
	}
}

func TestServesCheckedPatientNavigationUtility(t *testing.T) {
	server, _ := newTestServer(t)
	request := httptest.NewRequest(http.MethodGet, "https://localhost/assets/patient-navigation.js", nil)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if got := response.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/javascript") {
		t.Fatalf("Content-Type = %q", got)
	}
	if !strings.Contains(response.Body.String(), "mousedown") || !strings.Contains(response.Body.String(), "data-patient-navigation") {
		t.Fatal("navigation utility does not contain delegated mousedown behavior")
	}
}

func TestCreatePatientCommandUsesDoubleSubmitToken(t *testing.T) {
	server, repository := newTestServer(t)

	pageRequest := httptest.NewRequest(http.MethodGet, "https://localhost/patients", nil)
	pageResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(pageResponse, pageRequest)
	sid := findCookie(pageResponse.Result().Cookies(), security.SessionCookieName)
	csrf := findCookie(pageResponse.Result().Cookies(), security.CSRFCookieName)

	body := `{"csrf":"` + csrf.Value + `","patientName":"Avery Stone","patientDob":"1991-02-03","patientPronouns":"they/them","patientCareTeam":"Dr. Rivera"}`
	request := httptest.NewRequest(http.MethodPost, "https://localhost/commands?command=create-patient", strings.NewReader(body))
	request.Host = "localhost"
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "https://localhost")
	request.AddCookie(sid)
	request.AddCookie(csrf)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if repository.createdPatient.Name != "Avery Stone" {
		t.Fatalf("created patient = %#v", repository.createdPatient)
	}
	if !strings.Contains(response.Body.String(), "datastar-patch-signals") {
		t.Fatalf("response did not patch ephemeral signals: %s", response.Body.String())
	}
}

func TestCommandRejectsMissingSession(t *testing.T) {
	server, _ := newTestServer(t)
	request := httptest.NewRequest(http.MethodPost, "https://localhost/commands?command=create-patient", strings.NewReader(`{}`))
	request.Header.Set("Origin", "https://localhost")
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", response.Code)
	}
}

func findCookie(cookies []*http.Cookie, name string) *http.Cookie {
	for _, cookie := range cookies {
		if cookie.Name == name {
			return cookie
		}
	}
	return nil
}
