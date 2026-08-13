package app

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/starfederation/datastar-go/datastar"

	"github.com/preston/go-datastar-patient-dashboard/internal/domain"
	"github.com/preston/go-datastar-patient-dashboard/internal/realtime"
	"github.com/preston/go-datastar-patient-dashboard/internal/security"
	webviews "github.com/preston/go-datastar-patient-dashboard/internal/web"
)

const (
	maxSignalBody  = 64 << 10
	renderInterval = 100 * time.Millisecond
)

type Repository interface {
	Ping(context.Context) error
	Dashboard(context.Context, domain.DashboardQuery) (domain.DashboardSnapshot, error)
	Due(context.Context, domain.DueQuery) (domain.DueSnapshot, error)
	CreatePatient(context.Context, domain.NewPatient) error
	CreateTask(context.Context, domain.NewTask) error
	ToggleTask(context.Context, string) error
}

type Server struct {
	repository Repository
	renderer   *webviews.Renderer
	hub        *realtime.Hub
	protector  *security.Protector
	logger     *slog.Logger
	now        func() time.Time
}

type Signals struct {
	CSRF            string `json:"csrf"`
	TabID           string `json:"tabId"`
	PatientName     string `json:"patientName"`
	PatientDOB      string `json:"patientDob"`
	PatientPronouns string `json:"patientPronouns"`
	PatientCareTeam string `json:"patientCareTeam"`
	TaskTitle       string `json:"taskTitle"`
	TaskDue         string `json:"taskDue"`
	TaskPriority    string `json:"taskPriority"`
}

func NewServer(repository Repository, renderer *webviews.Renderer, hub *realtime.Hub, protector *security.Protector, logger *slog.Logger) *Server {
	return &Server{
		repository: repository,
		renderer:   renderer,
		hub:        hub,
		protector:  protector,
		logger:     logger,
		now:        time.Now,
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/patients", http.StatusSeeOther)
	})
	mux.HandleFunc("/patients", s.patientsPage)
	mux.HandleFunc("/tasks/due", s.duePage)
	mux.HandleFunc("POST /commands", s.command)
	mux.HandleFunc("GET /assets/{name}", s.javaScriptAsset)
	mux.HandleFunc("GET /healthz", s.health)

	return security.Headers(s.recover(mux))
}

func (s *Server) patientsPage(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.serveShell(w, r, "Patient dashboard", "patients")
	case http.MethodPost:
		query := dashboardQuery(r)
		s.serveUpdates(w, r, func(ctx context.Context) (string, error) {
			snapshot, err := s.repository.Dashboard(ctx, query)
			if err != nil {
				return "", err
			}
			return s.renderer.Dashboard(snapshot, s.today())
		})
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) duePage(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.serveShell(w, r, "Due tasks", "due")
	case http.MethodPost:
		query := dueQuery(r)
		s.serveUpdates(w, r, func(ctx context.Context) (string, error) {
			snapshot, err := s.repository.Due(ctx, query)
			if err != nil {
				return "", err
			}
			return s.renderer.Due(snapshot, s.today())
		})
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) serveShell(w http.ResponseWriter, r *http.Request, title, page string) {
	sid, ok := security.SessionID(r)
	if !ok {
		var err error
		sid, err = security.NewSessionID()
		if err != nil {
			s.internalError(w, r, err)
			return
		}
		security.SetSessionCookie(w, sid)
	}
	if _, err := s.protector.SetCSRFToken(w, sid); err != nil {
		s.internalError(w, r, err)
		return
	}

	body, err := s.renderer.Shell(title, page)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	etag := fmt.Sprintf(`"%x"`, shortDigest(body))
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "no-cache, must-revalidate")
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(body))
}

