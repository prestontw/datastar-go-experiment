package web

import (
	"strings"
	"testing"
	"time"

	"github.com/preston/go-datastar-patient-dashboard/internal/domain"
)

func TestRendererProducesOneMainMorphAndEscapesPatientData(t *testing.T) {
	renderer, err := NewRenderer()
	if err != nil {
		t.Fatal(err)
	}
	today := time.Date(2026, 8, 11, 0, 0, 0, 0, time.UTC)
	patient := domain.Patient{ID: "patient-id", Name: `<img src=x onerror="alert(1)">`, DateOfBirth: today}
	view, err := renderer.Dashboard(domain.DashboardSnapshot{
		Patients:        []domain.Patient{patient},
		SelectedPatient: &patient,
		Query:           domain.DashboardQuery{Status: "open"},
	}, today)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(view, `<main id="morph"`) {
		t.Fatal("dashboard did not render the full main morph target")
	}
	if strings.Contains(view, `<img src=x`) || !strings.Contains(view, `&lt;img`) {
		t.Fatalf("patient data was not escaped: %s", view)
	}
}

func TestShellReadsCSRFDoubleSubmitCookie(t *testing.T) {
	renderer, err := NewRenderer()
	if err != nil {
		t.Fatal(err)
	}
	shell, err := renderer.Shell("Patients", "patients")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(shell, "__Host-csrf") || !strings.Contains(shell, "@post(window.location.pathname") || !strings.Contains(shell, `data-effect=`) {
		t.Fatalf("shell does not initialize the secured update stream from the canonical browser URL: %s", shell)
	}
	if strings.Contains(shell, "_pageUrl") {
		t.Fatal("shell duplicates the canonical browser URL in a signal")
	}
	if !strings.Contains(shell, "new AbortController()") || strings.Contains(shell, "requestCancellation: 'cleanup'") {
		t.Fatal("shell does not explicitly own page-stream cancellation")
	}
	if !strings.Contains(shell, "data-signals:page-stream-id") || !strings.Contains(shell, "$pageStreamRevision") {
		t.Fatal("shell does not send server-verifiable page-stream identity")
	}
	if !strings.Contains(shell, `class="notifications"`) || !strings.Contains(shell, `position: fixed`) {
		t.Fatal("shell does not render notifications outside page layout")
	}
	if !strings.Contains(shell, `aria-label="Dismiss notification"`) || !strings.Contains(shell, `$_flash = ''`) {
		t.Fatal("shell notifications cannot be dismissed")
	}
}
