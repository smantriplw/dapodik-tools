package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	st, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func TestUsersAndSettings(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()

	id, err := st.CreateUser(ctx, "admin", "Admin", "hash", "admin")
	if err != nil {
		t.Fatal(err)
	}
	user, err := st.GetUserByUsername(ctx, "ADMIN")
	if err != nil {
		t.Fatal(err)
	}
	if user.ID != id || user.Role != "admin" || !user.IsActive {
		t.Fatalf("unexpected user: %+v", user)
	}
	if err := st.UpdateUser(ctx, id, "Admin Baru", "guru", false); err != nil {
		t.Fatal(err)
	}
	user, _ = st.GetUserByID(ctx, id)
	if user.Name != "Admin Baru" || user.Role != "guru" || user.IsActive {
		t.Fatalf("update failed: %+v", user)
	}
	if err := st.SetSetting(ctx, "x", "1"); err != nil {
		t.Fatal(err)
	}
	if v, ok, _ := st.GetSetting(ctx, "x"); !ok || v != "1" {
		t.Fatalf("setting = %q %v", v, ok)
	}
	if err := st.SeedSettings(ctx, map[string]string{"x": "2", "y": "3"}); err != nil {
		t.Fatal(err)
	}
	if v, _, _ := st.GetSetting(ctx, "x"); v != "1" {
		t.Fatalf("seed should not overwrite: %q", v)
	}
}

