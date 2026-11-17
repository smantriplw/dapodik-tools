package app_test

import (
	"context"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"dapodik-tool/internal/app"
	"dapodik-tool/internal/auth"
	"dapodik-tool/internal/config"
)

var csrfPattern = regexp.MustCompile(`name="_csrf" value="([^"]+)"`)

func newTestApp(t *testing.T) *app.App {
	t.Helper()
	cfg := &config.Config{
		Env:           "test",
		AppName:       "SMANTI Tools Test",
		DatabasePath:  filepath.Join(t.TempDir(), "app.db"),
		SessionSecret: "test-secret-0123456789abcdef",
		LogLevel:      "error",
	}
	application, err := app.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { application.Close() })
	return application
}

func newClient() *http.Client {
	jar, _ := cookiejar.New(nil)
	return &http.Client{
		Jar: jar,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func login(t *testing.T, base string, client *http.Client, username, password string) {
	t.Helper()
	res, err := client.Get(base + "/login")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	match := csrfPattern.FindSubmatch(body)
	if len(match) != 2 {
		t.Fatalf("csrf tidak ditemukan di halaman login: %s", string(body[:200]))
	}
	form := url.Values{
		"_csrf": {string(match[1])}, "username": {username}, "password": {password},
	}
	res, err = client.PostForm(base+"/login", form)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("login status = %d", res.StatusCode)
	}
}

func get(t *testing.T, client *http.Client, url string) (int, string) {
	t.Helper()
	res, err := client.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	return res.StatusCode, string(body)
}

func TestLoginPagesAndRBAC(t *testing.T) {
	application := newTestApp(t)
	ctx := context.Background()

	hash, _ := auth.HashPassword("rahasia123")
	if _, err := application.Store.CreateUser(ctx, "admin", "Admin", hash, "admin"); err != nil {
		t.Fatal(err)
	}
	hash2, _ := auth.HashPassword("guru12345")
	if _, err := application.Store.CreateUser(ctx, "guru", "Guru", hash2, "guru"); err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(application.Handler())
	defer server.Close()

	admin := newClient()
	login(t, server.URL, admin, "admin", "rahasia123")

	status, body := get(t, admin, server.URL+"/")
	if status != http.StatusOK || !strings.Contains(body, "Pengisian NIPD") || !strings.Contains(body, "Cek Siswa") {
		t.Fatalf("dashboard status=%d", status)
	}
	if status, body := get(t, admin, server.URL+"/nipd-filler/"); status != http.StatusOK || !strings.Contains(body, "Pengisian NIPD") {
		t.Fatalf("nipd page status=%d", status)
	}
	if status, body := get(t, admin, server.URL+"/admin/users"); status != http.StatusOK || !strings.Contains(body, "Operator") {
		t.Fatalf("admin users status=%d", status)
	}
	if status, _ := get(t, admin, server.URL+"/admin/settings"); status != http.StatusOK {
		t.Fatalf("admin settings status=%d", status)
	}
	if status, _ := get(t, admin, server.URL+"/admin/scopes"); status != http.StatusOK {
		t.Fatalf("admin scopes status=%d", status)
	}
	if status, _ := get(t, admin, server.URL+"/admin/dapodik"); status != http.StatusSeeOther {
		t.Fatalf("legacy admin dapodik should redirect, got %d", status)
	}

	res, err := admin.Post(server.URL+"/nipd-filler/api/preview", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("csrf-less API status = %d, want 403", res.StatusCode)
	}

	guru := newClient()
	login(t, server.URL, guru, "guru", "guru12345")
	if status, _ := get(t, guru, server.URL+"/nipd-filler/"); status != http.StatusOK {
		t.Fatalf("guru nipd status=%d", status)
	}
	if status, _ := get(t, guru, server.URL+"/admin/users"); status != http.StatusForbidden {
		t.Fatalf("guru admin status=%d, want 403", status)
	}
	if status, _ := get(t, guru, server.URL+"/admin/scopes"); status != http.StatusForbidden {
		t.Fatalf("guru scopes status=%d, want 403", status)
	}
}

func TestUnauthenticatedRedirect(t *testing.T) {
	application := newTestApp(t)
	server := httptest.NewServer(application.Handler())
	defer server.Close()

	client := newClient()
	res, err := client.Get(server.URL + "/jobs")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusSeeOther || !strings.HasPrefix(res.Header.Get("Location"), "/login") {
		t.Fatalf("status=%d location=%s", res.StatusCode, res.Header.Get("Location"))
	}
}

func TestModuleJobRegistered(t *testing.T) {
	application := newTestApp(t)
	for _, key := range []string{"nipd.preview", "nipd.apply"} {
		if _, ok := application.Jobs.Spec(key); !ok {
			t.Fatalf("job spec %s tidak terdaftar", key)
		}
	}
}

func TestModuleToggleIsDynamic(t *testing.T) {
	application := newTestApp(t)
	ctx := context.Background()
	hash, _ := auth.HashPassword("rahasia123")
	if _, err := application.Store.CreateUser(ctx, "admin", "Admin", hash, "admin"); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(application.Handler())
	defer server.Close()
	client := newClient()
	login(t, server.URL, client, "admin", "rahasia123")

	if status, _ := get(t, client, server.URL+"/nipd-filler/"); status != http.StatusOK {
		t.Fatalf("module should start enabled, got %d", status)
	}
	status, body := get(t, client, server.URL+"/admin/modules")
	if status != http.StatusOK {
		t.Fatalf("modules page status=%d", status)
	}
	match := csrfPattern.FindSubmatch([]byte(body))
	if len(match) != 2 {
		t.Fatal("csrf not found on modules page")
	}
	res, err := client.PostForm(server.URL+"/admin/modules/nipd_filler/toggle", url.Values{"_csrf": {string(match[1])}})
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("toggle status=%d", res.StatusCode)
	}
	if status, _ := get(t, client, server.URL+"/nipd-filler/"); status != http.StatusNotFound {
		t.Fatalf("disabled module should be 404, got %d", status)
	}
	if _, dashboard := get(t, client, server.URL+"/"); strings.Contains(dashboard, `href="/nipd-filler/"`) {
		t.Fatal("disabled module still appears on dashboard")
	}
	if _, modulesPage := get(t, client, server.URL+"/admin/modules"); !strings.Contains(modulesPage, "Nonaktif") {
		t.Fatal("modules page does not show disabled state")
	}
	// Enable again without restart.
	_, body = get(t, client, server.URL+"/admin/modules")
	match = csrfPattern.FindSubmatch([]byte(body))
	res, err = client.PostForm(server.URL+"/admin/modules/nipd_filler/toggle", url.Values{"_csrf": {string(match[1])}})
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if status, _ := get(t, client, server.URL+"/nipd-filler/"); status != http.StatusOK {
		t.Fatalf("re-enabled module should be 200, got %d", status)
	}
}
