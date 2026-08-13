CREATE TABLE IF NOT EXISTS patients (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    name TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 120),
    date_of_birth DATE NOT NULL,
    pronouns TEXT NOT NULL DEFAULT '',
    care_team TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS patient_tasks (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    patient_id UUID NOT NULL REFERENCES patients(id) ON DELETE CASCADE,
    title TEXT NOT NULL CHECK (length(title) BETWEEN 1 AND 180),
    due_date DATE NOT NULL,
    status TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'done')),
    priority TEXT NOT NULL DEFAULT 'routine' CHECK (priority IN ('routine', 'important', 'urgent')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS patient_tasks_patient_status_idx
    ON patient_tasks (patient_id, status, due_date);
CREATE INDEX IF NOT EXISTS patient_tasks_open_due_idx
    ON patient_tasks (due_date) WHERE status = 'open';

CREATE OR REPLACE FUNCTION notify_practice_changed() RETURNS trigger AS $$
BEGIN
    PERFORM pg_notify('practice_changed', TG_TABLE_NAME);
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS patients_changed ON patients;
CREATE TRIGGER patients_changed
AFTER INSERT OR UPDATE OR DELETE ON patients
FOR EACH STATEMENT EXECUTE FUNCTION notify_practice_changed();

DROP TRIGGER IF EXISTS patient_tasks_changed ON patient_tasks;
CREATE TRIGGER patient_tasks_changed
AFTER INSERT OR UPDATE OR DELETE ON patient_tasks
FOR EACH STATEMENT EXECUTE FUNCTION notify_practice_changed();

INSERT INTO patients (id, name, date_of_birth, pronouns, care_team) VALUES
    ('019fbd32-0601-7001-8000-000000000001', 'Maya Chen', '1988-04-17', 'she/her', 'Dr. Rivera · Jordan Lee, LMFT'),
    ('019fbd32-0602-7002-8000-000000000002', 'Elias Brooks', '1995-11-02', 'he/him', 'Dr. Patel · Sam Okafor, LCSW'),
    ('019fbd32-0603-7003-8000-000000000003', 'Noor Ahmed', '1979-07-29', 'they/them', 'Dr. Rivera · Sam Okafor, LCSW'),
    ('019fbd32-0604-7004-8000-000000000004', 'Sofia Martinez', '2001-01-12', 'she/her', 'Dr. Patel · Jordan Lee, LMFT')
ON CONFLICT (id) DO NOTHING;

INSERT INTO patient_tasks (id, patient_id, title, due_date, status, priority, completed_at) VALUES
    ('019fbd32-0605-7005-8000-000000000005', '019fbd32-0601-7001-8000-000000000001', 'Review sleep journal', CURRENT_DATE + 1, 'open', 'important', NULL),
    ('019fbd32-0606-7006-8000-000000000006', '019fbd32-0601-7001-8000-000000000001', 'Send grounding exercise handout', CURRENT_DATE + 5, 'open', 'routine', NULL),
    ('019fbd32-0607-7007-8000-000000000007', '019fbd32-0602-7002-8000-000000000002', 'Medication follow-up call', CURRENT_DATE - 1, 'open', 'urgent', NULL),
    ('019fbd32-0608-7008-8000-000000000008', '019fbd32-0603-7003-8000-000000000003', 'Coordinate care-team release', CURRENT_DATE + 10, 'open', 'important', NULL),
    ('019fbd32-0609-7009-8000-000000000009', '019fbd32-0604-7004-8000-000000000004', 'Complete intake summary', CURRENT_DATE - 3, 'done', 'routine', now())
ON CONFLICT (id) DO NOTHING;
