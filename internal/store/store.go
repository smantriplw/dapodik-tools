package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

var (
	ErrNotFound     = errors.New("data tidak ditemukan")
	ErrNotCancelled = errors.New("job tidak bisa dibatalkan")
)

type Store struct {
	db   *sql.DB
	path string
	mu   sync.Mutex
}

func Open(path string) (*Store, error) {
	if path == "" {
		path = "./data/app.db"
	}
	if dir := filepath.Dir(path); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, fmt.Errorf("membuat direktori database: %w", err)
		}
	}
	dsn := "file:" + filepath.ToSlash(path) +
		"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(4)
	db.SetConnMaxLifetime(0)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("membuka database: %w", err)
	}
	s := &Store{db: db, path: path}
	if err := s.migrateCore(ctx); err != nil {
		db.Close()
		return nil, err
	}
	for _, suffix := range []string{"", "-wal", "-shm"} {
		_ = os.Chmod(path+suffix, 0o600)
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }
func (s *Store) DB() *sql.DB  { return s.db }
func (s *Store) Path() string { return s.path }

func now() string { return time.Now().UTC().Format(time.RFC3339Nano) }

const coreSchema = `
CREATE TABLE IF NOT EXISTS users (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  username TEXT NOT NULL UNIQUE COLLATE NOCASE,
  name TEXT NOT NULL,
  password_hash TEXT NOT NULL,
  role TEXT NOT NULL CHECK (role IN ('admin','guru')),
  is_active INTEGER NOT NULL DEFAULT 1,
  created_at TEXT NOT NULL,
  last_login_at TEXT
);
CREATE TABLE IF NOT EXISTS settings (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS credentials (
  key TEXT PRIMARY KEY,
  ciphertext TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS dapodik_scopes (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL UNIQUE COLLATE NOCASE,
  base_url TEXT NOT NULL,
  sekolah_id TEXT NOT NULL,
  npsn TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS dapodik_scope_credentials (
  scope_id TEXT NOT NULL,
  kind TEXT NOT NULL CHECK (kind IN ('cookie','token')),
  ciphertext TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  PRIMARY KEY (scope_id, kind),
  FOREIGN KEY (scope_id) REFERENCES dapodik_scopes(id) ON DELETE CASCADE
);
CREATE TABLE IF NOT EXISTS user_dapodik_scopes (
  user_id INTEGER NOT NULL,
  scope_id TEXT NOT NULL,
  PRIMARY KEY (user_id, scope_id),
  FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
  FOREIGN KEY (scope_id) REFERENCES dapodik_scopes(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_user_dapodik_scopes_user ON user_dapodik_scopes(user_id);
CREATE INDEX IF NOT EXISTS idx_user_dapodik_scopes_scope ON user_dapodik_scopes(scope_id);
CREATE TABLE IF NOT EXISTS audit_log (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  ts TEXT NOT NULL,
  user_id INTEGER,
  action TEXT NOT NULL,
  detail TEXT NOT NULL DEFAULT '{}',
  ip TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS jobs (
  id TEXT PRIMARY KEY,
  module_key TEXT NOT NULL,
  job_type TEXT NOT NULL,
  status TEXT NOT NULL,
  public_params TEXT NOT NULL DEFAULT '{}',
  summary TEXT NOT NULL DEFAULT '{}',
  error TEXT NOT NULL DEFAULT '',
  created_by INTEGER NOT NULL,
  created_at TEXT NOT NULL,
  started_at TEXT,
  finished_at TEXT,
  cancel_requested INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS job_events (
  seq INTEGER PRIMARY KEY AUTOINCREMENT,
  job_id TEXT NOT NULL,
  ts TEXT NOT NULL,
  level TEXT NOT NULL DEFAULT 'info',
  message TEXT NOT NULL,
  data TEXT NOT NULL DEFAULT '{}'
);
CREATE TABLE IF NOT EXISTS job_items (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  job_id TEXT NOT NULL,
  group_label TEXT NOT NULL DEFAULT '',
  nama TEXT NOT NULL DEFAULT '',
  nisn TEXT NOT NULL DEFAULT '',
  registrasi_id TEXT NOT NULL DEFAULT '',
  old_nipd TEXT NOT NULL DEFAULT '',
  nipd TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT '',
  message TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS modules (
  key TEXT PRIMARY KEY,
  enabled INTEGER NOT NULL DEFAULT 1,
  updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS module_migrations (
  module_key TEXT NOT NULL,
  version INTEGER NOT NULL,
  applied_at TEXT NOT NULL,
  PRIMARY KEY (module_key, version)
);
CREATE INDEX IF NOT EXISTS idx_job_events_job ON job_events(job_id, seq);
CREATE INDEX IF NOT EXISTS idx_job_items_job ON job_items(job_id);
CREATE INDEX IF NOT EXISTS idx_jobs_created ON jobs(created_at);
CREATE INDEX IF NOT EXISTS idx_jobs_user ON jobs(created_by, created_at);
`

