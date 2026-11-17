package example

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
		Key:         "example",
		Name:        "Contoh Modul",
		Description: "Modul contoh: ganti key, nama, route, dan handler sesuai kebutuhan.",
		Version:     "0.1.0",
		Icon:        "boxes",
		Order:       100,
		Roles:       moduleRoles,
		Nav: []registry.NavItem{
			{Label: "Contoh Modul", Path: "/example/", Icon: "boxes", Order: 100},
		},
		Templates: templatesFS,
		Static:    staticFS,
		Register:  register,
	})
}

func register(s *registry.Services) error {
	s.Routes.Handle("GET", "/example/", moduleRoles, func(w http.ResponseWriter, r *http.Request) {
		s.Render(w, r, "example/index.html", map[string]any{
			"Title":  "Contoh Modul",
			"Active": "/example/",
		})
	})
	s.Jobs.Register(registry.JobSpec{
		Key: "example.echo", Label: "Contoh Job", ModuleKey: "example",
		Roles: moduleRoles,
		Run: func(j *registry.JobContext) (map[string]any, error) {
			j.Log("info", "Halo dari modul contoh", nil)
			return map[string]any{"ok": true}, nil
		},
	})
	return nil
}
