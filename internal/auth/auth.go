package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"

	"dapodik-tool/internal/store"
)

type Role string

const (
	RoleAdmin Role = "admin"
	RoleGuru  Role = "guru"
)

func ParseRole(v string) (Role, bool) {
	switch Role(strings.ToLower(strings.TrimSpace(v))) {
	case RoleAdmin:
		return RoleAdmin, true
	case RoleGuru:
		return RoleGuru, true
	default:
		return "", false
	}
}

func HasRole(user *store.User, roles ...Role) bool {
	if user == nil {
		return false
	}
	if len(roles) == 0 {
		return true
	}
	for _, r := range roles {
		if string(r) == user.Role {
			return true
		}
	}
	return false
}

var (
	ErrInvalidCredentials = errors.New("username atau password salah")
	ErrInactive           = errors.New("akun tidak aktif")
	ErrRateLimited        = errors.New("terlalu banyak percobaan login, coba lagi nanti")
	ErrNoSession          = errors.New("session tidak ditemukan")
)

type Session struct {
	UserID    int64
	ExpiresAt time.Time
	CSRF      string
}

const (
	sessionCookie = "smanti_session"
	sessionTTL    = 12 * time.Hour
)

type Manager struct {
	store  *store.Store
	secret []byte
	secure bool
	ttl    time.Duration
	limit  *Limiter
}

func NewManager(st *store.Store, secret string, secure bool) *Manager {
	return &Manager{
		store:  st,
		secret: []byte(secret),
		secure: secure,
		ttl:    sessionTTL,
		limit:  NewLimiter(5, 15*time.Minute),
	}
}

func HashPassword(password string) (string, error) {
	if len(password) < 8 {
		return "", errors.New("password minimal 8 karakter")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

func CheckPassword(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

func (m *Manager) Login(ctx context.Context, w http.ResponseWriter, username, password, rateKey string) (*store.User, error) {
	rateKey = strings.ToLower(strings.TrimSpace(rateKey))
	if !m.limit.Allowed(rateKey) {
		return nil, ErrRateLimited
	}
	user, err := m.store.GetUserByUsername(ctx, username)
	if err != nil {
		m.limit.Failure(rateKey)
		return nil, ErrInvalidCredentials
	}
	if !user.IsActive {
		m.limit.Failure(rateKey)
		return nil, ErrInactive
	}
	if !CheckPassword(user.PasswordHash, password) {
		m.limit.Failure(rateKey)
		return nil, ErrInvalidCredentials
	}
	m.limit.Reset(rateKey)
	if err := m.store.TouchLogin(ctx, user.ID); err != nil {
		return nil, err
	}
	if err := m.NewSession(w, user.ID); err != nil {
		return nil, err
	}
	return user, nil
}

func (m *Manager) NewSession(w http.ResponseWriter, userID int64) error {
	csrf, err := randomToken(24)
	if err != nil {
		return err
	}
	return m.setSession(w, &Session{
		UserID:    userID,
		ExpiresAt: time.Now().Add(m.ttl),
		CSRF:      csrf,
	})
}

func (m *Manager) EnsureCSRF(w http.ResponseWriter, r *http.Request) *Session {
	sess, _ := m.Session(r)
	if sess != nil && sess.CSRF != "" && time.Now().Before(sess.ExpiresAt) {
		return sess
	}
	csrf, err := randomToken(24)
	if err != nil {
		return &Session{}
	}
	sess = &Session{UserID: 0, ExpiresAt: time.Now().Add(m.ttl), CSRF: csrf}
	_ = m.setSession(w, sess)
	return sess
}

func (m *Manager) Session(r *http.Request) (*Session, error) {
	cookie, err := r.Cookie(sessionCookie)
	if err != nil || cookie.Value == "" {
		return nil, ErrNoSession
	}
	parts := strings.Split(cookie.Value, ".")
	if len(parts) != 2 {
		return nil, ErrNoSession
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, ErrNoSession
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, ErrNoSession
	}
	expected := m.sign(payload)
	if subtle.ConstantTimeCompare(sig, expected) != 1 {
		return nil, ErrNoSession
	}
	fields := strings.Split(string(payload), "|")
	if len(fields) != 4 || fields[0] != "v1" {
		return nil, ErrNoSession
	}
	userID, err := strconv.ParseInt(fields[1], 10, 64)
	if err != nil {
		return nil, ErrNoSession
	}
	exp, err := strconv.ParseInt(fields[2], 10, 64)
	if err != nil {
		return nil, ErrNoSession
	}
	expires := time.Unix(exp, 0)
	if time.Now().After(expires) {
		return nil, ErrNoSession
	}
	return &Session{UserID: userID, ExpiresAt: expires, CSRF: fields[3]}, nil
}

func (m *Manager) CurrentUser(ctx context.Context, r *http.Request) (*store.User, *Session) {
	sess, err := m.Session(r)
	if err != nil || sess.UserID <= 0 {
		return nil, sess
	}
	user, err := m.store.GetUserByID(ctx, sess.UserID)
	if err != nil || !user.IsActive {
		return nil, sess
	}
	return user, sess
}

func (m *Manager) ValidateCSRF(r *http.Request, token string) bool {
	sess, err := m.Session(r)
	if err != nil || sess.CSRF == "" || token == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(sess.CSRF), []byte(token)) == 1
}

func (m *Manager) ClearSession(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   m.secure,
	})
}

