package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/lib/pq"

	"github.com/preston/go-datastar-patient-dashboard/internal/domain"
)

type Store struct {
	db *sql.DB
}

func Open(ctx context.Context, databaseURL string) (*Store, error) {
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}
	db.SetMaxOpenConns(12)
	db.SetMaxIdleConns(6)
	db.SetConnMaxLifetime(30 * time.Minute)

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) Ping(ctx context.Context) error { return s.db.PingContext(ctx) }

func (s *Store) Dashboard(ctx context.Context, query domain.DashboardQuery) (domain.DashboardSnapshot, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return domain.DashboardSnapshot{}, fmt.Errorf("begin dashboard snapshot: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	patients, err := listPatients(ctx, tx, query.Search)
	if err != nil {
		return domain.DashboardSnapshot{}, err
	}

	selected := selectDashboardPatient(patients, query)

	var tasks []domain.Task
	if selected != nil {
		tasks, err = listPatientTasks(ctx, tx, selected.ID, query.Status)
		if err != nil {
			return domain.DashboardSnapshot{}, err
		}
	}

	var openCount, dueCount int
	if err := tx.QueryRowContext(ctx, `
		SELECT
			count(*) FILTER (WHERE status = 'open'),
			count(*) FILTER (WHERE status = 'open' AND due_date <= CURRENT_DATE + 7)
		FROM patient_tasks`).Scan(&openCount, &dueCount); err != nil {
		return domain.DashboardSnapshot{}, fmt.Errorf("query dashboard totals: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return domain.DashboardSnapshot{}, fmt.Errorf("commit dashboard snapshot: %w", err)
	}

	return domain.DashboardSnapshot{
		Patients:        patients,
		SelectedPatient: selected,
		Tasks:           tasks,
		Query:           query,
		OpenTaskCount:   openCount,
		DueTaskCount:    dueCount,
	}, nil
}

// selectDashboardPatient is the pure selection policy for a filtered picker.
// An explicit patient is selected only when present in the current results;
// browsing without a search defaults to the first patient; searching without
// an explicit matching choice leaves the task pane empty.
func selectDashboardPatient(patients []domain.Patient, query domain.DashboardQuery) *domain.Patient {
	for i := range patients {
		if patients[i].ID == query.PatientID {
			return &patients[i]
		}
	}
	if query.Search == "" && len(patients) > 0 {
		return &patients[0]
	}
	return nil
}

func listPatients(ctx context.Context, tx *sql.Tx, search string) ([]domain.Patient, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT p.id, p.name, p.date_of_birth, p.pronouns, p.care_team,
			count(t.id) FILTER (WHERE t.status = 'open') AS open_tasks,
			count(t.id) AS total_tasks
		FROM patients p
		LEFT JOIN patient_tasks t ON t.patient_id = p.id
		WHERE p.name ILIKE '%' || $1 || '%'
		GROUP BY p.id
		ORDER BY lower(p.name), p.id`, search)
	if err != nil {
		return nil, fmt.Errorf("query patients: %w", err)
	}
	defer rows.Close()

	patients := make([]domain.Patient, 0)
	for rows.Next() {
		var patient domain.Patient
		if err := rows.Scan(
			&patient.ID,
			&patient.Name,
			&patient.DateOfBirth,
			&patient.Pronouns,
			&patient.CareTeam,
			&patient.OpenTasks,
			&patient.TotalTasks,
		); err != nil {
			return nil, fmt.Errorf("scan patient: %w", err)
		}
		patients = append(patients, patient)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate patients: %w", err)
	}
	return patients, nil
}

func listPatientTasks(ctx context.Context, tx *sql.Tx, patientID, status string) ([]domain.Task, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT id, patient_id, title, due_date, status, priority, created_at, completed_at
		FROM patient_tasks
		WHERE patient_id = $1 AND ($2 = 'all' OR status = $2)
		ORDER BY status, due_date, created_at`, patientID, status)
	if err != nil {
		return nil, fmt.Errorf("query patient tasks: %w", err)
	}
	defer rows.Close()

	tasks := make([]domain.Task, 0)
	for rows.Next() {
		var task domain.Task
		var completedAt sql.NullTime
		if err := rows.Scan(
			&task.ID,
			&task.PatientID,
			&task.Title,
			&task.DueDate,
			&task.Status,
			&task.Priority,
			&task.CreatedAt,
			&completedAt,
		); err != nil {
			return nil, fmt.Errorf("scan task: %w", err)
		}
		if completedAt.Valid {
			task.CompletedAt = &completedAt.Time
		}
		tasks = append(tasks, task)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate patient tasks: %w", err)
	}
	return tasks, nil
}

func (s *Store) Due(ctx context.Context, query domain.DueQuery) (domain.DueSnapshot, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT t.id, t.patient_id, p.name, t.title, t.due_date, t.status,
			t.priority, t.created_at, t.completed_at
		FROM patient_tasks t
		JOIN patients p ON p.id = t.patient_id
		WHERE t.status = 'open' AND t.due_date <= CURRENT_DATE + $1::integer
		ORDER BY t.due_date, lower(p.name), t.created_at`, query.Days)
	if err != nil {
		return domain.DueSnapshot{}, fmt.Errorf("query due tasks: %w", err)
	}
	defer rows.Close()

	today := time.Now().UTC().Truncate(24 * time.Hour)
	tasks := make([]domain.Task, 0)
	overdue := 0
	for rows.Next() {
		var task domain.Task
		var completedAt sql.NullTime
		if err := rows.Scan(
			&task.ID,
			&task.PatientID,
			&task.PatientName,
			&task.Title,
			&task.DueDate,
			&task.Status,
			&task.Priority,
			&task.CreatedAt,
			&completedAt,
		); err != nil {
			return domain.DueSnapshot{}, fmt.Errorf("scan due task: %w", err)
		}
		if task.DueDate.Before(today) {
			overdue++
		}
		tasks = append(tasks, task)
	}
	if err := rows.Err(); err != nil {
		return domain.DueSnapshot{}, fmt.Errorf("iterate due tasks: %w", err)
	}
	return domain.DueSnapshot{
		Tasks:        tasks,
		Query:        query,
		OverdueCount: overdue,
		DueSoonCount: len(tasks) - overdue,
	}, nil
}

func (s *Store) CreatePatient(ctx context.Context, patient domain.NewPatient) (string, error) {
	var id string
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO patients (name, date_of_birth, pronouns, care_team)
		VALUES ($1, $2, $3, $4)
		RETURNING id`,
		patient.Name, patient.DateOfBirth, patient.Pronouns, patient.CareTeam,
	).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("insert patient: %w", err)
	}
	return id, nil
}

