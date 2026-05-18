package prober

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

const schema = `
CREATE TABLE IF NOT EXISTS samples (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    ts_ms       INTEGER NOT NULL,
    path        TEXT    NOT NULL CHECK (path IN ('direct','datum')),
    ok          INTEGER NOT NULL,
    status_code INTEGER,
    ttfb_ms     REAL,
    total_ms    REAL,
    bytes       INTEGER,
    error       TEXT
);
CREATE INDEX IF NOT EXISTS idx_samples_ts ON samples(ts_ms);
CREATE INDEX IF NOT EXISTS idx_samples_path_ts ON samples(path, ts_ms);
`

// Sample is one probe result for one path.
type Sample struct {
	TsMs       int64   `json:"ts_ms"`
	Path       string  `json:"path"`
	Ok         bool    `json:"ok"`
	StatusCode int     `json:"status_code,omitempty"`
	TtfbMs     float64 `json:"ttfb_ms,omitempty"`
	TotalMs    float64 `json:"total_ms,omitempty"`
	Bytes      int64   `json:"bytes,omitempty"`
	Error      string  `json:"error,omitempty"`
}

type Store struct {
	db *sql.DB
}

func OpenStore(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite %q: %w", path, err)
	}
	// SQLite likes a small connection pool when writers serialize.
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) Insert(sample Sample) error {
	_, err := s.db.Exec(
		`INSERT INTO samples (ts_ms, path, ok, status_code, ttfb_ms, total_ms, bytes, error)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		sample.TsMs, sample.Path, boolInt(sample.Ok),
		nullIfZeroInt(sample.StatusCode),
		nullIfZeroFloat(sample.TtfbMs),
		nullIfZeroFloat(sample.TotalMs),
		nullIfZeroInt64(sample.Bytes),
		nullIfEmptyStr(sample.Error),
	)
	return err
}

func (s *Store) Recent(path string, sinceMs int64) ([]Sample, error) {
	rows, err := s.db.Query(
		`SELECT ts_ms, ok, status_code, ttfb_ms, total_ms, bytes, error
		 FROM samples
		 WHERE path = ? AND ts_ms >= ?
		 ORDER BY ts_ms ASC`,
		path, sinceMs,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]Sample, 0, 64)
	for rows.Next() {
		var (
			s   = Sample{Path: path}
			ok  int
			sc  sql.NullInt64
			tt  sql.NullFloat64
			tot sql.NullFloat64
			b   sql.NullInt64
			er  sql.NullString
		)
		if err := rows.Scan(&s.TsMs, &ok, &sc, &tt, &tot, &b, &er); err != nil {
			return nil, err
		}
		s.Ok = ok == 1
		if sc.Valid {
			s.StatusCode = int(sc.Int64)
		}
		if tt.Valid {
			s.TtfbMs = tt.Float64
		}
		if tot.Valid {
			s.TotalMs = tot.Float64
		}
		if b.Valid {
			s.Bytes = b.Int64
		}
		if er.Valid {
			s.Error = er.String
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (s *Store) Prune(beforeMs int64) (int64, error) {
	res, err := s.db.Exec(`DELETE FROM samples WHERE ts_ms < ?`, beforeMs)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (s *Store) Count() (int64, error) {
	var n int64
	err := s.db.QueryRow(`SELECT COUNT(*) FROM samples`).Scan(&n)
	return n, err
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func nullIfZeroInt(v int) any {
	if v == 0 {
		return nil
	}
	return v
}

func nullIfZeroInt64(v int64) any {
	if v == 0 {
		return nil
	}
	return v
}

func nullIfZeroFloat(v float64) any {
	if v == 0 {
		return nil
	}
	return v
}

func nullIfEmptyStr(v string) any {
	if v == "" {
		return nil
	}
	return v
}