func (m *Manager) setSession(w http.ResponseWriter, sess *Session) error {
	payload := fmt.Sprintf("v1|%d|%d|%s", sess.UserID, sess.ExpiresAt.Unix(), sess.CSRF)
	sig := m.sign([]byte(payload))
	value := base64.RawURLEncoding.EncodeToString([]byte(payload)) + "." +
		base64.RawURLEncoding.EncodeToString(sig)
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    value,
		Path:     "/",
		MaxAge:   int(m.ttl.Seconds()),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   m.secure,
	})
	return nil
}

func (m *Manager) sign(payload []byte) []byte {
	mac := hmac.New(sha256.New, m.secret)
	mac.Write(payload)
	return mac.Sum(nil)
}

func randomToken(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func (m *Manager) ChangePassword(ctx context.Context, userID int64, current, next string) error {
	user, err := m.store.GetUserByID(ctx, userID)
	if err != nil {
		return err
	}
	if !CheckPassword(user.PasswordHash, current) {
		return errors.New("password saat ini salah")
	}
	hash, err := HashPassword(next)
	if err != nil {
		return err
	}
	return m.store.UpdatePassword(ctx, userID, hash)
}

type limiterEntry struct {
	fails int
	reset time.Time
}

type Limiter struct {
	mu        sync.Mutex
	entries   map[string]*limiterEntry
	max       int
	window    time.Duration
	lastSweep time.Time
}

func NewLimiter(max int, window time.Duration) *Limiter {
	return &Limiter{entries: make(map[string]*limiterEntry), max: max, window: window}
}

func (l *Limiter) Allowed(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.sweepLocked()
	e, ok := l.entries[key]
	if !ok {
		return true
	}
	if time.Now().After(e.reset) {
		delete(l.entries, key)
		return true
	}
	return e.fails < l.max
}

func (l *Limiter) Failure(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	e, ok := l.entries[key]
	if !ok || time.Now().After(e.reset) {
		l.entries[key] = &limiterEntry{fails: 1, reset: time.Now().Add(l.window)}
		return
	}
	e.fails++
}

func (l *Limiter) Reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.entries, key)
}

func (l *Limiter) sweepLocked() {
	if time.Since(l.lastSweep) < l.window {
		return
	}
	l.lastSweep = time.Now()
	for k, e := range l.entries {
		if time.Now().After(e.reset) {
			delete(l.entries, k)
		}
	}
}