func (s *Store) TaskDraft(ctx context.Context, ownerHash []byte, tabID, patientID string) (domain.TaskDraft, error) {
	draft := domain.TaskDraft{PatientID: patientID, Priority: "routine"}
	err := s.db.QueryRowContext(ctx, `
		SELECT title, COALESCE(due_date::text, ''), priority, revision
		FROM task_drafts
		WHERE session_id_hash = $1 AND tab_id = $2 AND patient_id = $3`,
		ownerHash, tabID, patientID,
	).Scan(&draft.Title, &draft.DueDate, &draft.Priority, &draft.Revision)
	if errors.Is(err, sql.ErrNoRows) {
		return draft, nil
	}
	if err != nil {
		return domain.TaskDraft{}, fmt.Errorf("query task draft: %w", err)
	}
	return draft, nil
}

func (s *Store) SaveTaskDraft(ctx context.Context, ownerHash []byte, tabID string, draft domain.TaskDraft) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO task_drafts (
			session_id_hash, tab_id, patient_id, title, due_date, priority, revision
		) VALUES ($1, $2, $3, $4, NULLIF($5, '')::date, $6, $7)
		ON CONFLICT (session_id_hash, tab_id, patient_id) DO UPDATE SET
			title = EXCLUDED.title,
			due_date = EXCLUDED.due_date,
			priority = EXCLUDED.priority,
			revision = EXCLUDED.revision,
			updated_at = now()
		WHERE task_drafts.revision < EXCLUDED.revision`,
		ownerHash, tabID, draft.PatientID, draft.Title, draft.DueDate, draft.Priority, draft.Revision,
	)
	if err != nil {
		return fmt.Errorf("save task draft: %w", err)
	}
	return nil
}

// CreateTask atomically inserts the task and advances its draft to an empty
// tombstone. The revision guard prevents an older in-flight autosave from
// resurrecting text after a successful submission.
func (s *Store) CreateTask(ctx context.Context, task domain.NewTask, ownerHash []byte, tabID string, clearRevision int64) (string, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("begin create task: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var id string
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO patient_tasks (patient_id, title, due_date, priority)
		VALUES ($1, $2, $3, $4)
		RETURNING id`,
		task.PatientID, task.Title, task.DueDate, task.Priority,
	).Scan(&id); err != nil {
		return "", fmt.Errorf("insert task: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO task_drafts (
			session_id_hash, tab_id, patient_id, title, due_date, priority, revision
		) VALUES ($1, $2, $3, '', NULL, 'routine', $4)
		ON CONFLICT (session_id_hash, tab_id, patient_id) DO UPDATE SET
			title = '', due_date = NULL, priority = 'routine',
			revision = GREATEST(task_drafts.revision, EXCLUDED.revision),
			updated_at = now()`,
		ownerHash, tabID, task.PatientID, clearRevision); err != nil {
		return "", fmt.Errorf("clear task draft: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("commit create task: %w", err)
	}
	return id, nil
}

func (s *Store) ToggleTask(ctx context.Context, taskID string) error {
	result, err := s.db.ExecContext(ctx, `
		UPDATE patient_tasks
		SET status = CASE status WHEN 'open' THEN 'done' ELSE 'open' END,
			completed_at = CASE status WHEN 'open' THEN now() ELSE NULL END
		WHERE id = $1`, taskID)
	if err != nil {
		return fmt.Errorf("toggle task: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read toggle result: %w", err)
	}
	if changed == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// Listen turns PostgreSQL NOTIFY messages into one homogeneous invalidation
// signal. Triggers emit after commit, so clients never render uncommitted state.
func Listen(ctx context.Context, databaseURL string, logger *slog.Logger, changed func()) error {
	listener := pq.NewListener(databaseURL, time.Second, 15*time.Second, func(event pq.ListenerEventType, err error) {
		if err != nil {
			logger.Warn("postgres listener state changed", "event", event, "error", err)
		}
		if event == pq.ListenerEventReconnected {
			changed() // A reconnect may have missed notifications; render latest state.
		}
	})
	defer listener.Close()
	if err := listener.Listen("practice_changed"); err != nil {
		return fmt.Errorf("listen for practice changes: %w", err)
	}

	keepalive := time.NewTicker(30 * time.Second)
	defer keepalive.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case notification, open := <-listener.Notify:
			if !open {
				if ctx.Err() != nil {
					return nil
				}
				return errors.New("postgres notification channel closed")
			}
			if notification != nil {
				changed()
			}
		case <-keepalive.C:
			if err := listener.Ping(); err != nil {
				logger.Warn("ping postgres notification listener", "error", err)
			}
		}
	}
}
