package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"dapodik-tool/internal/auth"
	"dapodik-tool/internal/registry"
	"dapodik-tool/internal/store"
)

type appScopeProvider struct {
	app *App
}

func (p appScopeProvider) List(user *store.User) ([]registry.DapodikScopeView, error) {
	return p.app.ListScopesForUser(user)
}

func (p appScopeProvider) Resolve(user *store.User, scopeID string) (registry.ScopeCredentials, error) {
	return p.app.ResolveScope(user, scopeID)
}

func (a *App) ListScopesForUser(user *store.User) ([]registry.DapodikScopeView, error) {
	if user == nil {
		return nil, registry.ErrScopeNotAllowed
	}
	scopes, err := a.Store.ListScopesForUser(context.Background(), user.ID, auth.HasRole(user, auth.RoleAdmin))
	if err != nil {
		return nil, err
	}
	out := make([]registry.DapodikScopeView, 0, len(scopes))
	for _, s := range scopes {
		out = append(out, toScopeView(s))
	}
	return out, nil
}

func toScopeView(s store.DapodikScope) registry.DapodikScopeView {
	return registry.DapodikScopeView{
		ID: s.ID, Name: s.Name, BaseURL: s.BaseURL, SekolahID: s.SekolahID, NPSN: s.NPSN,
		CookieConfigured: s.CookieConfigured, TokenConfigured: s.TokenConfigured,
	}
}

func (a *App) ResolveScope(user *store.User, scopeID string) (registry.ScopeCredentials, error) {
	if user == nil {
		return registry.ScopeCredentials{}, registry.ErrScopeNotAllowed
	}
	scopes, err := a.ListScopesForUser(user)
	if err != nil {
		return registry.ScopeCredentials{}, err
	}
	if len(scopes) == 0 {
		return registry.ScopeCredentials{}, registry.ErrNoScopeAvailable
	}
	var selected *registry.DapodikScopeView
	scopeID = strings.TrimSpace(scopeID)
	if scopeID == "" {
		if len(scopes) > 1 {
			return registry.ScopeCredentials{}, registry.ErrScopeSelectionRequired
		}
		selected = &scopes[0]
	} else {
		for i := range scopes {
			if scopes[i].ID == scopeID {
				selected = &scopes[i]
				break
			}
		}
		if selected == nil {
			return registry.ScopeCredentials{}, registry.ErrScopeNotAllowed
		}
	}
	creds := registry.ScopeCredentials{
		ID: selected.ID, Name: selected.Name, BaseURL: selected.BaseURL,
		SekolahID: selected.SekolahID, NPSN: selected.NPSN,
	}
	if cookie, ok, err := a.scopeCookie(selected.ID); err != nil {
		return registry.ScopeCredentials{}, err
	} else if ok {
		creds.Cookie = cookie
	}
	if token, ok, err := a.scopeToken(selected.ID); err != nil {
		return registry.ScopeCredentials{}, err
	} else if ok {
		creds.Token = token
	}
	return creds, nil
}

func (a *App) scopeCredential(scopeID, kind string) (string, string, bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ciphertext, updatedAt, ok, err := a.Store.GetScopeCredential(ctx, scopeID, kind)
	if err != nil || !ok {
		return "", updatedAt, ok, err
	}
	plain, err := a.CredentialBox.Decrypt(ciphertext)
	if err != nil {
		return "", updatedAt, false, err
	}
	return plain, updatedAt, true, nil
}

func (a *App) scopeCookie(scopeID string) (string, bool, error) {
	v, _, ok, err := a.scopeCredential(scopeID, "cookie")
	return v, ok, err
}

func (a *App) scopeToken(scopeID string) (string, bool, error) {
	v, _, ok, err := a.scopeCredential(scopeID, "token")
	return v, ok, err
}

func (a *App) ScopeCredentialState(scopeID, kind string) CredentialState {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ciphertext, updatedAt, ok, err := a.Store.GetScopeCredential(ctx, scopeID, kind)
	if err != nil {
		return CredentialState{Err: err}
	}
	if !ok {
		return CredentialState{}
	}
	if _, err := a.CredentialBox.Decrypt(ciphertext); err != nil {
		return CredentialState{UpdatedAt: updatedAt, Err: err}
	}
	return CredentialState{Configured: true, UpdatedAt: updatedAt}
}

func newScopeID() (string, error) {
	buf := make([]byte, 12)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func (a *App) migrateLegacyDapodikScope() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	count, err := a.Store.CountDapodikScopes(ctx)
	if err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	baseURL := a.Setting("dapodik.base_url")
	sekolahID := a.Setting("dapodik.sekolah_id")
	npsn := a.Setting("dapodik.npsn")
	legacyCookie, _, cookieOK, err := a.Store.GetCredential(ctx, CredentialDapodikCookie)
	if err != nil {
		return err
	}
	legacyToken, _, tokenOK, err := a.Store.GetCredential(ctx, CredentialDapodikToken)
	if err != nil {
		return err
	}
	if baseURL == "" && sekolahID == "" && !cookieOK && !tokenOK {
		return nil
	}
	id, err := newScopeID()
	if err != nil {
		return err
	}
	scope := &store.DapodikScope{
		ID: id, Name: "Dapodik Utama", BaseURL: baseURL, SekolahID: sekolahID, NPSN: npsn,
	}
	if err := a.Store.CreateDapodikScope(ctx, scope); err != nil {
		return fmt.Errorf("migrasi scope Dapodik: %w", err)
	}
	if cookieOK {
		if err := a.Store.SetScopeCredential(ctx, id, "cookie", legacyCookie); err != nil {
			return err
		}
	}
	if tokenOK {
		if err := a.Store.SetScopeCredential(ctx, id, "token", legacyToken); err != nil {
			return err
		}
	}
	users, err := a.Store.ListUsers(ctx)
	if err != nil {
		return err
	}
	for _, u := range users {
		if u.Role == string(auth.RoleAdmin) {
			continue
		}
		if err := a.Store.SetUserScopes(ctx, u.ID, []string{id}); err != nil {
			return err
		}
	}
	a.Log.Info("migrasi kredensial Dapodik lama ke scope", "scope_id", id, "name", scope.Name)
	return nil
}
