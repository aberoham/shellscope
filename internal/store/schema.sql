CREATE TABLE IF NOT EXISTS sessions (
  session_id            TEXT PRIMARY KEY,
  user                  TEXT NOT NULL,
  cluster               TEXT,
  kind                  TEXT,
  started_at            TEXT,
  ended_at              TEXT,
  uploaded_at           TEXT,
  duration_seconds      REAL,
  recording_uri         TEXT,
  recording_bytes       INTEGER,
  pty_present           INTEGER,
  print_chunks          INTEGER,
  print_bytes           INTEGER,
  median_chunk_gap_ms   REAL,
  idle_gap_count        INTEGER,
  edit_char_count       INTEGER,
  command_count         INTEGER,
  bpf_present           INTEGER,
  single_shot           INTEGER,
  join_count            INTEGER,
  parsed_at             TEXT NOT NULL,
  parser_version        TEXT NOT NULL,
  parse_error           TEXT
);
CREATE INDEX IF NOT EXISTS idx_sessions_user      ON sessions(user);
CREATE INDEX IF NOT EXISTS idx_sessions_uploaded  ON sessions(uploaded_at);
CREATE INDEX IF NOT EXISTS idx_sessions_kind      ON sessions(kind);

CREATE TABLE IF NOT EXISTS session_labels (
  session_id  TEXT NOT NULL REFERENCES sessions(session_id) ON DELETE CASCADE,
  key         TEXT NOT NULL,
  value       TEXT NOT NULL,
  set_by      TEXT NOT NULL,
  set_at      TEXT NOT NULL,
  PRIMARY KEY (session_id, key)
);
CREATE INDEX IF NOT EXISTS idx_labels_kv      ON session_labels(key, value);
CREATE INDEX IF NOT EXISTS idx_labels_session ON session_labels(session_id);

CREATE TABLE IF NOT EXISTS notable_events (
  session_id  TEXT NOT NULL REFERENCES sessions(session_id) ON DELETE CASCADE,
  event_time  TEXT NOT NULL,
  event_type  TEXT NOT NULL,
  payload     TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_notable_session ON notable_events(session_id);
