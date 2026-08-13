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
	if !strings.Contains(shell, "__Host-csrf") || !strings.Contains(shell, "@post($_pageUrl") || !strings.Contains(shell, `data-effect=`) {
		t.Fatalf("shell does not initialize the secured update stream: %s", shell)
	}
}
