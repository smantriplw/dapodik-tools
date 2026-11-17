package app

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"dapodik-tool/internal/auth"
	"dapodik-tool/internal/dapodik"
	"dapodik-tool/internal/store"
)

func (a *App) registerScopeRoutes() {
	admin := []auth.Role{auth.RoleAdmin}
	a.route("GET /admin/scopes", admin, a.handleScopesList)
	a.route("GET /admin/scopes/new", admin, a.handleScopeNewGet)
	a.route("POST /admin/scopes/new", admin, a.handleScopeNewPost)
	a.route("GET /admin/scopes/{id}", admin, a.handleScopeEditGet)
	a.route("POST /admin/scopes/{id}", admin, a.handleScopeEditPost)
	a.route("POST /admin/scopes/{id}/test", admin, a.handleScopeTest)
	a.route("POST /admin/scopes/{id}/delete", admin, a.handleScopeDelete)
	a.route("GET /admin/dapodik", admin, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/admin/scopes", http.StatusSeeOther)
	})
}

func (a *App) handleScopesList(w http.ResponseWriter, r *http.Request) {
	scopes, err := a.Store.ListDapodikScopes(r.Context())
	if err != nil {
		a.renderError(w, r, http.StatusInternalServerError, "Gagal memuat scope Dapodik.")
		return
	}
	a.RenderPage(w, r, "admin_scopes.html", map[string]any{
		"Title": "Dapodik Scopes", "Active": "/admin/scopes", "Scopes": scopes,
	})
}

type scopeFormValues struct {
	Name      string
	BaseURL   string
	SekolahID string
	NPSN      string
}

func (a *App) renderScopeForm(w http.ResponseWriter, r *http.Request, status int, scopeID string, isNew bool, action string, values scopeFormValues, errMsg string) {
	data := map[string]any{
		"Title":  "Scope Dapodik",
		"Active": "/admin/scopes",
		"IsNew":  isNew,
		"Action": action,
		"Values": values,
	}
	if !isNew {
		data["ScopeID"] = scopeID
		data["CookieState"] = a.ScopeCredentialState(scopeID, "cookie")
		data["TokenState"] = a.ScopeCredentialState(scopeID, "token")
	}
	if errMsg != "" {
		data["Error"] = errMsg
	}
	if status != 0 {
		w.WriteHeader(status)
	}
	a.RenderPage(w, r, "admin_scope_form.html", data)
}

func (a *App) handleScopeNewGet(w http.ResponseWriter, r *http.Request) {
	a.renderScopeForm(w, r, 0, "", true, "/admin/scopes/new", scopeFormValues{}, "")
}

func (a *App) handleScopeNewPost(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	values := scopeFormValues{
		Name:      strings.TrimSpace(r.FormValue("name")),
		BaseURL:   strings.TrimRight(strings.TrimSpace(r.FormValue("base_url")), "/"),
		SekolahID: strings.TrimSpace(r.FormValue("sekolah_id")),
		NPSN:      strings.TrimSpace(r.FormValue("npsn")),
	}
	cookie := strings.TrimSpace(r.FormValue("cookie"))
	token := strings.TrimSpace(r.FormValue("token"))
	fail := func(msg string) {
		a.renderScopeForm(w, r, http.StatusBadRequest, "", true, "/admin/scopes/new", values, msg)
	}
	if msg := validateScopeValues(values); msg != "" {
		fail(msg)
		return
	}
	id, err := newScopeID()
	if err != nil {
		fail("Gagal membuat ID scope.")
		return
	}
	scope := &store.DapodikScope{
		ID: id, Name: values.Name, BaseURL: values.BaseURL,
		SekolahID: values.SekolahID, NPSN: values.NPSN,
	}
	if err := a.Store.CreateDapodikScope(r.Context(), scope); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			fail("Nama scope sudah dipakai.")
		} else {
			fail("Gagal menyimpan scope.")
		}
		return
	}
	if cookie != "" {
		if err := a.setScopeCredential(id, "cookie", cookie); err != nil {
			fail("Scope dibuat, tetapi cookie gagal disimpan: " + err.Error())
			return
		}
	}
	if token != "" {
		if err := a.setScopeCredential(id, "token", token); err != nil {
			fail("Scope dibuat, tetapi token gagal disimpan: " + err.Error())
			return
		}
	}
	a.auditFromRequest(r, "dapodik_scope_created", map[string]any{
		"scope_id": id, "name": values.Name, "cookie": cookie != "", "token": token != "",
	})
	a.setFlash(w, "success", "Scope "+values.Name+" dibuat.")
	http.Redirect(w, r, "/admin/scopes", http.StatusSeeOther)
}

func (a *App) handleScopeEditGet(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	scope, err := a.Store.GetDapodikScope(r.Context(), id)
	if err != nil {
		a.renderError(w, r, http.StatusNotFound, "Scope tidak ditemukan.")
		return
	}
	a.renderScopeForm(w, r, 0, id, false, "/admin/scopes/"+id, scopeFormValues{
		Name: scope.Name, BaseURL: scope.BaseURL, SekolahID: scope.SekolahID, NPSN: scope.NPSN,
	}, "")
}

