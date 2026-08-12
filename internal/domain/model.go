package domain

import "time"

type Patient struct {
	ID          string
	Name        string
	DateOfBirth time.Time
	Pronouns    string
	CareTeam    string
	OpenTasks   int
	TotalTasks  int
}

type Task struct {
	ID          string
	PatientID   string
	PatientName string
	Title       string
	DueDate     time.Time
	Status      string
	Priority    string
	CreatedAt   time.Time
	CompletedAt *time.Time
}

type DashboardQuery struct {
	PatientID string
	Status    string
	Search    string
}

type DashboardSnapshot struct {
	Patients        []Patient
	SelectedPatient *Patient
	Tasks           []Task
	Query           DashboardQuery
	OpenTaskCount   int
	DueTaskCount    int
}

type DueQuery struct {
	Days int
}

type DueSnapshot struct {
	Tasks        []Task
	Query        DueQuery
	OverdueCount int
	DueSoonCount int
}

type NewPatient struct {
	ID          string
	Name        string
	DateOfBirth time.Time
	Pronouns    string
	CareTeam    string
}

type NewTask struct {
	ID        string
	PatientID string
	Title     string
	DueDate   time.Time
	Priority  string
}
