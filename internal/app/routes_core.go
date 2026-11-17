package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"dapodik-tool/internal/auth"
	"dapodik-tool/internal/registry"
	"dapodik-tool/internal/store"
)

var usernamePattern = regexp.MustCompile(`^[A-Za-z0-9._-]{3,40}$`)

func (a *App) registerRoutes() {
	a.publicRoute("GET /login", a.handleLoginGet)
	a.publicRoute("POST /login", a.handleLoginPost)
	a.route("POST /logout", nil, a.handleLogout)

	a.route("GET /{$}", nil, a.handleDashboard)
	a.route("GET /jobs", nil, a.handleJobsList)
	a.route("GET /account/password", nil, a.handleAccountPasswordGet)
	a.route("POST /account/password", nil, a.handleAccountPasswordPost)

	a.registerScopeRoutes()

	a.route("GET /admin/users", []auth.Role{auth.RoleAdmin}, a.handleAdminUsers)
	a.route("GET /admin/users/new", []auth.Role{auth.RoleAdmin}, a.handleAdminUserNewGet)
	a.route("POST /admin/users/new", []auth.Role{auth.RoleAdmin}, a.handleAdminUserNewPost)
	a.route("GET /admin/users/{id}", []auth.Role{auth.RoleAdmin}, a.handleAdminUserEditGet)
	a.route("POST /admin/users/{id}", []auth.Role{auth.RoleAdmin}, a.handleAdminUserEditPost)
	a.route("GET /admin/settings", []auth.Role{auth.RoleAdmin}, a.handleAdminSettingsGet)
	a.route("POST /admin/settings", []auth.Role{auth.RoleAdmin}, a.handleAdminSettingsPost)
	a.route("GET /admin/modules", []auth.Role{auth.RoleAdmin}, a.handleAdminModules)
	a.route("POST /admin/modules/{key}/toggle", []auth.Role{auth.RoleAdmin}, a.handleAdminModuleToggle)

	a.registerJobRoutes()
}

func (a *App) handleLoginGet(w http.ResponseWriter, r *http.Request) {
	if user, _ := a.Auth.CurrentUser(r.Context(), r); user != nil {
		http.Redirect(w, r, a.safeNext(r.URL.Query().Get("next")), http.StatusSeeOther)
		return
	}
	sess := a.Auth.EnsureCSRF(w, r)
	a.RenderAuth(w, r, "login.html", map[string]any{
		"Title": "Masuk",
		"CSRF":  sess.CSRF,
		"Next":  r.URL.Query().Get("next"),
	})
}

func (a *App) handleLoginPost(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	username := strings.TrimSpace(r.FormValue("username"))
	password := r.FormValue("password")
	next := a.safeNext(r.FormValue("next"))

	renderErr := func(status int, msg string) {
		sess := a.Auth.EnsureCSRF(w, r)
		w.WriteHeader(status)
		a.RenderAuth(w, r, "login.html", map[string]any{
			"Title":    "Masuk",
			"CSRF":     sess.CSRF,
			"Next":     r.FormValue("next"),
			"Username": username,
			"Error":    msg,
		})
	}

	if !a.Auth.ValidateCSRF(r, r.FormValue("_csrf")) {
		renderErr(http.StatusForbidden, "Sesi formulir kedaluwarsa. Coba lagi.")
		return
	}

	user, err := a.Auth.Login(r.Context(), w, username, password, clientIP(a.Config, r)+"|"+strings.ToLower(username))
	if err != nil {
		detail := map[string]any{"username": username}
		a.auditAuth(r, nil, "login_failed", detail)
		msg := "Username atau password salah."
		if errors.Is(err, auth.ErrRateLimited) {
			msg = "Terlalu banyak percobaan. Coba lagi dalam 15 menit."
		} else if errors.Is(err, auth.ErrInactive) {
			msg = "Akun ini nonaktif. Hubungi administrator."
		}
		renderErr(http.StatusUnauthorized, msg)
		return
	}

	a.auditAuth(r, user, "login_success", nil)
	a.setFlash(w, "success", "Selamat datang, "+user.Name+".")
	http.Redirect(w, r, next, http.StatusSeeOther)
}

