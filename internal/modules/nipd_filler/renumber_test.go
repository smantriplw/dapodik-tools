package nipd_filler

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"dapodik-tool/internal/dapodik"
	"dapodik-tool/internal/registry"
	"dapodik-tool/internal/store"
)

func renumberStudents() []dapodik.Student {
	return []dapodik.Student{
		{Nama: "Ani", NISN: flex("111"), NIPD: flex("17979"), Rombel: flex("X A"), RegistrasiID: "r1"},
		{Nama: "Budi", NISN: flex("222"), NIPD: flex("17978"), Rombel: flex("X A"), RegistrasiID: "r2"},
		{Nama: "Cici", NISN: flex("333"), NIPD: flex("18000"), Rombel: flex("X B"), RegistrasiID: "r3"},
		{Nama: "Dodi", NISN: flex("444"), NIPD: flex(""), Rombel: flex("X B"), RegistrasiID: "r4"},
		{Nama: "Eka", NISN: flex("555"), NIPD: flex("18001"), Rombel: flex(""), RegistrasiID: "r5"},
	}
}

func makeRenumberPreviewJob(t *testing.T, st *store.Store, id, baseURL, fingerprint string) {
	t.Helper()
	params, _ := json.Marshal(map[string]string{
		"base_url": baseURL, "sekolah_id": "s1", "npsn": "1",
		"limit": "20000", "delay": "0", "pd_module": "pdterdaftar",
		"threshold": "17978", "start_nipd": "17979",
	})
	summary := fmt.Sprintf(`{"fingerprint":%q,"start_nipd":17979,"threshold":17978}`, fingerprint)
	if err := st.CreateJob(context.Background(), &store.Job{
		ID: id, ModuleKey: "nipd_filler", JobType: renumberPreviewJobType, Status: "done",
		PublicParams: string(params), Summary: summary, CreatedBy: 1,
	}); err != nil {
		t.Fatal(err)
	}
}

