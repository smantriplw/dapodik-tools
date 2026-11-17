package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

type DapodikScope struct {
	ID               string
	Name             string
	BaseURL          string
	SekolahID        string
	NPSN             string
	CookieConfigured bool
	TokenConfigured  bool
	CookieUpdatedAt  string
	TokenUpdatedAt   string
	CreatedAt        string
	UpdatedAt        string
}

const scopeColumns = `id, name, base_url, sekolah_id, npsn, created_at, updated_at`

func scanScope(scanner interface{ Scan(...any) error }) (*DapodikScope, error) {
	var s DapodikScope
	if err := scanner.Scan(&s.ID, &s.Name, &s.BaseURL, &s.SekolahID, &s.NPSN, &s.CreatedAt, &s.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &s, nil
}

func (s *Store) attachScopeCredentials(ctx context.Context, scopes []DapodikScope) error {
	if len(scopes) == 0 {
		return nil
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT scope_id, kind, updated_at FROM dapodik_scope_credentials`)
	if err != nil {
		return err
	}
	defer rows.Close()
	meta := make(map[string]map[string]string)
	for rows.Next() {
		var scopeID, kind, updated string
		if err := rows.Scan(&scopeID, &kind, &updated); err != nil {
			return err
		}
		if meta[scopeID] == nil {
			meta[scopeID] = make(map[string]string)
		}
		meta[scopeID][kind] = updated
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for i := range scopes {
		m := meta[scopes[i].ID]
		if m == nil {
			continue
		}
		if updated, ok := m["cookie"]; ok {
			scopes[i].CookieConfigured = true
			scopes[i].CookieUpdatedAt = updated
		}
		if updated, ok := m["token"]; ok {
			scopes[i].TokenConfigured = true
			scopes[i].TokenUpdatedAt = updated
		}
	}
	return nil
}

func (s *Store) CreateDapodikScope(ctx context.Context, scope *DapodikScope) error {
	nowValue := now()
	scope.CreatedAt = nowValue
	scope.UpdatedAt = nowValue
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO dapodik_scopes(id, name, base_url, sekolah_id, npsn, created_at, updated_at)
		 VALUES (?,?,?,?,?,?,?)`,
		scope.ID, strings.TrimSpace(scope.Name), strings.TrimSpace(scope.BaseURL),
		strings.TrimSpace(scope.SekolahID), strings.TrimSpace(scope.NPSN), nowValue, nowValue,
	)
	return err
}

func (s *Store) UpdateDapodikScope(ctx context.Context, id, name, baseURL, sekolahID, npsn string) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE dapodik_scopes SET name=?, base_url=?, sekolah_id=?, npsn=?, updated_at=? WHERE id=?`,
		strings.TrimSpace(name), strings.TrimSpace(baseURL), strings.TrimSpace(sekolahID),
		strings.TrimSpace(npsn), now(), id,
	)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) GetDapodikScope(ctx context.Context, id string) (*DapodikScope, error) {
	scope, err := scanScope(s.db.QueryRowContext(ctx,
		`SELECT `+scopeColumns+` FROM dapodik_scopes WHERE id=?`, id))
	if err != nil {
		return nil, err
	}
	list := []DapodikScope{*scope}
	if err := s.attachScopeCredentials(ctx, list); err != nil {
		return nil, err
	}
	return &list[0], nil
}

func (s *Store) ListDapodikScopes(ctx context.Context) ([]DapodikScope, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+scopeColumns+` FROM dapodik_scopes ORDER BY name COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DapodikScope
	for rows.Next() {
		scope, err := scanScope(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *scope)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := s.attachScopeCredentials(ctx, out); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Store) CountDapodikScopes(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM dapodik_scopes`).Scan(&n)
	return n, err
}

func (s *Store) DeleteDapodikScope(ctx context.Context, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM user_dapodik_scopes WHERE scope_id=?`, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM dapodik_scope_credentials WHERE scope_id=?`, id); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM dapodik_scopes WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return tx.Commit()
}

func (s *Store) SetScopeCredential(ctx context.Context, scopeID, kind, ciphertext string) error {
	if kind != "cookie" && kind != "token" {
		return errors.New("jenis kredensial tidak valid")
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO dapodik_scope_credentials(scope_id, kind, ciphertext, updated_at)
		 VALUES (?,?,?,?)
		 ON CONFLICT(scope_id, kind) DO UPDATE
		 SET ciphertext=excluded.ciphertext, updated_at=excluded.updated_at`,
		scopeID, kind, ciphertext, now(),
	)
	return err
}

func (s *Store) GetScopeCredential(ctx context.Context, scopeID, kind string) (string, string, bool, error) {
	var ciphertext, updatedAt string
	err := s.db.QueryRowContext(ctx,
		`SELECT ciphertext, updated_at FROM dapodik_scope_credentials WHERE scope_id=? AND kind=?`,
		scopeID, kind,
	).Scan(&ciphertext, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", false, nil
	}
	if err != nil {
		return "", "", false, err
	}
	return ciphertext, updatedAt, true, nil
}

func (s *Store) DeleteScopeCredential(ctx context.Context, scopeID, kind string) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM dapodik_scope_credentials WHERE scope_id=? AND kind=?`, scopeID, kind)
	return err
}

func (s *Store) SetUserScopes(ctx context.Context, userID int64, scopeIDs []string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM user_dapodik_scopes WHERE user_id=?`, userID); err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, scopeID := range scopeIDs {
		scopeID = strings.TrimSpace(scopeID)
		if scopeID == "" || seen[scopeID] {
			continue
		}
		seen[scopeID] = true
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO user_dapodik_scopes(user_id, scope_id) VALUES (?,?)`,
			userID, scopeID,
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) UserScopeIDs(ctx context.Context, userID int64) ([]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT scope_id FROM user_dapodik_scopes WHERE user_id=? ORDER BY scope_id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func (s *Store) AllUserScopes(ctx context.Context) (map[int64][]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT user_id, scope_id FROM user_dapodik_scopes ORDER BY user_id, scope_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[int64][]string)
	for rows.Next() {
		var userID int64
		var scopeID string
		if err := rows.Scan(&userID, &scopeID); err != nil {
			return nil, err
		}
		out[userID] = append(out[userID], scopeID)
	}
	return out, rows.Err()
}

func (s *Store) ListScopesForUser(ctx context.Context, userID int64, admin bool) ([]DapodikScope, error) {
	if admin {
		return s.ListDapodikScopes(ctx)
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT s.id, s.name, s.base_url, s.sekolah_id, s.npsn, s.created_at, s.updated_at
		 FROM dapodik_scopes s
		 JOIN user_dapodik_scopes u ON u.scope_id=s.id
		 WHERE u.user_id=?
		 ORDER BY s.name COLLATE NOCASE`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DapodikScope
	for rows.Next() {
		scope, err := scanScope(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *scope)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := s.attachScopeCredentials(ctx, out); err != nil {
		return nil, err
	}
	return out, nil
}