func (s *Store) migrateCore(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := execStatements(ctx, tx, coreSchema); err != nil {
		return err
	}
	return tx.Commit()
}

func execStatements(ctx context.Context, tx *sql.Tx, script string) error {
	for _, stmt := range strings.Split(script, ";") {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" {
			continue
		}
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("menjalankan migrasi: %w", err)
		}
	}
	return nil
}

func (s *Store) EnsureSchema(ctx context.Context, moduleKey string, version int, ddl string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var count int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM module_migrations WHERE module_key=? AND version=?`,
		moduleKey, version,
	).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := execStatements(ctx, tx, ddl); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO module_migrations(module_key, version, applied_at) VALUES (?,?,?)`,
		moduleKey, version, now(),
	); err != nil {
		return err
	}
	return tx.Commit()
}

type User struct {
	ID           int64
	Username     string
	Name         string
	PasswordHash string
	Role         string
	IsActive     bool
	CreatedAt    string
	LastLoginAt  sql.NullString
}

func scanUser(scanner interface{ Scan(...any) error }) (*User, error) {
	var u User
	var active int
	if err := scanner.Scan(&u.ID, &u.Username, &u.Name, &u.PasswordHash, &u.Role, &active, &u.CreatedAt, &u.LastLoginAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	u.IsActive = active == 1
	return &u, nil
}

const userColumns = `id, username, name, password_hash, role, is_active, created_at, last_login_at`

func (s *Store) CountUsers(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&n)
	return n, err
}

func (s *Store) CountActiveAdmins(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE role='admin' AND is_active=1`).Scan(&n)
	return n, err
}

func (s *Store) CreateUser(ctx context.Context, username, name, hash, role string) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO users(username, name, password_hash, role, is_active, created_at) VALUES (?,?,?,?,1,?)`,
		strings.TrimSpace(username), strings.TrimSpace(name), hash, role, now(),
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) GetUserByUsername(ctx context.Context, username string) (*User, error) {
	return scanUser(s.db.QueryRowContext(ctx,
		`SELECT `+userColumns+` FROM users WHERE username=? COLLATE NOCASE`, strings.TrimSpace(username)))
}

func (s *Store) GetUserByID(ctx context.Context, id int64) (*User, error) {
	return scanUser(s.db.QueryRowContext(ctx, `SELECT `+userColumns+` FROM users WHERE id=?`, id))
}

func (s *Store) ListUsers(ctx context.Context) ([]User, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+userColumns+` FROM users ORDER BY name COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var users []User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		users = append(users, *u)
	}
	return users, rows.Err()
}

func (s *Store) UpdateUser(ctx context.Context, id int64, name, role string, active bool) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE users SET name=?, role=?, is_active=? WHERE id=?`,
		strings.TrimSpace(name), role, boolInt(active), id,
	)
	return err
}

func (s *Store) UpdatePassword(ctx context.Context, id int64, hash string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE users SET password_hash=? WHERE id=?`, hash, id)
	return err
}

func (s *Store) TouchLogin(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE users SET last_login_at=? WHERE id=?`, now(), id)
	return err
}