func TestJobsEventsItemsAndCounts(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	job := &Job{ID: "job1", ModuleKey: "m", JobType: "t", CreatedBy: 1}
	if err := st.CreateJob(ctx, job); err != nil {
		t.Fatal(err)
	}
	if err := st.SetJobRunning(ctx, job.ID); err != nil {
		t.Fatal(err)
	}
	if err := st.AddItems(ctx, job.ID, []JobItem{
		{Group: "X A", Nama: "A", Status: "ok"},
		{Group: "X A", Nama: "B", Status: "failed"},
		{Group: "X A", Nama: "C", Status: "skipped"},
	}); err != nil {
		t.Fatal(err)
	}
	counts, err := st.ItemCounts(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if counts.Total != 3 || counts.Done != 1 || counts.Failed != 1 || counts.Skipped != 1 {
		t.Fatalf("counts = %+v", counts)
	}
	seq1, _ := st.AppendEvent(ctx, job.ID, "info", "one", "{}")
	seq2, _ := st.AppendEvent(ctx, job.ID, "error", "two", `{"x":1}`)
	events, err := st.EventsAfter(ctx, job.ID, seq1, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Seq != seq2 || events[0].Message != "two" {
		t.Fatalf("events = %+v", events)
	}
	if err := st.SetJobDone(ctx, job.ID, `{"ok":true}`); err != nil {
		t.Fatal(err)
	}
	got, err := st.GetJob(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "done" || got.Summary != `{"ok":true}` {
		t.Fatalf("job = %+v", got)
	}
}

func TestModuleStatesAndSchema(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	if err := st.SyncModules(ctx, []string{"a", "b"}); err != nil {
		t.Fatal(err)
	}
	states, err := st.ListModuleStates(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !states["a"] || !states["b"] {
		t.Fatalf("states = %+v", states)
	}
	if err := st.SetModuleEnabled(ctx, "a", false); err != nil {
		t.Fatal(err)
	}
	states, _ = st.ListModuleStates(ctx)
	if states["a"] {
		t.Fatal("module a should be disabled")
	}
	ddl := `CREATE TABLE IF NOT EXISTS mod_table (id INTEGER PRIMARY KEY);`
	if err := st.EnsureSchema(ctx, "a", 1, ddl); err != nil {
		t.Fatal(err)
	}
	if err := st.EnsureSchema(ctx, "a", 1, ddl); err != nil {
		t.Fatalf("idempotent migration failed: %v", err)
	}
}

func TestCredentials(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	if _, _, ok, err := st.GetCredential(ctx, "x"); err != nil || ok {
		t.Fatalf("initial credential: ok=%v err=%v", ok, err)
	}
	if err := st.SetCredential(ctx, "x", "cipher"); err != nil {
		t.Fatal(err)
	}
	value, updated, ok, err := st.GetCredential(ctx, "x")
	if err != nil || !ok || value != "cipher" || updated == "" {
		t.Fatalf("credential = %q %q %v %v", value, updated, ok, err)
	}
	if err := st.DeleteCredential(ctx, "x"); err != nil {
		t.Fatal(err)
	}
	if _, _, ok, _ := st.GetCredential(ctx, "x"); ok {
		t.Fatal("credential belum terhapus")
	}
}

func TestPrune(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	old := time.Now().UTC().AddDate(0, 0, -120).Format(time.RFC3339Nano)
	if _, err := st.DB().ExecContext(ctx,
		`INSERT INTO jobs(id, module_key, job_type, status, created_by, created_at) VALUES (?,?,?,?,?,?)`,
		"old", "m", "t", "done", 1, old,
	); err != nil {
		t.Fatal(err)
	}
	_, _ = st.AppendEvent(ctx, "old", "info", "x", "{}")
	_ = st.AddItem(ctx, "old", JobItem{Nama: "A"})
	events, items, err := st.Prune(ctx, 90)
	if err != nil {
		t.Fatal(err)
	}
	if events != 1 || items != 1 {
		t.Fatalf("prune = %d/%d", events, items)
	}
}

func TestDapodikScopes(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	userID, err := st.CreateUser(ctx, "operator1", "Operator 1", "hash", "guru")
	if err != nil {
		t.Fatal(err)
	}
	scopeA := &DapodikScope{ID: "scope-a", Name: "Dapodik A", BaseURL: "https://a.example", SekolahID: "s-a", NPSN: "1"}
	scopeB := &DapodikScope{ID: "scope-b", Name: "Dapodik B", BaseURL: "https://b.example", SekolahID: "s-b", NPSN: "2"}
	if err := st.CreateDapodikScope(ctx, scopeA); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateDapodikScope(ctx, scopeB); err != nil {
		t.Fatal(err)
	}
	if err := st.SetScopeCredential(ctx, "scope-a", "cookie", "cipher-cookie"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetScopeCredential(ctx, "scope-a", "token", "cipher-token"); err != nil {
		t.Fatal(err)
	}
	got, err := st.GetDapodikScope(ctx, "scope-a")
	if err != nil {
		t.Fatal(err)
	}
	if !got.CookieConfigured || !got.TokenConfigured || got.Name != "Dapodik A" {
		t.Fatalf("scope meta = %+v", got)
	}
	all, err := st.ListDapodikScopes(ctx)
	if err != nil || len(all) != 2 {
		t.Fatalf("list scopes = %d err=%v", len(all), err)
	}
	if err := st.SetUserScopes(ctx, userID, []string{"scope-a"}); err != nil {
		t.Fatal(err)
	}
	visible, err := st.ListScopesForUser(ctx, userID, false)
	if err != nil || len(visible) != 1 || visible[0].ID != "scope-a" {
		t.Fatalf("visible scopes = %+v err=%v", visible, err)
	}
	if adminVisible, err := st.ListScopesForUser(ctx, userID, true); err != nil || len(adminVisible) != 2 {
		t.Fatalf("admin scopes = %d err=%v", len(adminVisible), err)
	}
	if err := st.UpdateDapodikScope(ctx, "scope-a", "Dapodik A Baru", "https://a2.example", "s-a2", "3"); err != nil {
		t.Fatal(err)
	}
	got, _ = st.GetDapodikScope(ctx, "scope-a")
	if got.Name != "Dapodik A Baru" || got.BaseURL != "https://a2.example" {
		t.Fatalf("updated scope = %+v", got)
	}
	if err := st.DeleteDapodikScope(ctx, "scope-a"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetDapodikScope(ctx, "scope-a"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted scope err = %v", err)
	}
	if ids, _ := st.UserScopeIDs(ctx, userID); len(ids) != 0 {
		t.Fatalf("user scopes after delete = %v", ids)
	}
}
