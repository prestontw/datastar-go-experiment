package postgres

import (
	"testing"

	"github.com/preston/go-datastar-patient-dashboard/internal/domain"
)

func TestSelectDashboardPatient(t *testing.T) {
	patients := []domain.Patient{
		{ID: "elias", Name: "Elias Brooks"},
		{ID: "maya", Name: "Maya Chen"},
		{ID: "noor", Name: "Noor Ahmed"},
	}

	tests := []struct {
		name   string
		query  domain.DashboardQuery
		input  []domain.Patient
		wantID string
	}{
		{
			name:   "browsing defaults to first patient",
			query:  domain.DashboardQuery{},
			input:  patients,
			wantID: "elias",
		},
		{
			name:   "browsing retains explicit patient",
			query:  domain.DashboardQuery{PatientID: "noor"},
			input:  patients,
			wantID: "noor",
		},
		{
			name:  "search starts without patient context",
			query: domain.DashboardQuery{Search: "Noor"},
			input: patients[2:],
		},
		{
			name:   "search result can be explicitly selected",
			query:  domain.DashboardQuery{PatientID: "noor", Search: "Noor"},
			input:  patients[2:],
			wantID: "noor",
		},
		{
			name:  "patient outside filtered results is not selected",
			query: domain.DashboardQuery{PatientID: "maya", Search: "Noor"},
			input: patients[2:],
		},
		{
			name:  "empty picker has no selection",
			query: domain.DashboardQuery{},
			input: nil,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			selected := selectDashboardPatient(test.input, test.query)
			if test.wantID == "" {
				if selected != nil {
					t.Fatalf("selected patient = %q, want nil", selected.ID)
				}
				return
			}
			if selected == nil || selected.ID != test.wantID {
				t.Fatalf("selected patient = %#v, want ID %q", selected, test.wantID)
			}
		})
	}
}
