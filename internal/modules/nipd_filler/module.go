package nipd_filler

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
		Key:         "nipd_filler",
		Name:        "Pengisian NIPD",
		Description: "Isi NIPD kosong atau renumber NIPD lama secara massal, dikelompokkan per rombel dari kelas bawah ke atas.",
		Version:     "1.3.1",
		Icon:        "id-card",
		Order:       10,
		Roles:       moduleRoles,
		Nav: []registry.NavItem{
			{Label: "Pengisian NIPD", Path: "/nipd-filler/", Icon: "id-card", Order: 10},
		},
		Templates: templatesFS,
		Static:    staticFS,
		Register:  register,
	})
}

func register(s *registry.Services) error {
	s.Routes.Handle("GET", "/nipd-filler/", moduleRoles, indexHandler(s))
	s.Routes.Handle("POST", "/nipd-filler/api/preview", moduleRoles, previewHandler(s))
	s.Routes.Handle("POST", "/nipd-filler/api/recalc", moduleRoles, recalcHandler(s))
	s.Routes.Handle("GET", "/nipd-filler/api/preview/{job_id}", moduleRoles, previewDataHandler(s))
	s.Routes.Handle("POST", "/nipd-filler/api/apply", moduleRoles, applyHandler(s))
	s.Routes.Handle("POST", "/nipd-filler/api/renumber/preview", moduleRoles, renumberPreviewHandler(s))
	s.Routes.Handle("POST", "/nipd-filler/api/renumber/recalc", moduleRoles, renumberRecalcHandler(s))
	s.Routes.Handle("GET", "/nipd-filler/api/renumber/preview/{job_id}", moduleRoles, renumberPreviewDataHandler(s))
	s.Routes.Handle("POST", "/nipd-filler/api/renumber/apply", moduleRoles, renumberApplyHandler(s))

	s.Jobs.Register(registry.JobSpec{
		Key: "nipd.preview", Label: "Preview NIPD", ModuleKey: "nipd_filler",
		Roles: moduleRoles, Run: runPreview,
	})
	s.Jobs.Register(registry.JobSpec{
		Key: "nipd.apply", Label: "Terapkan NIPD", ModuleKey: "nipd_filler",
		Roles: moduleRoles, Run: runApply,
	})
	s.Jobs.Register(registry.JobSpec{
		Key: renumberPreviewJobType, Label: "Preview Renumber NIPD", ModuleKey: "nipd_filler",
		Roles: moduleRoles, Run: runRenumberPreview,
	})
	s.Jobs.Register(registry.JobSpec{
		Key: renumberApplyJobType, Label: "Terapkan Renumber NIPD", ModuleKey: "nipd_filler",
		Roles: moduleRoles, Run: runRenumberApply,
	})
	return nil
}

type defaults struct {
	Limit string
	Delay string
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
		s.Render(w, r, "nipd_filler/index.html", map[string]any{
			"Title":     "Pengisian NIPD",
			"Active":    "/nipd-filler/",
			"Scopes":    scopes,
			"HasScopes": len(scopes) > 0,
			"ScopeErr":  scopeErr,
			"Defaults": defaults{
				Limit: s.Settings.Get("nipd.limit"),
				Delay: s.Settings.Get("nipd.delay"),
			},
		})
	}
}
