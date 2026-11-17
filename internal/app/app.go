package app

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	neturl "net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"dapodik-tool/internal/auth"
	"dapodik-tool/internal/config"
	"dapodik-tool/internal/jobs"
	"dapodik-tool/internal/registry"
	"dapodik-tool/internal/secret"
	"dapodik-tool/internal/store"
)

type App struct {
	Config        *config.Config
	Store         *store.Store
	Auth          *auth.Manager
	Jobs          *jobs.Runner
	Log           *slog.Logger
	CredentialBox *secret.Box

	mux      *http.ServeMux
	renderer *Renderer
	modules  []registry.Module

	settingsMu    sync.RWMutex
	settingFields []registry.SettingField
	settings      map[string]string

	modulesMu     sync.RWMutex
	modulesActive map[string]bool
}

func New(cfg *config.Config) (*App, error) {
	logger := newLogger(cfg.LogLevel)
	st, err := store.Open(cfg.DatabasePath)
	if err != nil {
		return nil, err
	}
	credentialKey := cfg.CredentialsKey
	if credentialKey == "" {
		credentialKey = cfg.SessionSecret
	}
	box, err := secret.New(credentialKey)
	if err != nil {
		st.Close()
		return nil, err
	}

	a := &App{
		Config:        cfg,
		Store:         st,
		Auth:          auth.NewManager(st, cfg.SessionSecret, cfg.SessionSecure),
		Log:           logger,
		CredentialBox: box,
		mux:           http.NewServeMux(),
		settings:      make(map[string]string),
		modulesActive: make(map[string]bool),
	}

	if err := a.loadSettings(); err != nil {
		st.Close()
		return nil, err
	}
	a.registerCoreSettings()
	if err := a.seedSettings(); err != nil {
		st.Close()
		return nil, err
	}
	if err := a.migrateLegacyDapodikScope(); err != nil {
		st.Close()
		return nil, err
	}
	a.Jobs = jobs.NewRunner(st, logger)
	a.renderer = NewRenderer(a)

	a.registerRoutes()

	if err := a.registerModules(); err != nil {
		st.Close()
		return nil, err
	}
	a.registerStatic()
	a.Jobs.Start(context.Background())
	return a, nil
}

func (a *App) Close() error { return a.Store.Close() }

func (a *App) Handler() http.Handler {
	return a.recoverer(a.requestLogger(a.securityHeaders(a.mux)))
}

func newLogger(level string) *slog.Logger {
	var lvl slog.Level
	switch strings.ToLower(level) {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}
	return slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: lvl}))
}

func (a *App) registerCoreSettings() {
	fields := []registry.SettingField{
		{Key: "nipd.limit", Label: "Limit data per halaman", Help: "Default 20000", Type: "int", Default: "20000"},
		{Key: "nipd.delay", Label: "Jeda antar request (detik)", Help: "Default 0.3", Type: "text", Default: "0.3"},
		{Key: "retention_days", Label: "Retensi riwayat job (hari)", Help: "Event dan item job lebih lama dari ini akan dihapus oleh prune", Type: "int", Default: "90"},
	}
	for _, f := range fields {
		a.Register(f)
	}
}

func (a *App) Register(f registry.SettingField) {
	a.settingsMu.Lock()
	defer a.settingsMu.Unlock()
	for _, existing := range a.settingFields {
		if existing.Key == f.Key {
			return
		}
	}
	a.settingFields = append(a.settingFields, f)
}

func (a *App) Setting(key string) string {
	a.settingsMu.RLock()
	defer a.settingsMu.RUnlock()
	return a.settings[key]
}

func (a *App) settingFieldsList() []registry.SettingField {
	a.settingsMu.RLock()
	defer a.settingsMu.RUnlock()
	out := make([]registry.SettingField, len(a.settingFields))
	copy(out, a.settingFields)
	return out
}

func (a *App) loadSettings() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	values, err := a.Store.AllSettings(ctx)
	if err != nil {
		return err
	}
	a.settingsMu.Lock()
	a.settings = values
	a.settingsMu.Unlock()
	return nil
}

func (a *App) seedSettings() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	defaults := map[string]string{
		"dapodik.base_url":   a.Config.DefaultBaseURL,
		"dapodik.sekolah_id": a.Config.DefaultSekolahID,
		"dapodik.npsn":       a.Config.DefaultNPSN,
	}
	for _, f := range a.settingFieldsList() {
		if f.Default != "" {
			defaults[f.Key] = f.Default
		}
	}
	for key, value := range defaults {
		if _, ok := a.settings[key]; !ok && value != "" {
			if err := a.Store.SetSetting(ctx, key, value); err != nil {
				return err
			}
			a.settingsMu.Lock()
			a.settings[key] = value
			a.settingsMu.Unlock()
		}
	}
	return nil
}

