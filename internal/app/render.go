package app

import (
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"strings"
	"sync"
	"time"

	"dapodik-tool/internal/auth"
	"dapodik-tool/internal/registry"
	"dapodik-tool/internal/store"
	"dapodik-tool/internal/web"
)

type NavLink struct {
	Label  string
	Path   string
	Icon   string
	Active bool
}

type NavGroup struct {
	Label string
	Items []NavLink
}

type ModuleCard struct {
	Key         string
	Name        string
	Description string
	Icon        string
	Path        string
	Order       int
}

type BaseData struct {
	User    *store.User
	Nav     []NavGroup
	Active  string
	CSRF    string
	AppName string
	Flash   *Flash
	IsAdmin bool
	Title   string
}

type Renderer struct {
	app   *App
	cache sync.Map
}

func NewRenderer(a *App) *Renderer {
	return &Renderer{app: a}
}

func (a *App) RenderPage(w http.ResponseWriter, r *http.Request, name string, data map[string]any) {
	a.renderer.RenderPage(w, r, name, data)
}

func (a *App) RenderAuth(w http.ResponseWriter, r *http.Request, name string, data map[string]any) {
	a.renderer.RenderAuth(w, r, name, data)
}

func (a *App) Get(key string) string { return a.Setting(key) }

func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

