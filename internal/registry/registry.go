package registry

import (
	"context"
	"database/sql"
	"errors"
	"io/fs"
	"net/http"
	"sync"

	"dapodik-tool/internal/auth"
	"dapodik-tool/internal/config"
	"dapodik-tool/internal/store"
)

type NavItem struct {
	Label string
	Path  string
	Icon  string
	Order int
}

type Module struct {
	Key         string
	Name        string
	Description string
	Version     string
	Icon        string
	Order       int
	Roles       []auth.Role
	Nav         []NavItem
	Templates   fs.FS
	Static      fs.FS
	Register    func(*Services) error
}

type SettingField struct {
	Key     string
	Label   string
	Help    string
	Type    string
	Default string
	Options []string
}

type JobSpec struct {
	Key       string
	Label     string
	ModuleKey string
	Roles     []auth.Role
	Run       func(*JobContext) (map[string]any, error)
}

type JobItem struct {
	Group        string `json:"group"`
	Nama         string `json:"nama"`
	NISN         string `json:"nisn"`
	RegistrasiID string `json:"registrasi_id"`
	OldNIPD      string `json:"old_nipd"`
	NIPD         string `json:"nipd"`
	Status       string `json:"status"`
	Message      string `json:"message"`
}

type JobContext struct {
	Ctx       context.Context
	JobID     string
	ModuleKey string
	User      *store.User
	Store     *store.Store
	Params    map[string]string
	Secrets   map[string]string

	Log       func(level, message string, data map[string]any)
	Progress  func(current, total int, label string)
	AddItem   func(JobItem)
	AddItems  func([]JobItem)
	Cancelled func() bool
}

type JobRegistrar interface {
	Register(JobSpec)
	Submit(ctx context.Context, user *store.User, specKey string, public, secrets map[string]string) (string, error)
}

type SettingRegistrar interface {
	Register(SettingField)
	Get(key string) string
}

type RouteRegistrar interface {
	Handle(method, pattern string, roles []auth.Role, handler http.HandlerFunc)
}

var (
	ErrScopeNotAllowed        = errors.New("scope Dapodik tidak diizinkan untuk akun ini")
	ErrScopeSelectionRequired = errors.New("pilih salah satu scope Dapodik")
	ErrNoScopeAvailable       = errors.New("belum ada scope Dapodik yang tersedia")
	ErrScopeCredentialMissing = errors.New("scope Dapodik belum memiliki cookie")
)

type DapodikScopeView struct {
	ID               string
	Name             string
	BaseURL          string
	SekolahID        string
	NPSN             string
	CookieConfigured bool
	TokenConfigured  bool
}

type ScopeCredentials struct {
	ID        string
	Name      string
	BaseURL   string
	SekolahID string
	NPSN      string
	Cookie    string
	Token     string
}

type ScopeProvider interface {
	List(user *store.User) ([]DapodikScopeView, error)
	Resolve(user *store.User, scopeID string) (ScopeCredentials, error)
}

type Services struct {
	ModuleKey   string
	Store       *store.Store
	DB          *sql.DB
	Config      *config.Config
	Jobs        JobRegistrar
	Settings    SettingRegistrar
	Routes      RouteRegistrar
	Render      func(w http.ResponseWriter, r *http.Request, name string, data map[string]any)
	Audit       func(r *http.Request, action string, detail map[string]any)
	CurrentUser func(r *http.Request) *store.User
	Scopes      ScopeProvider
}

var (
	mu      sync.RWMutex
	modules []Module
)

func Register(m Module) {
	if m.Key == "" || m.Name == "" || m.Register == nil {
		panic("modul tidak valid: key, name, dan Register wajib diisi")
	}
	mu.Lock()
	defer mu.Unlock()
	for _, existing := range modules {
		if existing.Key == m.Key {
			panic("modul duplikat: " + m.Key)
		}
	}
	modules = append(modules, m)
}

func All() []Module {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]Module, len(modules))
	copy(out, modules)
	return out
}

func Find(key string) (Module, bool) {
	mu.RLock()
	defer mu.RUnlock()
	for _, m := range modules {
		if m.Key == key {
			return m, true
		}
	}
	return Module{}, false
}