func (a *App) setSetting(ctx context.Context, key, value string) error {
	if err := a.Store.SetSetting(ctx, key, value); err != nil {
		return err
	}
	a.settingsMu.Lock()
	a.settings[key] = value
	a.settingsMu.Unlock()
	return nil
}

func (a *App) registerModules() error {
	all := registry.All()
	sort.SliceStable(all, func(i, j int) bool {
		if all[i].Order == all[j].Order {
			return all[i].Name < all[j].Name
		}
		return all[i].Order < all[j].Order
	})
	keys := make([]string, 0, len(all))
	for _, m := range all {
		keys = append(keys, m.Key)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := a.Store.SyncModules(ctx, keys); err != nil {
		return err
	}
	states, err := a.Store.ListModuleStates(ctx)
	if err != nil {
		return err
	}

	for _, m := range all {
		enabled := states[m.Key]
		services := &registry.Services{
			ModuleKey:   m.Key,
			Store:       a.Store,
			DB:          a.Store.DB(),
			Config:      a.Config,
			Jobs:        &moduleJobRegistrar{app: a, runner: a.Jobs, moduleKey: m.Key},
			Settings:    a,
			Routes:      &moduleRouteRegistrar{app: a, moduleKey: m.Key},
			Render:      a.RenderPage,
			Audit:       a.auditFromRequest,
			CurrentUser: func(r *http.Request) *store.User { return userFrom(r) },
			Scopes:      appScopeProvider{app: a},
		}
		if err := m.Register(services); err != nil {
			return fmt.Errorf("register modul %s: %w", m.Key, err)
		}
		a.modules = append(a.modules, m)
		a.modulesMu.Lock()
		a.modulesActive[m.Key] = enabled
		a.modulesMu.Unlock()
		state := "nonaktif"
		if enabled {
			state = "aktif"
		}
		a.Log.Info("modul terdaftar", "module", m.Key, "version", m.Version, "state", state)
	}
	return nil
}

func (a *App) ModuleEnabled(key string) bool {
	a.modulesMu.RLock()
	defer a.modulesMu.RUnlock()
	return a.modulesActive[key]
}

func (a *App) setModuleEnabled(key string, enabled bool) {
	a.modulesMu.Lock()
	a.modulesActive[key] = enabled
	a.modulesMu.Unlock()
}

type moduleJobRegistrar struct {
	app       *App
	runner    *jobs.Runner
	moduleKey string
}

func (r *moduleJobRegistrar) Register(spec registry.JobSpec) {
	if spec.ModuleKey == "" {
		spec.ModuleKey = r.moduleKey
	}
	r.runner.Register(spec)
}

func (r *moduleJobRegistrar) Submit(ctx context.Context, user *store.User, specKey string, public, secrets map[string]string) (string, error) {
	if !r.app.ModuleEnabled(r.moduleKey) {
		return "", fmt.Errorf("modul %s sedang nonaktif", r.moduleKey)
	}
	return r.runner.Submit(ctx, user, specKey, public, secrets)
}

type moduleRouteRegistrar struct {
	app       *App
	moduleKey string
}

func (r *moduleRouteRegistrar) Handle(method, pattern string, roles []auth.Role, handler http.HandlerFunc) {
	r.app.route(method+" "+pattern, roles, func(w http.ResponseWriter, req *http.Request) {
		if !r.app.ModuleEnabled(r.moduleKey) {
			if wantsJSON(req) {
				writeJSON(w, http.StatusNotFound, map[string]any{"error": "modul nonaktif atau tidak ditemukan"})
			} else {
				r.app.renderError(w, req, http.StatusNotFound, "Modul nonaktif atau tidak ditemukan.")
			}
			return
		}
		handler(w, req)
	})
}

func (a *App) auditFromRequest(r *http.Request, action string, detail map[string]any) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	user := userFrom(r)
	var userID *int64
	if user != nil {
		id := user.ID
		userID = &id
	}
	detailJSON := "{}"
	if len(detail) > 0 {
		if b, err := json.Marshal(detail); err == nil {
			detailJSON = string(b)
		}
	}
	if err := a.Store.AddAudit(ctx, userID, action, detailJSON, clientIP(a.Config, r)); err != nil {
		a.Log.Error("gagal menulis audit", "error", err)
	}
}

func (a *App) route(pattern string, roles []auth.Role, handler http.HandlerFunc) {
	a.mux.Handle(pattern, a.authenticated(roles, handler))
}

func (a *App) publicRoute(pattern string, handler http.HandlerFunc) {
	a.mux.Handle(pattern, handler)
}

