CREATE TABLE task_drafts (
    session_id_hash BYTEA NOT NULL,
    tab_id UUID NOT NULL,
    patient_id UUID NOT NULL REFERENCES patients(id) ON DELETE CASCADE,
    title TEXT NOT NULL DEFAULT '' CHECK (length(title) <= 180),
    due_date DATE,
    priority TEXT NOT NULL DEFAULT 'routine' CHECK (priority IN ('routine', 'important', 'urgent')),
    revision BIGINT NOT NULL DEFAULT 0 CHECK (revision >= 0),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (session_id_hash, tab_id, patient_id)
);

CREATE INDEX task_drafts_patient_idx ON task_drafts (patient_id);
