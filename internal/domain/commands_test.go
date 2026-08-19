package domain

import (
	"errors"
	"testing"
	"time"
)

func TestPreparePatient(t *testing.T) {
	today := time.Date(2026, time.August, 11, 0, 0, 0, 0, time.UTC)
	patient, err := PreparePatient(CreatePatientInput{
		Name:        "  Avery Stone  ",
		DateOfBirth: "1991-02-03",
		Pronouns:    " they/them ",
		CareTeam:    " Dr. Rivera ",
	}, today)
	if err != nil {
		t.Fatalf("PreparePatient() error = %v", err)
	}
	if patient.Name != "Avery Stone" || patient.Pronouns != "they/them" {
		t.Fatalf("PreparePatient() did not normalize values: %#v", patient)
	}
}

func TestPreparePatientRejectsFutureBirthDate(t *testing.T) {
	_, err := PreparePatient(CreatePatientInput{Name: "Avery", DateOfBirth: "2027-01-01"}, time.Date(2026, 8, 11, 0, 0, 0, 0, time.UTC))
	if !errors.Is(err, ErrInvalidCommand) {
		t.Fatalf("PreparePatient() error = %v, want ErrInvalidCommand", err)
	}
}

func TestPrepareTaskDraftAllowsIncompleteInput(t *testing.T) {
	draft, err := PrepareTaskDraft(SaveTaskDraftInput{
		PatientID: "patient-id",
		Title:     " Call patient ",
		Revision:  3,
	})
	if err != nil {
		t.Fatal(err)
	}
	if draft.Title != " Call patient " || draft.Priority != "routine" || draft.Revision != 3 {
		t.Fatalf("PrepareTaskDraft() = %#v", draft)
	}
}

func TestPrepareTaskDefaultsPriority(t *testing.T) {
	task, err := PrepareTask(CreateTaskInput{PatientID: "patient-id", Title: " Call patient ", DueDate: "2026-08-12"})
	if err != nil {
		t.Fatalf("PrepareTask() error = %v", err)
	}
	if task.Title != "Call patient" || task.Priority != "routine" {
		t.Fatalf("PrepareTask() = %#v", task)
	}
}
