package web

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/preston/go-datastar-patient-dashboard/internal/domain"
)

//go:embed templates/*.gohtml assets/*.js
var files embed.FS

type Renderer struct {
	templates *template.Template
}

type ShellData struct {
	Title   string
	Page    string
	Signals string
}

type DashboardData struct {
	domain.DashboardSnapshot
	DraftSignals string
	Today        time.Time
}

type DueData struct {
	domain.DueSnapshot
	Today time.Time
}

func NewRenderer() (*Renderer, error) {
	functions := template.FuncMap{
		"initials":   initials,
		"tone":       tone,
		"selected":   selected,
		"formatDate": func(value time.Time) string { return value.Format("Jan 2, 2006") },
		"isoDate":    func(value time.Time) string { return value.Format(time.DateOnly) },
		"dueClass":   dueClass,
		"dueLabel":   dueLabel,
	}
	templates, err := template.New("views").Funcs(functions).ParseFS(files, "templates/*.gohtml")
	if err != nil {
		return nil, fmt.Errorf("parse views: %w", err)
	}
	return &Renderer{templates: templates}, nil
}

func (r *Renderer) Shell(title, page string) (string, error) {
	signals, err := json.Marshal(map[string]any{
		"patientName":       "",
		"patientDob":        "",
		"patientPronouns":   "",
		"patientCareTeam":   "",
		"taskTitle":         "",
		"taskDue":           "",
		"taskPriority":      "routine",
		"taskDraftRevision": 0,
		"_draftPatientId":   "",
		"_flash":            "",
		"_error":            "",
	})
	if err != nil {
		return "", fmt.Errorf("marshal initial signals: %w", err)
	}
	return r.execute("shell", ShellData{Title: title, Page: page, Signals: string(signals)})
}

func (r *Renderer) Dashboard(snapshot domain.DashboardSnapshot, today time.Time) (string, error) {
	draft := snapshot.TaskDraft
	if draft.Priority == "" {
		draft.Priority = "routine"
	}
	draftSignals, err := json.Marshal(map[string]any{
		"patientId": draft.PatientID,
		"title":     draft.Title,
		"due":       draft.DueDate,
		"priority":  draft.Priority,
		"revision":  draft.Revision,
	})
	if err != nil {
		return "", fmt.Errorf("marshal task draft signals: %w", err)
	}
	return r.execute("dashboard-view", DashboardData{
		DashboardSnapshot: snapshot,
		DraftSignals:      string(draftSignals),
		Today:             today,
	})
}

func (r *Renderer) Due(snapshot domain.DueSnapshot, today time.Time) (string, error) {
	return r.execute("due-view", DueData{DueSnapshot: snapshot, Today: today})
}

func (r *Renderer) Asset(name string) ([]byte, error) {
	return files.ReadFile("assets/" + name)
}

func (r *Renderer) execute(name string, data any) (string, error) {
	var output bytes.Buffer
	if err := r.templates.ExecuteTemplate(&output, name, data); err != nil {
		return "", fmt.Errorf("execute %s: %w", name, err)
	}
	return output.String(), nil
}

func initials(name string) string {
	parts := strings.Fields(name)
	if len(parts) == 0 {
		return "?"
	}
	first, _ := utf8.DecodeRuneInString(parts[0])
	result := string(first)
	if len(parts) > 1 {
		last, _ := utf8.DecodeRuneInString(parts[len(parts)-1])
		result += string(last)
	}
	return strings.ToUpper(result)
}

func tone(value string) string {
	tones := []string{"sage", "clay", "gold", "sky"}
	var hash uint32 = 2166136261
	for _, b := range []byte(value) {
		hash ^= uint32(b)
		hash *= 16777619
	}
	return tones[int(hash)%len(tones)]
}

func selected(patient *domain.Patient, id string) bool {
	return patient != nil && patient.ID == id
}

func dueClass(due, today time.Time) string {
	if dateOnly(due).Before(dateOnly(today)) {
		return "overdue"
	}
	return ""
}

func dueLabel(due, today time.Time) string {
	days := int(dateOnly(due).Sub(dateOnly(today)).Hours() / 24)
	switch days {
	case -1:
		return "Yesterday"
	case 0:
		return "Today"
	case 1:
		return "Tomorrow"
	default:
		if days < 0 {
			return fmt.Sprintf("%d days overdue", -days)
		}
		return fmt.Sprintf("In %d days", days)
	}
}

func dateOnly(value time.Time) time.Time {
	year, month, day := value.Date()
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
}