func (a *App) auditAuth(r *http.Request, user *store.User, action string, detail map[string]any) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var uid *int64
	if user != nil {
		id := user.ID
		uid = &id
	}
	_ = a.Store.AddAudit(ctx, uid, action, mustJSON(detail), clientIP(a.Config, r))
}

func mustJSON(v any) string {
	if v == nil {
		return "{}"
	}
	b, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(b)
}

func (a *App) handleLogout(w http.ResponseWriter, r *http.Request) {
	user := userFrom(r)
	a.Auth.ClearSession(w)
	if user != nil {
		a.auditFromRequest(r, "logout", nil)
	}
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (a *App) handleDashboard(w http.ResponseWriter, r *http.Request) {
	user := userFrom(r)
	admin := auth.HasRole(user, auth.RoleAdmin)
	jobs, _, err := a.Store.ListJobs(r.Context(), user.ID, admin, 5, 0)
	if err != nil {
		a.Log.Error("gagal memuat job dashboard", "error", err)
	}
	stats, err := a.Store.Stats(r.Context())
	if err != nil {
		a.Log.Error("gagal memuat statistik", "error", err)
	}
	a.RenderPage(w, r, "dashboard.html", map[string]any{
		"Title":   "Dashboard",
		"Modules": a.moduleCards(user),
		"Jobs":    jobs,
		"Stats":   stats,
		"Labels":  a.jobLabels(),
	})
}

func (a *App) handleJobsList(w http.ResponseWriter, r *http.Request) {
	user := userFrom(r)
	admin := auth.HasRole(user, auth.RoleAdmin)
	page := parseIntDefault(r.URL.Query().Get("page"), 1)
	if page < 1 {
		page = 1
	}
	const pageSize = 20
	jobs, total, err := a.Store.ListJobs(r.Context(), user.ID, admin, pageSize, (page-1)*pageSize)
	if err != nil {
		a.renderError(w, r, http.StatusInternalServerError, "Gagal memuat riwayat job.")
		return
	}
	pages := (total + pageSize - 1) / pageSize
	if pages < 1 {
		pages = 1
	}
	prev, next := page-1, page+1
	a.RenderPage(w, r, "jobs_list.html", map[string]any{
		"Title":   "Riwayat Job",
		"Jobs":    jobs,
		"Total":   total,
		"Page":    page,
		"Pages":   pages,
		"Prev":    prev,
		"Next":    next,
		"Labels":  a.jobLabels(),
		"Modules": a.moduleNames(jobs),
		"Active":  "/jobs",
	})
}

func (a *App) jobLabels() map[string]string {
	out := map[string]string{}
	for _, spec := range a.Jobs.Specs() {
		label := spec.Label
		if label == "" {
			label = spec.Key
		}
		out[spec.Key] = label
	}
	return out
}

func (a *App) moduleNames(jobs []store.Job) map[string]string {
	out := map[string]string{}
	for _, m := range a.modules {
		out[m.Key] = m.Name
	}
	for _, j := range jobs {
		if _, ok := out[j.ModuleKey]; !ok {
			out[j.ModuleKey] = j.ModuleKey
		}
	}
	return out
}

func (a *App) handleAccountPasswordGet(w http.ResponseWriter, r *http.Request) {
	a.RenderPage(w, r, "account_password.html", map[string]any{
		"Title":  "Ganti Password",
		"Active": "/account/password",
	})
}

func (a *App) handleAccountPasswordPost(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	current := r.FormValue("current")
	next := r.FormValue("next")
	confirm := r.FormValue("confirm")
	if next != confirm {
		w.WriteHeader(http.StatusBadRequest)
		a.RenderPage(w, r, "account_password.html", map[string]any{
			"Title": "Ganti Password", "Active": "/account/password",
			"Error": "Konfirmasi password baru tidak sama.",
		})
		return
	}
	if err := a.Auth.ChangePassword(r.Context(), userFrom(r).ID, current, next); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		a.RenderPage(w, r, "account_password.html", map[string]any{
			"Title": "Ganti Password", "Active": "/account/password",
			"Error": err.Error(),
		})
		return
	}
	a.auditFromRequest(r, "password_changed", nil)
	a.setFlash(w, "success", "Password berhasil diubah.")
	http.Redirect(w, r, "/account/password", http.StatusSeeOther)
}

