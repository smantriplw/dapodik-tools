package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"dapodik-tool/internal/store"
)

func setup(t *testing.T) (*store.Store, *Manager) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "auth.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st, NewManager(st, "0123456789abcdef0123456789abcdef", false)
}

func TestPasswordHashing(t *testing.T) {
	hash, err := HashPassword("rahasia123")
	if err != nil {
		t.Fatal(err)
	}
	if !CheckPassword(hash, "rahasia123") || CheckPassword(hash, "salah") {
		t.Fatal("password check failed")
	}
	if _, err := HashPassword("pendek"); err == nil {
		t.Fatal("expected short password error")
	}
}

func TestLoginSessionAndCSRF(t *testing.T) {
	st, manager := setup(t)
	ctx := context.Background()
	hash, _ := HashPassword("rahasia123")
	id, err := st.CreateUser(ctx, "guru", "Guru", hash, "guru")
	if err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	user, err := manager.Login(ctx, rec, "guru", "rahasia123", "ip|guru")
	if err != nil {
		t.Fatal(err)
	}
	if user.ID != id {
		t.Fatalf("user id = %d", user.ID)
	}
	cookies := rec.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("no session cookie")
	}
	cookie := cookies[0]
	if !cookie.HttpOnly || cookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("cookie flags: %+v", cookie)
	}

	req2 := httptest.NewRequest(http.MethodGet, "/", nil)
	req2.AddCookie(cookie)
	sess, err := manager.Session(req2)
	if err != nil || sess.UserID != id {
		t.Fatalf("session = %+v err=%v", sess, err)
	}
	if !manager.ValidateCSRF(req2, sess.CSRF) {
		t.Fatal("csrf should validate")
	}
	if manager.ValidateCSRF(req2, "wrong") {
		t.Fatal("wrong csrf accepted")
	}

	loaded, sess2 := manager.CurrentUser(ctx, req2)
	if loaded == nil || loaded.ID != id || sess2 == nil {
		t.Fatal("current user failed")
	}
}

func TestSessionTampering(t *testing.T) {
	_, manager := setup(t)
	rec := httptest.NewRecorder()
	if err := manager.NewSession(rec, 7); err != nil {
		t.Fatal(err)
	}
	cookie := rec.Result().Cookies()[0]
	cookie.Value = strings.Replace(cookie.Value, ".", ".x", 1)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(cookie)
	if _, err := manager.Session(req); err == nil {
		t.Fatal("tampered session accepted")
	}
}

func TestRateLimit(t *testing.T) {
	_, manager := setup(t)
	rec := httptest.NewRecorder()
	key := "ip|missing"
	for i := 0; i < 5; i++ {
		_, err := manager.Login(context.Background(), rec, "missing", "wrong", key)
		if !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("attempt %d: %v", i, err)
		}
	}
	_, err := manager.Login(context.Background(), rec, "missing", "wrong", key)
	if !errors.Is(err, ErrRateLimited) {
		t.Fatalf("expected rate limit, got %v", err)
	}
}