func (s *Server) serveUpdates(w http.ResponseWriter, r *http.Request, render func(context.Context) (string, error)) {
	_, _, ok := s.authorizeSignals(w, r)
	if !ok {
		return
	}

	events, unsubscribe := s.hub.Subscribe()
	defer unsubscribe()

	initialView, err := render(r.Context())
	if err != nil {
		s.internalError(w, r, fmt.Errorf("initial page render: %w", err))
		return
	}

	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Vary", "Accept-Encoding")
	sse := datastar.NewSSE(w, r, datastar.WithCompression(
		datastar.WithServerPriority(),
		datastar.WithBrotli(datastar.WithBrotliLevel(4), datastar.WithBrotliLGWin(18)),
		datastar.WithGzip(),
	))
	if err := sse.PatchElements(initialView); err != nil {
		s.logger.Debug("initial SSE render ended", "error", err, "path", r.URL.Path)
		return
	}

	lastRender := time.Now()
	var timer *time.Timer
	var timerC <-chan time.Time
	for {
		select {
		case _, open := <-events:
			if !open {
				return
			}
			if timerC != nil {
				continue
			}
			wait := renderInterval - time.Since(lastRender)
			if wait < 0 {
				wait = 0
			}
			timer = time.NewTimer(wait)
			timerC = timer.C
		case <-timerC:
			timerC = nil
			timer = nil
			view, err := render(r.Context())
			if err != nil {
				s.logger.Error("realtime page render failed", "error", err, "path", r.URL.Path)
				continue
			}
			if err := sse.PatchElements(view); err != nil {
				s.logger.Debug("SSE connection ended", "error", err, "path", r.URL.Path)
				return
			}
			lastRender = time.Now()
		case <-r.Context().Done():
			if timer != nil {
				timer.Stop()
			}
			return
		}
	}
}

func (s *Server) command(w http.ResponseWriter, r *http.Request) {
	signals, _, ok := s.authorizeSignals(w, r)
	if !ok {
		return
	}

	command := r.URL.Query().Get("command")
	switch command {
	case "create-patient":
		id, err := newUUID()
		if err != nil {
			s.commandError(w, r, err)
			return
		}
		patient, err := domain.PreparePatient(domain.CreatePatientInput{
			Name:        signals.PatientName,
			DateOfBirth: signals.PatientDOB,
			Pronouns:    signals.PatientPronouns,
			CareTeam:    signals.PatientCareTeam,
		}, id, s.today())
		if err != nil {
			s.validationError(w, r, err)
			return
		}
		if err := s.repository.CreatePatient(r.Context(), patient); err != nil {
			s.commandError(w, r, err)
			return
		}
		s.patchSignals(w, r, map[string]any{
			"patientName": "", "patientDob": "", "patientPronouns": "", "patientCareTeam": "",
			"_error": "", "_flash": "Patient created. All connected views are refreshing.",
		})

	case "create-task":
		id, err := newUUID()
		if err != nil {
			s.commandError(w, r, err)
			return
		}
		task, err := domain.PrepareTask(domain.CreateTaskInput{
			PatientID: r.URL.Query().Get("patient"),
			Title:     signals.TaskTitle,
			DueDate:   signals.TaskDue,
			Priority:  signals.TaskPriority,
		}, id)
		if err != nil {
			s.validationError(w, r, err)
			return
		}
		if err := s.repository.CreateTask(r.Context(), task); err != nil {
			s.commandError(w, r, err)
			return
		}
		s.patchSignals(w, r, map[string]any{
			"taskTitle": "", "taskDue": "", "taskPriority": "routine",
			"_error": "", "_flash": "Task created.",
		})

	case "toggle-task":
		taskID := r.URL.Query().Get("task")
		if !validUUID(taskID) {
			s.validationError(w, r, errors.Join(domain.ErrInvalidCommand, errors.New("task ID is invalid")))
			return
		}
		if err := s.repository.ToggleTask(r.Context(), taskID); err != nil {
			s.commandError(w, r, err)
			return
		}
		s.patchSignals(w, r, map[string]any{"_error": "", "_flash": "Task status updated."})

	default:
		http.Error(w, "unknown command", http.StatusBadRequest)
	}
}

func (s *Server) authorizeSignals(w http.ResponseWriter, r *http.Request) (Signals, string, bool) {
	sid, ok := security.SessionID(r)
	if !ok {
		http.Error(w, "forbidden", http.StatusForbidden)
		return Signals{}, "", false
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxSignalBody)
	var signals Signals
	if err := datastar.ReadSignals(r, &signals); err != nil {
		http.Error(w, "invalid signals", http.StatusBadRequest)
		return Signals{}, "", false
	}
	if err := s.protector.ValidateUnsafeRequest(r, sid, signals.CSRF); err != nil {
		http.Error(w, "forbidden", http.StatusForbidden)
		return Signals{}, "", false
	}
	return signals, sid, true
}