func (a *App) handleScopeEditPost(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := a.Store.GetDapodikScope(r.Context(), id); err != nil {
		a.renderError(w, r, http.StatusNotFound, "Scope tidak ditemukan.")
		return
	}
	_ = r.ParseForm()
	values := scopeFormValues{
		Name:      strings.TrimSpace(r.FormValue("name")),
		BaseURL:   strings.TrimRight(strings.TrimSpace(r.FormValue("base_url")), "/"),
		SekolahID: strings.TrimSpace(r.FormValue("sekolah_id")),
		NPSN:      strings.TrimSpace(r.FormValue("npsn")),
	}
	cookie := strings.TrimSpace(r.FormValue("cookie"))
	token := strings.TrimSpace(r.FormValue("token"))
	deleteCookie := formBool(r.FormValue("delete_cookie"))
	deleteToken := formBool(r.FormValue("delete_token"))
	fail := func(msg string) {
		a.renderScopeForm(w, r, http.StatusBadRequest, id, false, "/admin/scopes/"+id, values, msg)
	}
	if msg := validateScopeValues(values); msg != "" {
		fail(msg)
		return
	}
	if err := a.Store.UpdateDapodikScope(r.Context(), id, values.Name, values.BaseURL, values.SekolahID, values.NPSN); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			fail("Nama scope sudah dipakai.")
		} else {
			fail("Gagal memperbarui scope.")
		}
		return
	}
	if deleteCookie {
		_ = a.Store.DeleteScopeCredential(r.Context(), id, "cookie")
	} else if cookie != "" {
		if err := a.setScopeCredential(id, "cookie", cookie); err != nil {
			fail("Gagal menyimpan cookie: " + err.Error())
			return
		}
	}
	if deleteToken {
		_ = a.Store.DeleteScopeCredential(r.Context(), id, "token")
	} else if token != "" {
		if err := a.setScopeCredential(id, "token", token); err != nil {
			fail("Gagal menyimpan token: " + err.Error())
			return
		}
	}
	a.auditFromRequest(r, "dapodik_scope_updated", map[string]any{
		"scope_id": id, "name": values.Name,
		"cookie_updated": cookie != "" && !deleteCookie,
		"cookie_deleted": deleteCookie,
		"token_updated":  token != "" && !deleteToken,
		"token_deleted":  deleteToken,
	})
	a.setFlash(w, "success", "Scope "+values.Name+" diperbarui.")
	http.Redirect(w, r, "/admin/scopes", http.StatusSeeOther)
}

func (a *App) handleScopeTest(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	scope, err := a.Store.GetDapodikScope(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "Scope tidak ditemukan."})
		return
	}
	cookie, ok, err := a.scopeCookie(id)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Cookie scope tidak dapat didekripsi: " + err.Error()})
		return
	}
	if !ok || cookie == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Scope belum memiliki cookie."})
		return
	}
	token, _, _ := a.scopeToken(id)
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	client := dapodik.New(dapodik.Config{
		BaseURL: scope.BaseURL, Cookie: cookie, Token: token, Timeout: 30 * time.Second, Retries: 0,
	})
	var total *int
	students, err := client.FetchStudents(ctx, dapodik.FetchOptions{
		SekolahID: scope.SekolahID, NPSN: scope.NPSN, PDModule: "pdterdaftar",
		Limit: 1, MaxPages: 1,
		Progress: func(p dapodik.FetchProgress) { total = p.Total },
	})
	if err != nil {
		a.auditFromRequest(r, "dapodik_scope_tested", map[string]any{"scope_id": id, "ok": false})
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
		return
	}
	a.auditFromRequest(r, "dapodik_scope_tested", map[string]any{"scope_id": id, "ok": true, "rows": len(students)})
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"message": fmt.Sprintf("Koneksi scope %s berhasil. %d baris diterima.", scope.Name, len(students)),
		"rows":    len(students), "total": total, "scope": scope.Name,
	})
}

func (a *App) handleScopeDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	scope, err := a.Store.GetDapodikScope(r.Context(), id)
	if err != nil {
		a.renderError(w, r, http.StatusNotFound, "Scope tidak ditemukan.")
		return
	}
	if err := a.Store.DeleteDapodikScope(r.Context(), id); err != nil {
		a.renderError(w, r, http.StatusInternalServerError, "Gagal menghapus scope.")
		return
	}
	a.auditFromRequest(r, "dapodik_scope_deleted", map[string]any{"scope_id": id, "name": scope.Name})
	a.setFlash(w, "warning", "Scope "+scope.Name+" dihapus.")
	http.Redirect(w, r, "/admin/scopes", http.StatusSeeOther)
}

func validateScopeValues(v scopeFormValues) string {
	if v.Name == "" || len(v.Name) > 80 {
		return "Nama scope wajib diisi (maksimal 80 karakter)."
	}
	u, err := url.Parse(v.BaseURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "Base URL tidak valid. Gunakan http:// atau https://."
	}
	if v.SekolahID == "" {
		return "sekolah_id wajib diisi."
	}
	return ""
}

func (a *App) setScopeCredential(scopeID, kind, plain string) error {
	encoded, err := a.CredentialBox.Encrypt(plain)
	if err != nil {
		return err
	}
	return a.Store.SetScopeCredential(context.Background(), scopeID, kind, encoded)
}