func (a *App) scopeChoices(ctx context.Context, submitted []string) ([]store.DapodikScope, map[string]bool, []string, error) {
	scopes, err := a.Store.ListDapodikScopes(ctx)
	if err != nil {
		return nil, nil, nil, err
	}
	valid := make(map[string]bool, len(scopes))
	for _, scope := range scopes {
		valid[scope.ID] = true
	}
	assigned := make(map[string]bool)
	cleaned := make([]string, 0, len(submitted))
	seen := map[string]bool{}
	for _, id := range submitted {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] || !valid[id] {
			continue
		}
		seen[id] = true
		assigned[id] = true
		cleaned = append(cleaned, id)
	}
	return scopes, assigned, cleaned, nil
}

type userForm struct {
	Username string
	Name     string
	Role     string
	Active   bool
}

func (a *App) handleAdminUsers(w http.ResponseWriter, r *http.Request) {
	users, err := a.Store.ListUsers(r.Context())
	if err != nil {
		a.renderError(w, r, http.StatusInternalServerError, "Gagal memuat pengguna.")
		return
	}
	scopes, err := a.Store.ListDapodikScopes(r.Context())
	if err != nil {
		a.renderError(w, r, http.StatusInternalServerError, "Gagal memuat scope Dapodik.")
		return
	}
	assignments, err := a.Store.AllUserScopes(r.Context())
	if err != nil {
		a.renderError(w, r, http.StatusInternalServerError, "Gagal memuat akses scope.")
		return
	}
	scopeName := make(map[string]string, len(scopes))
	for _, scope := range scopes {
		scopeName[scope.ID] = scope.Name
	}
	display := make(map[int64]string)
	for _, u := range users {
		if u.Role == string(auth.RoleAdmin) {
			display[u.ID] = "Semua scope"
			continue
		}
		names := make([]string, 0)
		for _, id := range assignments[u.ID] {
			if name, ok := scopeName[id]; ok {
				names = append(names, name)
			}
		}
		if len(names) == 0 {
			display[u.ID] = "—"
		} else {
			display[u.ID] = strings.Join(names, ", ")
		}
	}
	a.RenderPage(w, r, "admin_users.html", map[string]any{
		"Title": "Pengguna", "Active": "/admin/users", "Users": users,
		"ScopeDisplay": display,
	})
}

func (a *App) handleAdminUserNewGet(w http.ResponseWriter, r *http.Request) {
	scopes, _, _, err := a.scopeChoices(r.Context(), nil)
	if err != nil {
		a.renderError(w, r, http.StatusInternalServerError, "Gagal memuat scope Dapodik.")
		return
	}
	a.RenderPage(w, r, "admin_user_form.html", map[string]any{
		"Title": "Tambah Pengguna", "Active": "/admin/users", "IsNew": true,
		"Action":    "/admin/users/new",
		"Form":      userForm{Role: "guru", Active: true},
		"AllScopes": scopes, "AssignedScopes": map[string]bool{},
	})
}

func (a *App) handleAdminUserNewPost(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	form := userForm{
		Username: strings.TrimSpace(r.FormValue("username")),
		Name:     strings.TrimSpace(r.FormValue("name")),
		Role:     strings.TrimSpace(r.FormValue("role")),
		Active:   formBool(r.FormValue("active")),
	}
	password := r.FormValue("password")
	scopes, assignedScopes, scopeIDs, scopeErr := a.scopeChoices(r.Context(), r.Form["scope_ids"])
	renderErr := func(msg string) {
		w.WriteHeader(http.StatusBadRequest)
		a.RenderPage(w, r, "admin_user_form.html", map[string]any{
			"Title": "Tambah Pengguna", "Active": "/admin/users", "IsNew": true,
			"Action": "/admin/users/new", "Form": form, "Error": msg,
			"AllScopes": scopes, "AssignedScopes": assignedScopes,
		})
	}
	if scopeErr != nil {
		renderErr("Gagal memuat scope Dapodik.")
		return
	}
	if !usernamePattern.MatchString(form.Username) {
		renderErr("Username 3-40 karakter: huruf, angka, titik, garis bawah, atau strip.")
		return
	}
	if form.Name == "" {
		renderErr("Nama wajib diisi.")
		return
	}
	if _, ok := auth.ParseRole(form.Role); !ok {
		renderErr("Role tidak valid.")
		return
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		renderErr(err.Error())
		return
	}
	id, err := a.Store.CreateUser(r.Context(), form.Username, form.Name, hash, form.Role)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			renderErr("Username sudah dipakai.")
		} else {
			renderErr("Gagal menyimpan pengguna.")
		}
		return
	}
	if !form.Active {
		if err := a.Store.UpdateUser(r.Context(), id, form.Name, form.Role, false); err != nil {
			renderErr("Pengguna dibuat tetapi gagal dinonaktifkan.")
			return
		}
	}
	if form.Role != string(auth.RoleAdmin) {
		if err := a.Store.SetUserScopes(r.Context(), id, scopeIDs); err != nil {
			renderErr("Pengguna dibuat, tetapi akses scope gagal disimpan.")
			return
		}
	}
	a.auditFromRequest(r, "user_created", map[string]any{
		"user_id": id, "username": form.Username, "role": form.Role, "scope_ids": scopeIDs,
	})
	a.setFlash(w, "success", "Pengguna "+form.Username+" berhasil dibuat.")
	http.Redirect(w, r, "/admin/users", http.StatusSeeOther)
}