func (s *Store) GetSetting(ctx context.Context, key string) (string, bool, error) {
	var value string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key=?`, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return value, true, nil
}

func (s *Store) SetSetting(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO settings(key, value) VALUES (?,?)
		 ON CONFLICT(key) DO UPDATE SET value=excluded.value`,
		key, value,
	)
	return err
}

func (s *Store) AllSettings(ctx context.Context) (map[string]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT key, value FROM settings`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]string)
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		out[k] = v
	}
	return out, rows.Err()
}

func (s *Store) SeedSettings(ctx context.Context, values map[string]string) error {
	for k, v := range values {
		if _, ok, err := s.GetSetting(ctx, k); err != nil {
			return err
		} else if !ok {
			if err := s.SetSetting(ctx, k, v); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Store) SetCredential(ctx context.Context, key, ciphertext string) error {
	if strings.TrimSpace(key) == "" || strings.TrimSpace(ciphertext) == "" {
		return errors.New("key/ciphertext kredensial kosong")
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO credentials(key, ciphertext, updated_at) VALUES (?,?,?)
		 ON CONFLICT(key) DO UPDATE SET ciphertext=excluded.ciphertext, updated_at=excluded.updated_at`,
		key, ciphertext, now(),
	)
	return err
}

func (s *Store) GetCredential(ctx context.Context, key string) (string, string, bool, error) {
	var ciphertext, updatedAt string
	err := s.db.QueryRowContext(ctx,
		`SELECT ciphertext, updated_at FROM credentials WHERE key=?`, key,
	).Scan(&ciphertext, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", false, nil
	}
	if err != nil {
		return "", "", false, err
	}
	return ciphertext, updatedAt, true, nil
}

func (s *Store) DeleteCredential(ctx context.Context, key string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM credentials WHERE key=?`, key)
	return err
}

func (s *Store) AddAudit(ctx context.Context, userID *int64, action, detailJSON, ip string) error {
	var uid any
	if userID != nil && *userID > 0 {
		uid = *userID
	}
	if detailJSON == "" {
		detailJSON = "{}"
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO audit_log(ts, user_id, action, detail, ip) VALUES (?,?,?,?,?)`,
		now(), uid, action, detailJSON, ip,
	)
	return err
}

type Job struct {
	ID              string
	ModuleKey       string
	JobType         string
	Status          string
	PublicParams    string
	Summary         string
	Error           string
	CreatedBy       int64
	CreatedAt       string
	StartedAt       sql.NullString
	FinishedAt      sql.NullString
	CancelRequested bool
}

func (s *Store) CreateJob(ctx context.Context, job *Job) error {
	if job.CreatedAt == "" {
		job.CreatedAt = now()
	}
	if job.PublicParams == "" {
		job.PublicParams = "{}"
	}
	if job.Summary == "" {
		job.Summary = "{}"
	}
	if job.Status == "" {
		job.Status = "queued"
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO jobs(id, module_key, job_type, status, public_params, summary, error, created_by, created_at)
		 VALUES (?,?,?,?,?,?,?,?,?)`,
		job.ID, job.ModuleKey, job.JobType, job.Status, job.PublicParams, job.Summary, job.Error, job.CreatedBy, job.CreatedAt,
	)
	return err
}

func scanJob(scanner interface{ Scan(...any) error }) (*Job, error) {
	var j Job
	var cancel int
	if err := scanner.Scan(&j.ID, &j.ModuleKey, &j.JobType, &j.Status, &j.PublicParams, &j.Summary,
		&j.Error, &j.CreatedBy, &j.CreatedAt, &j.StartedAt, &j.FinishedAt, &cancel); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	j.CancelRequested = cancel == 1
	return &j, nil
}

const jobColumns = `id, module_key, job_type, status, public_params, summary, error, created_by, created_at, started_at, finished_at, cancel_requested`

func (s *Store) GetJob(ctx context.Context, id string) (*Job, error) {
	return scanJob(s.db.QueryRowContext(ctx, `SELECT `+jobColumns+` FROM jobs WHERE id=?`, id))
}

func (s *Store) ListJobs(ctx context.Context, viewerID int64, admin bool, limit, offset int) ([]Job, int, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	where := ""
	args := []any{}
	if !admin {
		where = " WHERE created_by=?"
		args = append(args, viewerID)
	}
	var total int
	countQuery := `SELECT COUNT(*) FROM jobs` + where
	if err := s.db.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	query := `SELECT ` + jobColumns + ` FROM jobs` + where + ` ORDER BY created_at DESC LIMIT ? OFFSET ?`
	args = append(args, limit, offset)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []Job
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *j)
	}
	return out, total, rows.Err()
}

func (s *Store) SetJobRunning(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE jobs SET status='running', started_at=?, finished_at=NULL WHERE id=? AND status='queued'`,
		now(), id,
	)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) SetJobDone(ctx context.Context, id, summaryJSON string) error {
	if summaryJSON == "" {
		summaryJSON = "{}"
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE jobs SET status='done', summary=?, finished_at=?, error='' WHERE id=?`,
		summaryJSON, now(), id,
	)
	return err
}

func (s *Store) SetJobFailed(ctx context.Context, id, message string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE jobs SET status='failed', error=?, finished_at=? WHERE id=?`,
		message, now(), id,
	)
	return err
}