func TestRunRenumberPreview(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "renumber-preview.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	fake := newFakeDapodik(t, renumberStudents())

	var items []registry.JobItem
	ctx := &registry.JobContext{
		Ctx: context.Background(), Store: st,
		Params: map[string]string{
			"base_url": fake.server.URL, "sekolah_id": "s1", "npsn": "1",
			"limit": "20000", "delay": "0", "pd_module": "pdterdaftar",
			"threshold": "17978", "start_nipd": "17979",
		},
		Secrets:   map[string]string{"cookie": "test"},
		Log:       func(string, string, map[string]any) {},
		Progress:  func(int, int, string) {},
		AddItem:   func(registry.JobItem) {},
		AddItems:  func(list []registry.JobItem) { items = append(items, list...) },
		Cancelled: func() bool { return false },
	}
	summary, err := runRenumberPreview(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if summary["total_students"] != 5 || summary["eligible"] != 3 || summary["kept"] != 2 || summary["empty"] != 1 {
		t.Fatalf("summary = %+v", summary)
	}
	if summary["start_nipd"] != 17979 || summary["new_max"] != 17981 {
		t.Fatalf("range = %+v", summary)
	}
	if len(items) != 5 {
		t.Fatalf("items = %d", len(items))
	}
	byName := map[string]registry.JobItem{}
	for _, it := range items {
		byName[it.Nama] = it
	}
	if byName["Ani"].Status != "preview" || byName["Ani"].NIPD != "17979" {
		t.Fatalf("Ani = %+v", byName["Ani"])
	}
	if byName["Cici"].NIPD != "17980" || byName["Eka"].NIPD != "17981" {
		t.Fatalf("Cici/Eka = %+v/%+v", byName["Cici"], byName["Eka"])
	}
	if byName["Budi"].Status != "filled" || byName["Budi"].NIPD != "17978" {
		t.Fatalf("Budi = %+v", byName["Budi"])
	}
	if summary["conflict_count"] != 0 {
		t.Fatalf("conflicts = %+v", summary)
	}
}

func TestRunRenumberApply(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "renumber-apply.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	students := renumberStudents()
	fake := newFakeDapodik(t, students)
	makeRenumberPreviewJob(t, st, "pvr1", fake.server.URL, dapodik.Fingerprint(students))

	var items []registry.JobItem
	ctx := &registry.JobContext{
		Ctx: context.Background(), Store: st,
		Params: map[string]string{
			"preview_job_id": "pvr1",
			"groups":         `["X A","X B","(Tanpa Rombel)"]`,
			"delay":          "0",
		},
		Secrets:   map[string]string{"cookie": "test"},
		Log:       func(string, string, map[string]any) {},
		Progress:  func(int, int, string) {},
		AddItem:   func(it registry.JobItem) { items = append(items, it) },
		AddItems:  func([]registry.JobItem) {},
		Cancelled: func() bool { return false },
	}
	summary, err := runRenumberApply(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if fake.putCount() != 3 {
		t.Fatalf("put count = %d, want 3", fake.putCount())
	}
	if fake.puts[0]["nipd"] != "17979" || fake.puts[0]["registrasi_id"] != "r1" {
		t.Fatalf("put[0] = %+v", fake.puts[0])
	}
	if fake.puts[1]["nipd"] != "17980" || fake.puts[2]["nipd"] != "17981" {
		t.Fatalf("puts = %+v", fake.puts)
	}
	if summary["updated"] != 3 || summary["failed"] != 0 || summary["eligible"] != 3 {
		t.Fatalf("summary = %+v", summary)
	}
	if len(items) != 3 || items[0].OldNIPD != "17979" || items[0].NIPD != "17979" {
		t.Fatalf("items = %+v", items)
	}
}

func TestRunRenumberApplyRejectsConflict(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "renumber-conflict.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	students := renumberStudents()
	fake := newFakeDapodik(t, students)
	makeRenumberPreviewJob(t, st, "pvr2", fake.server.URL, dapodik.Fingerprint(students))

	ctx := &registry.JobContext{
		Ctx: context.Background(), Store: st,
		Params: map[string]string{
			"preview_job_id": "pvr2",
			"groups":         `["X B"]`,
			"delay":          "0",
		},
		Secrets: map[string]string{"cookie": "test"},
		Log:     func(string, string, map[string]any) {}, Progress: func(int, int, string) {},
		AddItem: func(registry.JobItem) {}, AddItems: func([]registry.JobItem) {},
		Cancelled: func() bool { return false },
	}
	if _, err := runRenumberApply(ctx); err == nil || !strings.Contains(err.Error(), "konflik") {
		t.Fatalf("err = %v, want conflict error", err)
	}
	if fake.putCount() != 0 {
		t.Fatalf("conflict wrote %d updates", fake.putCount())
	}
}

func TestRunRenumberPreviewOperator(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "renumber-operator.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	fake := newFakeDapodik(t, renumberStudents())

	var items []registry.JobItem
	ctx := &registry.JobContext{
		Ctx: context.Background(), Store: st,
		Params: map[string]string{
			"base_url": fake.server.URL, "sekolah_id": "s1", "npsn": "1",
			"limit": "20000", "delay": "0", "pd_module": "pdterdaftar",
			"threshold": "17979", "operator": "<", "start_nipd": "300",
		},
		Secrets:   map[string]string{"cookie": "test"},
		Log:       func(string, string, map[string]any) {},
		Progress:  func(int, int, string) {},
		AddItem:   func(registry.JobItem) {},
		AddItems:  func(list []registry.JobItem) { items = append(items, list...) },
		Cancelled: func() bool { return false },
	}
	summary, err := runRenumberPreview(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if summary["operator"] != "<" || summary["eligible"] != 1 || summary["new_max"] != 300 {
		t.Fatalf("summary = %+v", summary)
	}
	for _, it := range items {
		if it.Nama == "Budi" && (it.Status != "preview" || it.NIPD != "300") {
			t.Fatalf("Budi = %+v", it)
		}
		if it.Nama == "Ani" && it.Status == "preview" {
			t.Fatalf("Ani should not be selected: %+v", it)
		}
	}
}