func (a *App) authenticated(roles []auth.Role, next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		user, sess := a.Auth.CurrentUser(ctx, r)
		if user == nil {
			if wantsJSON(r) {
				writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "silakan login"})
				return
			}
			target := "/login?next=" + urlQuery(r.URL.RequestURI())
			http.Redirect(w, r, target, http.StatusSeeOther)
			return
		}
		if !auth.HasRole(user, roles...) {
			if wantsJSON(r) {
				writeJSON(w, http.StatusForbidden, map[string]any{"error": "akses ditolak"})
				return
			}
			a.renderError(w, r, http.StatusForbidden, "Anda tidak memiliki akses ke halaman ini.")
			return
		}
		if isMutating(r.Method) {
			token := r.Header.Get("X-CSRF-Token")
			if token == "" {
				_ = r.ParseForm()
				token = r.FormValue("_csrf")
			}
			if sess == nil || !a.Auth.ValidateCSRF(r, token) {
				if wantsJSON(r) {
					writeJSON(w, http.StatusForbidden, map[string]any{"error": "CSRF token tidak valid"})
				} else {
					a.renderError(w, r, http.StatusForbidden, "Sesi formulir kedaluwarsa. Muat ulang halaman lalu coba lagi.")
				}
				return
			}
		}
		ctx = context.WithValue(ctx, ctxUserKey, user)
		ctx = context.WithValue(ctx, ctxSessionKey, sess)
		next(w, r.WithContext(ctx))
	})
}

func (a *App) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' https://fonts.googleapis.com; font-src 'self' https://fonts.gstatic.com data:; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'")
		h.Set("X-Frame-Options", "DENY")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "same-origin")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
			h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		next.ServeHTTP(w, r)
	})
}

func (a *App) requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		a.Log.Info("request", "method", r.Method, "path", r.URL.Path, "status", rec.status,
			"duration_ms", time.Since(start).Milliseconds(), "ip", clientIP(a.Config, r))
	})
}

func (a *App) recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				a.Log.Error("panic pada request", "panic", rec, "path", r.URL.Path)
				http.Error(w, "Terjadi kesalahan internal.", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

type contextKey string

const (
	ctxUserKey    contextKey = "user"
	ctxSessionKey contextKey = "session"
)

func userFrom(r *http.Request) *store.User {
	if v, ok := r.Context().Value(ctxUserKey).(*store.User); ok {
		return v
	}
	return nil
}

func sessionFrom(r *http.Request) *auth.Session {
	if v, ok := r.Context().Value(ctxSessionKey).(*auth.Session); ok {
		return v
	}
	return nil
}

func isMutating(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

func wantsJSON(r *http.Request) bool {
	return strings.HasPrefix(r.URL.Path, "/api/") ||
		strings.Contains(r.URL.Path, "/api/") ||
		strings.Contains(r.Header.Get("Accept"), "application/json") ||
		strings.EqualFold(r.Header.Get("X-Requested-With"), "XMLHttpRequest")
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func clientIP(cfg *config.Config, r *http.Request) string {
	if cfg.TrustProxy {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			parts := strings.Split(xff, ",")
			if ip := strings.TrimSpace(parts[0]); ip != "" {
				return ip
			}
		}
		if ip := strings.TrimSpace(r.Header.Get("X-Real-IP")); ip != "" {
			return ip
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func urlQuery(v string) string {
	return neturl.QueryEscape(v)
}

type Flash struct {
	Kind    string `json:"kind"`
	Message string `json:"message"`
}

const flashCookie = "smanti_flash"

func (a *App) setFlash(w http.ResponseWriter, kind, message string) {
	b, _ := json.Marshal(Flash{Kind: kind, Message: message})
	http.SetCookie(w, &http.Cookie{
		Name:     flashCookie,
		Value:    base64.RawURLEncoding.EncodeToString(b),
		Path:     "/",
		MaxAge:   60,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   a.Config.SessionSecure,
	})
}

func (a *App) popFlash(w http.ResponseWriter, r *http.Request) *Flash {
	cookie, err := r.Cookie(flashCookie)
	if err != nil || cookie.Value == "" {
		return nil
	}
	http.SetCookie(w, &http.Cookie{
		Name: flashCookie, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: a.Config.SessionSecure,
	})
	raw, err := base64.RawURLEncoding.DecodeString(cookie.Value)
	if err != nil {
		return nil
	}
	var f Flash
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil
	}
	return &f
}

func (a *App) safeNext(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || !strings.HasPrefix(value, "/") || strings.HasPrefix(value, "//") {
		return "/"
	}
	return value
}

func formBool(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func parseIntDefault(value string, fallback int) int {
	n, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return fallback
	}
	return n
}