func (s *Store) SetJobCancelled(ctx context.Context, id, message string) error {
	if message == "" {
		message = "Job dibatalkan"
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE jobs SET status='cancelled', error=?, finished_at=? WHERE id=?`,
		message, now(), id,
	)
	return err
}

func (s *Store) RequestCancel(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE jobs SET cancel_requested=1 WHERE id=? AND status IN ('queued','running')`,
		id,
	)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotCancelled
	}
	return nil
}

func (s *Store) FailStaleJobs(ctx context.Context, message string) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`UPDATE jobs SET status='failed', error=?, finished_at=? WHERE status IN ('queued','running')`,
		message, now(),
	)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (s *Store) AppendEvent(ctx context.Context, jobID, level, message, data string) (int64, error) {
	if data == "" {
		data = "{}"
	}
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO job_events(job_id, ts, level, message, data) VALUES (?,?,?,?,?)`,
		jobID, now(), level, message, data,
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

type JobEvent struct {
	Seq     int64  `json:"seq"`
	TS      string `json:"ts"`
	Level   string `json:"level"`
	Message string `json:"message"`
	Data    string `json:"data"`
}

func (s *Store) EventsAfter(ctx context.Context, jobID string, after int64, limit int) ([]JobEvent, error) {
	if limit <= 0 || limit > 1000 {
		limit = 500
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT seq, ts, level, message, data FROM job_events WHERE job_id=? AND seq>? ORDER BY seq LIMIT ?`,
		jobID, after, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []JobEvent
	for rows.Next() {
		var e JobEvent
		if err := rows.Scan(&e.Seq, &e.TS, &e.Level, &e.Message, &e.Data); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

type ItemCounts struct {
	Total   int
	Done    int
	Failed  int
	Skipped int
}

func (s *Store) ItemCounts(ctx context.Context, jobID string) (ItemCounts, error) {
	var c ItemCounts
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*),
		       COALESCE(SUM(CASE WHEN status IN ('ok','done','success') THEN 1 ELSE 0 END),0),
		       COALESCE(SUM(CASE WHEN status IN ('failed','error') THEN 1 ELSE 0 END),0),
		       COALESCE(SUM(CASE WHEN status='skipped' THEN 1 ELSE 0 END),0)
		FROM job_items WHERE job_id=?`, jobID).Scan(&c.Total, &c.Done, &c.Failed, &c.Skipped)
	return c, err
}

type JobItem struct {
	ID           int64  `json:"id"`
	Group        string `json:"group"`
	Nama         string `json:"nama"`
	NISN         string `json:"nisn"`
	RegistrasiID string `json:"registrasi_id"`
	OldNIPD      string `json:"old_nipd"`
	NIPD         string `json:"nipd"`
	Status       string `json:"status"`
	Message      string `json:"message"`
}

func (s *Store) AddItems(ctx context.Context, jobID string, items []JobItem) error {
	if len(items) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stmt, err := tx.PrepareContext(ctx,
		`INSERT INTO job_items(job_id, group_label, nama, nisn, registrasi_id, old_nipd, nipd, status, message)
		 VALUES (?,?,?,?,?,?,?,?,?)`,
	)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, it := range items {
		if _, err := stmt.ExecContext(ctx, jobID, it.Group, it.Nama, it.NISN, it.RegistrasiID,
			it.OldNIPD, it.NIPD, it.Status, it.Message); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) AddItem(ctx context.Context, jobID string, it JobItem) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO job_items(job_id, group_label, nama, nisn, registrasi_id, old_nipd, nipd, status, message)
		 VALUES (?,?,?,?,?,?,?,?,?)`,
		jobID, it.Group, it.Nama, it.NISN, it.RegistrasiID, it.OldNIPD, it.NIPD, it.Status, it.Message,
	)
	return err
}