func (rd *Renderer) RenderPage(w http.ResponseWriter, r *http.Request, name string, data map[string]any) {
	if data == nil {
		data = map[string]any{}
	}
	base := rd.app.baseData(w, r)
	active, _ := data["Active"].(string)
	if active == "" {
		active = r.URL.Path
	}
	base.Active = active
	title, _ := data["Title"].(string)
	if title == "" {
		title = rd.app.Config.AppName
	}
	base.Title = title
	data["Base"] = base
	data["Title"] = title

	tmpl, err := rd.pageTemplate(name, false)
	if err != nil {
		rd.app.Log.Error("gagal memuat template", "template", name, "error", err)
		http.Error(w, "Template tidak dapat dimuat.", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if err := tmpl.ExecuteTemplate(w, "base", data); err != nil {
		rd.app.Log.Error("gagal render template", "template", name, "error", err)
	}
}

func (rd *Renderer) RenderAuth(w http.ResponseWriter, r *http.Request, name string, data map[string]any) {
	if data == nil {
		data = map[string]any{}
	}
	data["AppName"] = rd.app.Config.AppName
	if _, ok := data["Title"]; !ok {
		data["Title"] = rd.app.Config.AppName
	}
	tmpl, err := rd.pageTemplate(name, true)
	if err != nil {
		rd.app.Log.Error("gagal memuat template auth", "template", name, "error", err)
		http.Error(w, "Template tidak dapat dimuat.", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if err := tmpl.ExecuteTemplate(w, "base", data); err != nil {
		rd.app.Log.Error("gagal render template auth", "template", name, "error", err)
	}
}

func (rd *Renderer) pageTemplate(name string, authLayout bool) (*template.Template, error) {
	cacheKey := name
	if authLayout {
		cacheKey = "auth:" + name
	}
	if cached, ok := rd.cache.Load(cacheKey); ok {
		return cached.(*template.Template), nil
	}

	var pageFS fs.FS = web.FS
	pagePath := "templates/" + name
	if key, file, ok := strings.Cut(name, "/"); ok {
		mod, found := rd.app.moduleByKey(key)
		if !found || mod.Templates == nil {
			return nil, fmt.Errorf("template modul %s tidak ditemukan", key)
		}
		pageFS = mod.Templates
		pagePath = "templates/" + file
	}

	layout := "templates/base.html"
	if authLayout {
		layout = "templates/auth.html"
	}
	tmpl, err := template.New("base").Funcs(templateFuncs()).ParseFS(web.FS, layout, "templates/partials.html")
	if err != nil {
		return nil, err
	}
	tmpl, err = tmpl.ParseFS(pageFS, pagePath)
	if err != nil {
		return nil, err
	}
	rd.cache.Store(cacheKey, tmpl)
	return tmpl, nil
}

func (a *App) renderError(w http.ResponseWriter, r *http.Request, status int, message string) {
	tmpl, err := template.New("error").Funcs(templateFuncs()).ParseFS(web.FS, "templates/error.html")
	if err != nil {
		http.Error(w, message, status)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = tmpl.ExecuteTemplate(w, "error.html", map[string]any{
		"Status":  status,
		"Message": message,
		"AppName": a.Config.AppName,
		"Back":    "/",
	})
}

func (a *App) moduleByKey(key string) (registry.Module, bool) {
	for _, m := range a.modules {
		if m.Key == key {
			return m, true
		}
	}
	return registry.Module{}, false
}

func (a *App) baseData(w http.ResponseWriter, r *http.Request) *BaseData {
	user := userFrom(r)
	sess := sessionFrom(r)
	csrf := ""
	if sess != nil {
		csrf = sess.CSRF
	}
	base := &BaseData{
		User:    user,
		AppName: a.Config.AppName,
		Flash:   a.popFlash(w, r),
		CSRF:    csrf,
	}
	if user != nil {
		base.IsAdmin = user.Role == string(auth.RoleAdmin)
	}
	base.Nav = a.navFor(r, user)
	return base
}

func (a *App) navFor(r *http.Request, user *store.User) []NavGroup {
	active := r.URL.Path
	link := func(label, path, icon string) NavLink {
		return NavLink{Label: label, Path: path, Icon: icon, Active: navActive(path, active)}
	}

	groups := []NavGroup{
		{Label: "Utama", Items: []NavLink{
			link("Dashboard", "/", "dashboard"),
			link("Riwayat Job", "/jobs", "history"),
		}},
	}

	var appItems []NavLink
	for _, m := range a.modules {
		if !a.ModuleEnabled(m.Key) || !auth.HasRole(user, m.Roles...) {
			continue
		}
		for _, item := range m.Nav {
			appItems = append(appItems, link(item.Label, item.Path, item.Icon))
		}
	}
	if len(appItems) > 0 {
		groups = append(groups, NavGroup{Label: "Aplikasi", Items: appItems})
	}

	if auth.HasRole(user, auth.RoleAdmin) {
		groups = append(groups, NavGroup{Label: "Administrasi", Items: []NavLink{
			link("Dapodik", "/admin/scopes", "shield"),
			link("Pengguna", "/admin/users", "users"),
			link("Pengaturan", "/admin/settings", "settings"),
			link("Modul", "/admin/modules", "boxes"),
		}})
	}

	groups = append(groups, NavGroup{Label: "Akun", Items: []NavLink{
		link("Ganti Password", "/account/password", "key"),
	}})
	return groups
}

func navActive(path, active string) bool {
	if path == "/" {
		return active == "/"
	}
	return active == path || strings.HasPrefix(active, path+"/")
}

func (a *App) moduleCards(user *store.User) []ModuleCard {
	var cards []ModuleCard
	for _, m := range a.modules {
		if !a.ModuleEnabled(m.Key) || !auth.HasRole(user, m.Roles...) || len(m.Nav) == 0 {
			continue
		}
		cards = append(cards, ModuleCard{
			Key:         m.Key,
			Name:        m.Name,
			Description: m.Description,
			Icon:        m.Icon,
			Path:        m.Nav[0].Path,
			Order:       m.Order,
		})
	}
	return cards
}

func templateFuncs() template.FuncMap {
	return template.FuncMap{
		"icon":        iconSVG,
		"statusClass": statusClass,
		"statusLabel": statusLabel,
		"fmtTime":     fmtTime,
		"fmtShort":    fmtShort,
		"initials":    initials,
		"pct":         pct,
		"roleLabel":   roleLabel,
		"add":         func(a, b int) int { return a + b },
		"hasPrefix":   strings.HasPrefix,
		"shortID":     shortID,
	}
}

var wita = func() *time.Location {
	loc, err := time.LoadLocation("Asia/Makassar")
	if err != nil {
		return time.FixedZone("WITA", 8*3600)
	}
	return loc
}()

func fmtTime(ts string) string {
	t, err := time.Parse(time.RFC3339Nano, ts)
	if err != nil {
		return ts
	}
	return t.In(wita).Format("02 Jan 2006 15:04")
}

func fmtShort(ts string) string {
	t, err := time.Parse(time.RFC3339Nano, ts)
	if err != nil {
		return ts
	}
	return t.In(wita).Format("02/01 15:04")
}

func statusClass(status string) string {
	switch strings.ToLower(status) {
	case "done", "ok", "success", "berhasil":
		return "badge-success"
	case "running", "progress":
		return "badge-running"
	case "queued", "pending", "dry-run", "menunggu":
		return "badge-pending"
	case "failed", "error", "gagal":
		return "badge-danger"
	case "cancelled", "skipped", "dilewati":
		return "badge-muted"
	default:
		return "badge-muted"
	}
}

func statusLabel(status string) string {
	switch strings.ToLower(status) {
	case "done", "success":
		return "Selesai"
	case "running":
		return "Berjalan"
	case "queued":
		return "Menunggu"
	case "pending":
		return "Pending"
	case "failed", "error":
		return "Gagal"
	case "cancelled":
		return "Dibatalkan"
	case "ok":
		return "Berhasil"
	case "preview":
		return "Preview"
	case "filled":
		return "Sudah ada"
	case "skipped":
		return "Dilewati"
	case "dry-run":
		return "Dry-run"
	default:
		if status == "" {
			return "-"
		}
		return status
	}
}

func roleLabel(role string) string {
	switch role {
	case string(auth.RoleAdmin):
		return "Admin"
	case string(auth.RoleGuru):
		return "Operator"
	default:
		return role
	}
}

func initials(name string) string {
	parts := strings.Fields(strings.TrimSpace(name))
	if len(parts) == 0 {
		return "?"
	}
	out := make([]rune, 0, 2)
	for i, p := range parts {
		if i >= 2 {
			break
		}
		r := []rune(p)
		if len(r) > 0 {
			out = append(out, []rune(strings.ToUpper(string(r[0])))...)
		}
	}
	if len(out) == 0 {
		return "?"
	}
	return string(out)
}

func pct(current, total int) int {
	if total <= 0 {
		return 0
	}
	if current < 0 {
		current = 0
	}
	if current > total {
		current = total
	}
	return current * 100 / total
}

func iconSVG(name string) template.HTML {
	body, ok := icons[name]
	if !ok {
		body = icons["square"]
	}
	return template.HTML(`<svg class="icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">` + body + `</svg>`)
}

var icons = map[string]string{
	"square":    `<rect x="4" y="4" width="16" height="16" rx="2"/>`,
	"dashboard": `<rect x="3" y="3" width="7" height="9" rx="1"/><rect x="14" y="3" width="7" height="5" rx="1"/><rect x="14" y="12" width="7" height="9" rx="1"/><rect x="3" y="16" width="7" height="5" rx="1"/>`,
	"history":   `<path d="M3 12a9 9 0 1 0 3-6.7"/><path d="M3 3v5h5"/><path d="M12 7v5l3 2"/>`,
	"users":     `<path d="M16 21v-2a4 4 0 0 0-4-4H6a4 4 0 0 0-4 4v2"/><circle cx="9" cy="7" r="4"/><path d="M22 21v-2a4 4 0 0 0-3-3.87"/><path d="M16 3.13a4 4 0 0 1 0 7.75"/>`,
	"settings":  `<circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.7 1.7 0 0 0 .34 1.88l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06A1.7 1.7 0 0 0 15 19.4a1.7 1.7 0 0 0-1 .6 1.7 1.7 0 0 0-.4 1.1V21a2 2 0 1 1-4 0v-.09A1.7 1.7 0 0 0 8.6 19.4a1.7 1.7 0 0 0-1.88.34l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06A1.7 1.7 0 0 0 4.6 15a1.7 1.7 0 0 0-.6-1 1.7 1.7 0 0 0-1.1-.4H3a2 2 0 1 1 0-4h.09A1.7 1.7 0 0 0 4.6 8.6a1.7 1.7 0 0 0-.34-1.88l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06A1.7 1.7 0 0 0 9 4.6a1.7 1.7 0 0 0 1-.6 1.7 1.7 0 0 0 .4-1.1V3a2 2 0 1 1 4 0v.09A1.7 1.7 0 0 0 15.4 4.6a1.7 1.7 0 0 0 1.88-.34l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06A1.7 1.7 0 0 0 19.4 9c.1.37.32.7.6 1 .3.28.68.42 1.1.4H21a2 2 0 1 1 0 4h-.09a1.7 1.7 0 0 0-1.51.6z"/>`,
	"boxes":     `<path d="M12 3 3 7.5 12 12l9-4.5L12 3z"/><path d="M3 7.5v9L12 21l9-4.5v-9"/><path d="M12 12v9"/>`,
	"key":       `<circle cx="7.5" cy="15.5" r="4.5"/><path d="m10.5 12.5 8-8"/><path d="m16 7 2 2"/><path d="m19 4 2 2"/>`,
	"id-card":   `<rect x="3" y="5" width="18" height="14" rx="2"/><circle cx="8" cy="11" r="2"/><path d="M6 16c.5-1.5 1.5-2 2-2s1.5.5 2 2"/><path d="M13 10h5"/><path d="M13 14h5"/>`,
	"search":    `<circle cx="11" cy="11" r="7"/><path d="m20 20-3.5-3.5"/>`,
	"menu":      `<path d="M4 6h16"/><path d="M4 12h16"/><path d="M4 18h16"/>`,
	"close":     `<path d="M6 6l12 12"/><path d="M18 6 6 18"/>`,
	"logout":    `<path d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4"/><path d="m16 17 5-5-5-5"/><path d="M21 12H9"/>`,
	"check":     `<path d="m5 12 5 5L20 7"/>`,
	"x":         `<path d="M6 6l12 12"/><path d="M18 6 6 18"/>`,
	"alert":     `<path d="M12 9v4"/><path d="M12 17h.01"/><path d="M10.3 3.9 2.6 18a2 2 0 0 0 1.7 3h15.4a2 2 0 0 0 1.7-3L13.7 3.9a2 2 0 0 0-3.4 0z"/>`,
	"download":  `<path d="M12 3v12"/><path d="m7 10 5 5 5-5"/><path d="M5 21h14"/>`,
	"play":      `<path d="m7 4 13 8-13 8V4z"/>`,
	"refresh":   `<path d="M21 12a9 9 0 1 1-2.64-6.36"/><path d="M21 3v6h-6"/>`,
	"clock":     `<circle cx="12" cy="12" r="9"/><path d="M12 7v5l3 2"/>`,
	"shield":    `<path d="M12 3 5 6v6c0 4.5 3 7.5 7 9 4-1.5 7-4.5 7-9V6l-7-3z"/>`,
	"upload":    `<path d="M12 21V9"/><path d="m7 14 5-5 5 5"/><path d="M5 3h14"/>`,
	"file":      `<path d="M14 3H6a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V9z"/><path d="M14 3v6h6"/>`,
}