func (s *Server) patchSignals(w http.ResponseWriter, r *http.Request, signals map[string]any) {
	w.Header().Set("Cache-Control", "no-store")
	sse := datastar.NewSSE(w, r)
	if err := sse.MarshalAndPatchSignals(signals); err != nil {
		s.logger.Debug("patch command signals", "error", err)
	}
}

func (s *Server) validationError(w http.ResponseWriter, r *http.Request, err error) {
	message := strings.TrimPrefix(err.Error(), domain.ErrInvalidCommand.Error()+"\n")
	message = strings.TrimPrefix(message, domain.ErrInvalidCommand.Error()+": ")
	s.patchSignals(w, r, map[string]any{"_error": message, "_flash": ""})
}

func (s *Server) commandError(w http.ResponseWriter, r *http.Request, err error) {
	s.logger.Error("command failed", "error", err, "command", r.URL.Query().Get("command"))
	s.patchSignals(w, r, map[string]any{"_error": "The change could not be saved. Please try again.", "_flash": ""})
}

func (s *Server) javaScriptAsset(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name != "patient-avatar.js" && name != "patient-navigation.js" {
		http.NotFound(w, r)
		return
	}
	asset, err := s.renderer.Asset(name)
	if err != nil {
		s.internalError(w, r, err)
		return
	}

	etag := fmt.Sprintf(`"%x"`, shortDigest(string(asset)))
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "no-cache")
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	_, _ = w.Write(asset)
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.repository.Ping(ctx); err != nil {
		http.Error(w, "database unavailable", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("ok\n"))
}

func (s *Server) recover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if value := recover(); value != nil {
				s.logger.Error("panic serving request", "panic", value, "method", r.Method, "path", r.URL.Path)
				http.Error(w, "internal server error", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func (s *Server) internalError(w http.ResponseWriter, r *http.Request, err error) {
	s.logger.Error("request failed", "error", err, "method", r.Method, "path", r.URL.Path)
	http.Error(w, "internal server error", http.StatusInternalServerError)
}

func (s *Server) today() time.Time {
	now := s.now().UTC()
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
}

func dashboardQuery(r *http.Request) domain.DashboardQuery {
	status := r.URL.Query().Get("status")
	switch status {
	case "all", "done", "open":
	default:
		status = "open"
	}
	search := strings.TrimSpace(r.URL.Query().Get("q"))
	if len(search) > 80 {
		search = search[:80]
	}
	return domain.DashboardQuery{
		PatientID: r.URL.Query().Get("patient"),
		Status:    status,
		Search:    search,
	}
}

func dueQuery(r *http.Request) domain.DueQuery {
	days, _ := strconv.Atoi(r.URL.Query().Get("window"))
	switch days {
	case 7, 14, 30:
	default:
		days = 14
	}
	return domain.DueQuery{Days: days}
}

func newUUID() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("generate UUID: %w", err)
	}
	value[6] = (value[6] & 0x0f) | 0x40
	value[8] = (value[8] & 0x3f) | 0x80
	encoded := make([]byte, 36)
	hex.Encode(encoded[0:8], value[0:4])
	encoded[8] = '-'
	hex.Encode(encoded[9:13], value[4:6])
	encoded[13] = '-'
	hex.Encode(encoded[14:18], value[6:8])
	encoded[18] = '-'
	hex.Encode(encoded[19:23], value[8:10])
	encoded[23] = '-'
	hex.Encode(encoded[24:36], value[10:16])
	return string(encoded), nil
}

func validUUID(value string) bool {
	if len(value) != 36 || value[8] != '-' || value[13] != '-' || value[18] != '-' || value[23] != '-' {
		return false
	}
	_, err := hex.DecodeString(strings.ReplaceAll(value, "-", ""))
	return err == nil
}

func shortDigest(value string) []byte {
	digest := sha256.Sum256([]byte(value))
	return digest[:12]
}
