package jobs

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"dapodik-tool/internal/auth"
	"dapodik-tool/internal/registry"
	"dapodik-tool/internal/store"
)

func setupRunner(t *testing.T) (*Runner, *store.Store, *store.User) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "jobs.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	hash, _ := auth.HashPassword("rahasia123")
	if _, err := st.CreateUser(context.Background(), "admin", "Admin", hash, "admin"); err != nil {
		t.Fatal(err)
	}
	user, _ := st.GetUserByUsername(context.Background(), "admin")
	runner := NewRunner(st, slog.New(slog.NewTextHandler(io.Discard, nil)))
	return runner, st, user
}

func waitForStatus(t *testing.T, st *store.Store, id string, statuses ...string) *store.Job {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		job, err := st.GetJob(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		for _, status := range statuses {
			if job.Status == status {
				return job
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	job, _ := st.GetJob(context.Background(), id)
	t.Fatalf("timeout menunggu status %v, saat ini %s", statuses, job.Status)
	return nil
}

func TestRunnerSuccess(t *testing.T) {
	runner, st, user := setupRunner(t)
	runner.Register(registry.JobSpec{
		Key: "test.ok", Label: "Test OK", ModuleKey: "test",
		Roles: []auth.Role{auth.RoleAdmin},
		Run: func(j *registry.JobContext) (map[string]any, error) {
			if j.Secrets["cookie"] != "secret-cookie" {
				t.Error("secret tidak diteruskan")
			}
			j.Log("info", "halo", nil)
			j.AddItem(registry.JobItem{Nama: "Ani", NIPD: "1", Status: "ok"})
			return map[string]any{"updated": 1}, nil
		},
	})
	runner.Start(context.Background())
	id, err := runner.Submit(context.Background(), user, "test.ok", map[string]string{"x": "1"}, map[string]string{"cookie": "secret-cookie"})
	if err != nil {
		t.Fatal(err)
	}
	job := waitForStatus(t, st, id, "done")
	if !strings.Contains(job.Summary, `"updated":1`) {
		t.Fatalf("summary = %s", job.Summary)
	}
	if strings.Contains(job.PublicParams, "secret-cookie") {
		t.Fatal("secret leaked into public params")
	}
	items, _ := st.ListItems(context.Background(), id)
	if len(items) != 1 || items[0].Nama != "Ani" {
		t.Fatalf("items = %+v", items)
	}
	events, _ := st.EventsAfter(context.Background(), id, 0, 100)
	if len(events) < 3 {
		t.Fatalf("events = %d", len(events))
	}
}

func TestRunnerCancel(t *testing.T) {
	runner, st, user := setupRunner(t)
	runner.Register(registry.JobSpec{
		Key: "test.cancel", Label: "Test Cancel", ModuleKey: "test",
		Roles: []auth.Role{auth.RoleAdmin},
		Run: func(j *registry.JobContext) (map[string]any, error) {
			for i := 0; i < 500; i++ {
				if j.Cancelled() {
					return nil, errors.New("dibatalkan")
				}
				time.Sleep(10 * time.Millisecond)
			}
			return map[string]any{}, nil
		},
	})
	runner.Start(context.Background())
	id, err := runner.Submit(context.Background(), user, "test.cancel", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	job := waitForStatus(t, st, id, "running")
	if err := runner.Cancel(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	waitForStatus(t, st, id, "cancelled")
}

func TestRunnerRejectsSecretInPublicParams(t *testing.T) {
	runner, _, user := setupRunner(t)
	runner.Register(registry.JobSpec{
		Key: "test.secret", Label: "Test", ModuleKey: "test",
		Roles: []auth.Role{auth.RoleAdmin},
		Run:   func(j *registry.JobContext) (map[string]any, error) { return nil, nil },
	})
	if _, err := runner.Submit(context.Background(), user, "test.secret", map[string]string{"cookie": "x"}, nil); err == nil {
		t.Fatal("expected error for secret in public params")
	}
}
