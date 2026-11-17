package student_lookup

import (
	"embed"
	"net/http"

	"dapodik-tool/internal/auth"
	"dapodik-tool/internal/registry"
)

//go:embed templates/*.html
var templatesFS embed.FS

//go:embed static
var staticFS embed.FS

var moduleRoles = []auth.Role{auth.RoleAdmin, auth.RoleGuru}

func init() {
	registry.Register(registry.Module{
		Key:         "student_lookup",
		Name:        "Cek Siswa",
		Description: "Cari siswa berdasarkan NISN, termasuk siswa yang belum masuk rombel.",
		Version:     "1.2.0",
		Icon:        "search",
		Order:       20,
		Roles:       moduleRoles,
		Nav: []registry.NavItem{
			{Label: "Cek Siswa", Path: "/student-lookup/", Icon: "search", Order: 10},
		},
		Templates: templatesFS,
		Static:    staticFS,
		Register:  register,
	})
}

func register(s *registry.Services) error {
	s.Routes.Handle("GET", "/student-lookup/", moduleRoles, indexHandler(s))
	s.Routes.Handle("POST", "/student-lookup/api/search", moduleRoles, searchHandler(s))
	return nil
}

type defaults struct {
	Limit string
}

func indexHandler(s *registry.Services) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var scopes []registry.DapodikScopeView
		var scopeErr string
		if s.Scopes != nil {
			list, err := s.Scopes.List(s.CurrentUser(r))
			if err != nil {
				scopeErr = err.Error()
			} else {
				scopes = list
			}
		}
		s.Render(w, r, "student_lookup/index.html", map[string]any{
			"Title":     "Cek Siswa",
			"Active":    "/student-lookup/",
			"Scopes":    scopes,
			"HasScopes": len(scopes) > 0,
			"ScopeErr":  scopeErr,
			"Defaults":  defaults{Limit: s.Settings.Get("nipd.limit")},
		})
	}
}
