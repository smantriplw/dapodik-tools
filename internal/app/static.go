package app

import (
	"io/fs"
	"net/http"

	"dapodik-tool/internal/web"
)

func (a *App) registerStatic() {
	coreFS, err := fs.Sub(web.FS, "static")
	if err != nil {
		a.Log.Error("static core tidak ditemukan", "error", err)
		return
	}
	coreServer := http.FileServer(http.FS(coreFS))
	a.publicRoute("GET /static/", http.StripPrefix("/static/", cacheStatic(coreServer)).ServeHTTP)

	for _, m := range a.modules {
		if m.Static == nil {
			continue
		}
		sub, err := fs.Sub(m.Static, "static")
		if err != nil {
			a.Log.Error("static modul tidak valid", "module", m.Key, "error", err)
			continue
		}
		server := http.FileServer(http.FS(sub))
		prefix := "/static/modules/" + m.Key + "/"
		a.publicRoute("GET "+prefix, http.StripPrefix(prefix, cacheStatic(server)).ServeHTTP)
	}

	a.publicRoute("GET /favicon.ico", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/static/img/logo-smanti.png", http.StatusPermanentRedirect)
	})
	a.publicRoute("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "app": a.Config.AppName})
	})
}

func cacheStatic(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=3600")
		next.ServeHTTP(w, r)
	})
}
