package nipd_filler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"dapodik-tool/internal/dapodik"
	"dapodik-tool/internal/registry"
	"dapodik-tool/internal/store"
)

func flex(v string) dapodik.FlexString {
	return dapodik.FlexString{Value: v, Valid: v != ""}
}

func fakeStudents() []dapodik.Student {
	return []dapodik.Student{
		{PesertaDidikID: "p1", Nama: "Ani", NISN: flex("111"), NIPD: flex(""), Rombel: flex("X A"), RegistrasiID: "r1"},
		{PesertaDidikID: "p2", Nama: "Budi", NISN: flex("222"), NIPD: flex("100"), Rombel: flex("X B"), RegistrasiID: "r2"},
		{PesertaDidikID: "p3", Nama: "Cici", NISN: flex("333"), NIPD: flex(""), Rombel: flex(""), RegistrasiID: "r3"},
	}
}

type fakeDapodik struct {
	mu       sync.Mutex
	puts     []map[string]string
	server   *httptest.Server
	students []dapodik.Student
}

func newFakeDapodik(t *testing.T, students []dapodik.Student) *fakeDapodik {
	t.Helper()
	f := &fakeDapodik{students: students}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/rest/PesertaDidik"):
			start := 0
			if v := r.URL.Query().Get("start"); v != "" {
				_, _ = fmt.Sscanf(v, "%d", &start)
			}
			if start > len(f.students) {
				start = len(f.students)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"success": true, "total": len(f.students), "rows": f.students[start:],
			})
		case r.Method == http.MethodPut && strings.Contains(r.URL.Path, "/rest/RegistrasiPesertaDidik/"):
			var payload map[string]string
			_ = json.NewDecoder(r.Body).Decode(&payload)
			f.mu.Lock()
			f.puts = append(f.puts, payload)
			f.mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{
				"success": true, "message": "Berhasil mengupdate RegistrasiPesertaDidik",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(f.server.Close)
	return f
}

func (f *fakeDapodik) putCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.puts)
}

func makePreviewJob(t *testing.T, st *store.Store, id, baseURL, fingerprint string) {
	t.Helper()
	params, _ := json.Marshal(map[string]string{
		"base_url": baseURL, "sekolah_id": "s1", "npsn": "1",
		"limit": "20000", "delay": "0", "pd_module": "pdterdaftar",
	})
	summary := fmt.Sprintf(`{"fingerprint":%q,"start_nipd":101}`, fingerprint)
	err := st.CreateJob(context.Background(), &store.Job{
		ID: id, ModuleKey: "nipd_filler", JobType: previewJobType, Status: "done",
		PublicParams: string(params), Summary: summary, CreatedBy: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestRunApplySelectedGroup(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "apply.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	students := fakeStudents()
	fake := newFakeDapodik(t, students)
	makePreviewJob(t, st, "pv1", fake.server.URL, dapodik.Fingerprint(students))

	var items []registry.JobItem
	ctx := &registry.JobContext{
		Ctx: context.Background(), Store: st,
		Params: map[string]string{
			"preview_job_id": "pv1",
			"groups":         `["X A"]`,
			"delay":          "0",
		},
		Secrets:   map[string]string{"cookie": "test", "token": ""},
		Log:       func(string, string, map[string]any) {},
		Progress:  func(int, int, string) {},
		AddItem:   func(it registry.JobItem) { items = append(items, it) },
		AddItems:  func([]registry.JobItem) {},
		Cancelled: func() bool { return false },
	}
	summary, err := runApply(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if fake.putCount() != 1 {
		t.Fatalf("put count = %d, want 1", fake.putCount())
	}
	if fake.puts[0]["nipd"] != "101" || fake.puts[0]["registrasi_id"] != "r1" {
		t.Fatalf("payload = %+v", fake.puts[0])
	}
	if len(items) != 1 || items[0].Status != "ok" || items[0].NIPD != "101" {
		t.Fatalf("items = %+v", items)
	}
	if summary["updated"] != 1 || summary["failed"] != 0 {
		t.Fatalf("summary = %+v", summary)
	}
}

func TestRunApplyRejectsStalePreview(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "stale.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	fake := newFakeDapodik(t, fakeStudents())
	makePreviewJob(t, st, "pv2", fake.server.URL, "fingerprint-lama")

	ctx := &registry.JobContext{
		Ctx: context.Background(), Store: st,
		Params:  map[string]string{"preview_job_id": "pv2", "groups": `["X A"]`, "delay": "0"},
		Secrets: map[string]string{"cookie": "test"},
		Log:     func(string, string, map[string]any) {}, Progress: func(int, int, string) {},
		AddItem: func(registry.JobItem) {}, AddItems: func([]registry.JobItem) {},
		Cancelled: func() bool { return false },
	}
	if _, err := runApply(ctx); err == nil || !strings.Contains(err.Error(), "data berubah") {
		t.Fatalf("err = %v, want stale preview error", err)
	}
	if fake.putCount() != 0 {
		t.Fatalf("stale preview wrote %d updates", fake.putCount())
	}
}

func TestRunPreviewStoresAllStudents(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "preview.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	fake := newFakeDapodik(t, fakeStudents())

	var items []registry.JobItem
	ctx := &registry.JobContext{
		Ctx: context.Background(), Store: st,
		Params: map[string]string{
			"base_url": fake.server.URL, "sekolah_id": "s1", "npsn": "1",
			"limit": "20000", "delay": "0", "pd_module": "pdterdaftar",
		},
		Secrets:   map[string]string{"cookie": "test"},
		Log:       func(string, string, map[string]any) {},
		Progress:  func(int, int, string) {},
		AddItem:   func(registry.JobItem) {},
		AddItems:  func(list []registry.JobItem) { items = append(items, list...) },
		Cancelled: func() bool { return false },
	}
	summary, err := runPreview(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if summary["total_students"] != 3 || summary["empty"] != 2 || summary["start_nipd"] != 101 {
		t.Fatalf("summary = %+v", summary)
	}
	if len(items) != 3 {
		t.Fatalf("items = %d", len(items))
	}
	byName := map[string]registry.JobItem{}
	for _, it := range items {
		byName[it.Nama] = it
	}
	if byName["Ani"].NIPD != "101" || byName["Ani"].Group != "X A" {
		t.Fatalf("Ani = %+v", byName["Ani"])
	}
	if byName["Cici"].NIPD != "102" || byName["Cici"].Group != dapodik.NoRombelLabel {
		t.Fatalf("Cici = %+v", byName["Cici"])
	}
	if byName["Budi"].Status != "filled" || byName["Budi"].NIPD != "100" {
		t.Fatalf("Budi = %+v", byName["Budi"])
	}
}