func (a *App) handleAdminUserEditGet(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		a.renderError(w, r, http.StatusNotFound, "Pengguna tidak ditemukan.")
		return
	}
	target, err := a.Store.GetUserByID(r.Context(), id)
	if err != nil {
		a.renderError(w, r, http.StatusNotFound, "Pengguna tidak ditemukan.")
		return
	}
	assignedIDs, err := a.Store.UserScopeIDs(r.Context(), id)
	if err != nil {
		a.renderError(w, r, http.StatusInternalServerError, "Gagal memuat akses scope.")
		return
	}
	scopes, assignedScopes, _, err := a.scopeChoices(r.Context(), assignedIDs)
	if err != nil {
		a.renderError(w, r, http.StatusInternalServerError, "Gagal memuat scope Dapodik.")
		return
	}
	a.RenderPage(w, r, "admin_user_form.html", map[string]any{
		"Title": "Edit Pengguna", "Active": "/admin/users", "IsNew": false,
		"Action": "/admin/users/" + strconv.FormatInt(id, 10),
		"Form": userForm{
			Username: target.Username, Name: target.Name,
			Role: target.Role, Active: target.IsActive,
		},
		"AllScopes": scopes, "AssignedScopes": assignedScopes,
	})
}

func (a *App) handleAdminUserEditPost(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		a.renderError(w, r, http.StatusNotFound, "Pengguna tidak ditemukan.")
		return
	}
	target, err := a.Store.GetUserByID(r.Context(), id)
	if err != nil {
		a.renderError(w, r, http.StatusNotFound, "Pengguna tidak ditemukan.")
		return
	}
	_ = r.ParseForm()
	form := userForm{
		Username: target.Username,
		Name:     strings.TrimSpace(r.FormValue("name")),
		Role:     strings.TrimSpace(r.FormValue("role")),
		Active:   formBool(r.FormValue("active")),
	}
	password := r.FormValue("password")
	scopes, assignedScopes, scopeIDs, scopeErr := a.scopeChoices(r.Context(), r.Form["scope_ids"])
	renderErr := func(msg string) {
		w.WriteHeader(http.StatusBadRequest)
		a.RenderPage(w, r, "admin_user_form.html", map[string]any{
			"Title": "Edit Pengguna", "Active": "/admin/users", "IsNew": false,
			"Action": "/admin/users/" + strconv.FormatInt(id, 10), "Form": form, "Error": msg,
			"AllScopes": scopes, "AssignedScopes": assignedScopes,
		})
	}
	if scopeErr != nil {
		renderErr("Gagal memuat scope Dapodik.")
		return
	}
	if form.Name == "" {
		renderErr("Nama wajib diisi.")
		return
	}
	if _, ok := auth.ParseRole(form.Role); !ok {
		renderErr("Role tidak valid.")
		return
	}
	current := userFrom(r)
	if target.ID == current.ID && !form.Active {
		renderErr("Tidak bisa menonaktifkan akun sendiri.")
		return
	}
	if target.Role == string(auth.RoleAdmin) && (form.Role != string(auth.RoleAdmin) || !form.Active) {
		if n, err := a.Store.CountActiveAdmins(r.Context()); err == nil && n <= 1 {
			renderErr("Harus ada minimal satu admin aktif.")
			return
		}
	}
	if password != "" {
		hash, err := auth.HashPassword(password)
		if err != nil {
			renderErr(err.Error())
			return
		}
		if err := a.Store.UpdatePassword(r.Context(), id, hash); err != nil {
			renderErr("Gagal memperbarui password.")
			return
		}
	}
	if err := a.Store.UpdateUser(r.Context(), id, form.Name, form.Role, form.Active); err != nil {
		renderErr("Gagal memperbarui pengguna.")
		return
	}
	if form.Role == string(auth.RoleAdmin) {
		scopeIDs = nil
	}
	if err := a.Store.SetUserScopes(r.Context(), id, scopeIDs); err != nil {
		renderErr("Pengguna diperbarui, tetapi akses scope gagal disimpan.")
		return
	}
	a.auditFromRequest(r, "user_updated", map[string]any{
		"user_id": id, "role": form.Role, "active": form.Active, "scope_ids": scopeIDs,
	})
	a.setFlash(w, "success", "Pengguna "+target.Username+" diperbarui.")
	http.Redirect(w, r, "/admin/users", http.StatusSeeOther)
}

