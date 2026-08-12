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

// PreparePatient is part of the functional core: all values and coeffects are
// supplied by the caller, and the result only describes the desired write.
func PreparePatient(input CreatePatientInput, id string, today time.Time) (NewPatient, error) {
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
		ID:          id,
		Name:        name,
		DateOfBirth: dateOfBirth,
		Pronouns:    pronouns,
		CareTeam:    careTeam,
	}, nil
}

// PrepareTask is pure for the same reason as PreparePatient. The shell owns ID
// generation, time, database access, and effects.
func PrepareTask(input CreateTaskInput, id string) (NewTask, error) {
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
		ID:        id,
		PatientID: input.PatientID,
		Title:     title,
		DueDate:   dueDate,
		Priority:  priority,
	}, nil
}
