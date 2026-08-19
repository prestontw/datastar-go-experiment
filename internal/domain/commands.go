package domain

import (
	"errors"
	"strings"
	"time"
)

var ErrInvalidCommand = errors.New("invalid command")

type CreatePatientInput struct {
	Name        string
	DateOfBirth string
	Pronouns    string
	CareTeam    string
}

type CreateTaskInput struct {
	PatientID string
	Title     string
	DueDate   string
	Priority  string
}

type SaveTaskDraftInput struct {
	PatientID string
	Title     string
	DueDate   string
	Priority  string
	Revision  int64
}

// PreparePatient is part of the functional core: the current date is supplied
// by the caller, and the result describes attributes to persist. PostgreSQL
// assigns the durable identity when the shell inserts the record.
func PreparePatient(input CreatePatientInput, today time.Time) (NewPatient, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" || len(name) > 120 {
		return NewPatient{}, errors.Join(ErrInvalidCommand, errors.New("name must be between 1 and 120 characters"))
	}

	dateOfBirth, err := time.Parse(time.DateOnly, input.DateOfBirth)
	if err != nil || dateOfBirth.After(today) {
		return NewPatient{}, errors.Join(ErrInvalidCommand, errors.New("date of birth must be a valid past date"))
	}

	pronouns := strings.TrimSpace(input.Pronouns)
	if len(pronouns) > 60 {
		return NewPatient{}, errors.Join(ErrInvalidCommand, errors.New("pronouns must be 60 characters or fewer"))
	}
	careTeam := strings.TrimSpace(input.CareTeam)
	if len(careTeam) > 120 {
		return NewPatient{}, errors.Join(ErrInvalidCommand, errors.New("care team must be 120 characters or fewer"))
	}

	return NewPatient{
		Name:        name,
		DateOfBirth: dateOfBirth,
		Pronouns:    pronouns,
		CareTeam:    careTeam,
	}, nil
}

// PrepareTaskDraft preserves partial user input while enforcing the database's
// bounded shape. Drafts are intentionally allowed to be incomplete.
func PrepareTaskDraft(input SaveTaskDraftInput) (TaskDraft, error) {
	if strings.TrimSpace(input.PatientID) == "" {
		return TaskDraft{}, errors.Join(ErrInvalidCommand, errors.New("a patient is required"))
	}
	if len(input.Title) > 180 {
		return TaskDraft{}, errors.Join(ErrInvalidCommand, errors.New("task title must be 180 characters or fewer"))
	}
	if input.DueDate != "" {
		if _, err := time.Parse(time.DateOnly, input.DueDate); err != nil {
			return TaskDraft{}, errors.Join(ErrInvalidCommand, errors.New("draft due date is invalid"))
		}
	}
	priority := strings.ToLower(strings.TrimSpace(input.Priority))
	switch priority {
	case "routine", "important", "urgent":
	case "":
		priority = "routine"
	default:
		return TaskDraft{}, errors.Join(ErrInvalidCommand, errors.New("priority is not recognized"))
	}
	if input.Revision < 0 {
		return TaskDraft{}, errors.Join(ErrInvalidCommand, errors.New("draft revision is invalid"))
	}
	return TaskDraft{
		PatientID: input.PatientID,
		Title:     input.Title,
		DueDate:   input.DueDate,
		Priority:  priority,
		Revision:  input.Revision,
	}, nil
}

// PrepareTask is pure for the same reason as PreparePatient. PostgreSQL assigns
// the task ID when the shell persists these validated attributes.
func PrepareTask(input CreateTaskInput) (NewTask, error) {
	title := strings.TrimSpace(input.Title)
	if title == "" || len(title) > 180 {
		return NewTask{}, errors.Join(ErrInvalidCommand, errors.New("task title must be between 1 and 180 characters"))
	}
	if strings.TrimSpace(input.PatientID) == "" {
		return NewTask{}, errors.Join(ErrInvalidCommand, errors.New("a patient is required"))
	}

	dueDate, err := time.Parse(time.DateOnly, input.DueDate)
	if err != nil {
		return NewTask{}, errors.Join(ErrInvalidCommand, errors.New("due date is required"))
	}

	priority := strings.ToLower(strings.TrimSpace(input.Priority))
	switch priority {
	case "routine", "important", "urgent":
	case "":
		priority = "routine"
	default:
		return NewTask{}, errors.Join(ErrInvalidCommand, errors.New("priority is not recognized"))
	}

	return NewTask{
		PatientID: input.PatientID,
		Title:     title,
		DueDate:   dueDate,
		Priority:  priority,
	}, nil
}