func (s *Store) ListItems(ctx context.Context, jobID string) ([]JobItem, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, group_label, nama, nisn, registrasi_id, old_nipd, nipd, status, message
		 FROM job_items WHERE job_id=? ORDER BY id`,
		jobID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []JobItem
	for rows.Next() {
		var it JobItem
		if err := rows.Scan(&it.ID, &it.Group, &it.Nama, &it.NISN, &it.RegistrasiID,
			&it.OldNIPD, &it.NIPD, &it.Status, &it.Message); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

func (s *Store) Prune(ctx context.Context, days int) (int64, int64, error) {
	if days < 1 {
		days = 90
	}
	cutoff := time.Now().UTC().AddDate(0, 0, -days).Format(time.RFC3339Nano)
	evRes, err := s.db.ExecContext(ctx,
		`DELETE FROM job_events WHERE job_id IN (SELECT id FROM jobs WHERE created_at < ?)`, cutoff,
	)
	if err != nil {
		return 0, 0, err
	}
	itRes, err := s.db.ExecContext(ctx,
		`DELETE FROM job_items WHERE job_id IN (SELECT id FROM jobs WHERE created_at < ?)`, cutoff,
	)
	if err != nil {
		return 0, 0, err
	}
	ev, _ := evRes.RowsAffected()
	it, _ := itRes.RowsAffected()
	return ev, it, nil
}

func (s *Store) SyncModules(ctx context.Context, keys []string) error {
	for _, key := range keys {
		_, err := s.db.ExecContext(ctx,
			`INSERT INTO modules(key, enabled, updated_at) VALUES (?,1,?)
			 ON CONFLICT(key) DO NOTHING`,
			key, now(),
		)
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) ListModuleStates(ctx context.Context) (map[string]bool, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT key, enabled FROM modules`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]bool)
	for rows.Next() {
		var key string
		var enabled int
		if err := rows.Scan(&key, &enabled); err != nil {
			return nil, err
		}
		out[key] = enabled == 1
	}
	return out, rows.Err()
}

func (s *Store) SetModuleEnabled(ctx context.Context, key string, enabled bool) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE modules SET enabled=?, updated_at=? WHERE key=?`,
		boolInt(enabled), now(), key,
	)
	return err
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func (s *Store) Backup(ctx context.Context, dest string) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
		return err
	}
	if _, err := os.Stat(dest); err == nil {
		return fmt.Errorf("file backup sudah ada: %s", dest)
	}
	quoted := strings.ReplaceAll(dest, "'", "''")
	if _, err := s.db.ExecContext(ctx, `VACUUM INTO '`+quoted+`'`); err != nil {
		return err
	}
	_ = os.Chmod(dest, 0o600)
	return nil
}

func (s *Store) Stats(ctx context.Context) (map[string]int, error) {
	out := make(map[string]int)
	queries := map[string]string{
		"users":  `SELECT COUNT(*) FROM users`,
		"jobs":   `SELECT COUNT(*) FROM jobs`,
		"events": `SELECT COUNT(*) FROM job_events`,
		"items":  `SELECT COUNT(*) FROM job_items`,
	}
	for key, q := range queries {
		var n int
		if err := s.db.QueryRowContext(ctx, q).Scan(&n); err != nil {
			return nil, err
		}
		out[key] = n
	}
	return out, nil
}