func (a *App) handleAdminSettingsGet(w http.ResponseWriter, r *http.Request) {
	values := map[string]string{}
	a.settingsMu.RLock()
	for k, v := range a.settings {
		values[k] = v
	}
	a.settingsMu.RUnlock()
	a.RenderPage(w, r, "admin_settings.html", map[string]any{
		"Title": "Pengaturan", "Active": "/admin/settings",
		"Fields": a.settingFieldsList(), "Values": values,
	})
}

func (a *App) handleAdminSettingsPost(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	values := map[string]string{}
	fields := a.settingFieldsList()
	for _, f := range fields {
		values[f.Key] = strings.TrimSpace(r.FormValue(f.Key))
	}
	renderErr := func(msg string) {
		w.WriteHeader(http.StatusBadRequest)
		a.RenderPage(w, r, "admin_settings.html", map[string]any{
			"Title": "Pengaturan", "Active": "/admin/settings",
			"Fields": fields, "Values": values, "Error": msg,
		})
	}
	for _, f := range fields {
		v := values[f.Key]
		if f.Type == "int" {
			if v == "" {
				renderErr(f.Label + " wajib diisi.")
				return
			}
			if _, err := strconv.Atoi(v); err != nil {
				renderErr(f.Label + " harus berupa angka.")
				return
			}
		}
	}
	for _, f := range fields {
		if err := a.setSetting(r.Context(), f.Key, values[f.Key]); err != nil {
			renderErr("Gagal menyimpan pengaturan.")
			return
		}
	}
	a.auditFromRequest(r, "settings_updated", nil)
	a.setFlash(w, "success", "Pengaturan disimpan.")
	http.Redirect(w, r, "/admin/settings", http.StatusSeeOther)
}

type moduleView struct {
	Module  registry.Module
	Enabled bool
}

func (a *App) handleAdminModules(w http.ResponseWriter, r *http.Request) {
	var views []moduleView
	for _, m := range a.modules {
		views = append(views, moduleView{Module: m, Enabled: a.ModuleEnabled(m.Key)})
	}
	a.RenderPage(w, r, "admin_modules.html", map[string]any{
		"Title": "Modul", "Active": "/admin/modules", "Modules": views,
	})
}

func (a *App) handleAdminModuleToggle(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	m, ok := registry.Find(key)
	if !ok {
		a.renderError(w, r, http.StatusNotFound, "Modul tidak ditemukan.")
		return
	}
	next := !a.ModuleEnabled(key)
	if err := a.Store.SetModuleEnabled(r.Context(), key, next); err != nil {
		a.renderError(w, r, http.StatusInternalServerError, "Gagal mengubah status modul.")
		return
	}
	a.setModuleEnabled(key, next)
	a.auditFromRequest(r, "module_toggled", map[string]any{"module": m.Key, "enabled": next})
	if next {
		a.setFlash(w, "success", "Modul "+m.Name+" diaktifkan. Berlaku langsung.")
	} else {
		a.setFlash(w, "warning", "Modul "+m.Name+" dinonaktifkan. Berlaku langsung.")
	}
	http.Redirect(w, r, "/admin/modules", http.StatusSeeOther)
}
