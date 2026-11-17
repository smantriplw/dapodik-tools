package jobs

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"

	"dapodik-tool/internal/auth"
	"dapodik-tool/internal/registry"
	"dapodik-tool/internal/store"
)

type Runner struct {
	store *store.Store
	log   *slog.Logger

	mu      sync.RWMutex
	specs   map[string]registry.JobSpec
	secrets map[string]map[string]string
	cancels map[string]*atomic.Bool

	queue   chan queuedJob
	started atomic.Bool
}

type queuedJob struct {
	id     string
	spec   registry.JobSpec
	user   *store.User
	params map[string]string
}

func NewRunner(st *store.Store, logger *slog.Logger) *Runner {
	if logger == nil {
		logger = slog.Default()
	}
	return &Runner{
		store:   st,
		log:     logger,
		specs:   make(map[string]registry.JobSpec),
		secrets: make(map[string]map[string]string),
		cancels: make(map[string]*atomic.Bool),
		queue:   make(chan queuedJob, 64),
	}
}

func (r *Runner) Register(spec registry.JobSpec) {
	if spec.Key == "" || spec.Run == nil {
		panic("job spec tidak valid: key dan Run wajib diisi")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.specs[spec.Key]; ok {
		panic("job spec duplikat: " + spec.Key)
	}
	r.specs[spec.Key] = spec
}

func (r *Runner) Spec(key string) (registry.JobSpec, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	spec, ok := r.specs[key]
	return spec, ok
}

func (r *Runner) Specs() []registry.JobSpec {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]registry.JobSpec, 0, len(r.specs))
	for _, spec := range r.specs {
		out = append(out, spec)
	}
	return out
}

func (r *Runner) Start(ctx context.Context) {
	if !r.started.CompareAndSwap(false, true) {
		return
	}
	if n, err := r.store.FailStaleJobs(ctx, "Server restart saat job berjalan. Cookie/token hilang, silakan ulangi."); err != nil {
		r.log.Error("gagal menandai job lama", "error", err)
	} else if n > 0 {
		r.log.Warn("job lama ditandai gagal", "jumlah", n)
	}
	go r.worker(ctx)
}

func (r *Runner) Submit(ctx context.Context, user *store.User, specKey string, public, secrets map[string]string) (string, error) {
	spec, ok := r.Spec(specKey)
	if !ok {
		return "", fmt.Errorf("jenis job tidak dikenal: %s", specKey)
	}
	if !auth.HasRole(user, spec.Roles...) {
		return "", fmt.Errorf("role tidak diizinkan untuk job ini")
	}
	for k := range public {
		lk := strings.ToLower(k)
		if strings.Contains(lk, "cookie") || strings.Contains(lk, "token") || strings.Contains(lk, "password") {
			return "", fmt.Errorf("parameter publik tidak boleh memuat data sensitif: %s", k)
		}
	}

	id, err := newJobID()
	if err != nil {
		return "", err
	}
	publicJSON, err := json.Marshal(public)
	if err != nil {
		return "", err
	}
	job := &store.Job{
		ID:           id,
		ModuleKey:    spec.ModuleKey,
		JobType:      spec.Key,
		Status:       "queued",
		PublicParams: string(publicJSON),
		CreatedBy:    user.ID,
	}
	if err := r.store.CreateJob(ctx, job); err != nil {
		return "", err
	}

	secretCopy := make(map[string]string, len(secrets))
	for k, v := range secrets {
		secretCopy[k] = v
	}
	r.mu.Lock()
	r.secrets[id] = secretCopy
	r.cancels[id] = &atomic.Bool{}
	r.mu.Unlock()

	_, _ = r.store.AppendEvent(ctx, id, "info", "Job masuk antrean", "{}")

	select {
	case r.queue <- queuedJob{id: id, spec: spec, user: user, params: public}:
		return id, nil
	default:
		_ = r.store.SetJobFailed(ctx, id, "Antrean job penuh, coba lagi.")
		r.cleanup(id)
		return "", fmt.Errorf("antrean job penuh")
	}
}

func (r *Runner) Cancel(ctx context.Context, job *store.Job) error {
	if job == nil {
		return store.ErrNotFound
	}
	if err := r.store.RequestCancel(ctx, job.ID); err != nil {
		return err
	}
	r.mu.RLock()
	flag := r.cancels[job.ID]
	r.mu.RUnlock()
	if flag != nil {
		flag.Store(true)
	}
	_, _ = r.store.AppendEvent(ctx, job.ID, "warn", "Permintaan pembatalan diterima", "{}")
	if job.Status == "queued" {
		_ = r.store.SetJobCancelled(ctx, job.ID, "Job dibatalkan sebelum berjalan")
	}
	return nil
}

func (r *Runner) worker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case q, ok := <-r.queue:
			if !ok {
				return
			}
			r.runOne(ctx, q)
		}
	}
}

func (r *Runner) runOne(ctx context.Context, q queuedJob) {
	defer r.cleanup(q.id)
	defer func() {
		if rec := recover(); rec != nil {
			msg := fmt.Sprintf("Job gagal karena panic: %v", rec)
			r.log.Error("panic pada job", "job", q.id, "panic", rec)
			_, _ = r.store.AppendEvent(ctx, q.id, "error", msg, "{}")
			_ = r.store.SetJobFailed(ctx, q.id, msg)
		}
	}()

	job, err := r.store.GetJob(ctx, q.id)
	if err != nil {
		r.log.Error("job hilang", "job", q.id, "error", err)
		return
	}
	if job.Status == "cancelled" || job.CancelRequested {
		_ = r.store.SetJobCancelled(ctx, job.ID, "Job dibatalkan sebelum berjalan")
		return
	}
	if err := r.store.SetJobRunning(ctx, q.id); err != nil {
		r.log.Error("gagal menandai job running", "job", q.id, "error", err)
		return
	}
	_, _ = r.store.AppendEvent(ctx, q.id, "info", "Job dimulai: "+q.spec.Label, "{}")

	r.mu.RLock()
	secretMap := r.secrets[q.id]
	cancelFlag := r.cancels[q.id]
	r.mu.RUnlock()
	if secretMap == nil {
		secretMap = map[string]string{}
	}
	if cancelFlag == nil {
		cancelFlag = &atomic.Bool{}
	}

	jobCtx := &registry.JobContext{
		Ctx:       ctx,
		JobID:     q.id,
		ModuleKey: q.spec.ModuleKey,
		User:      q.user,
		Store:     r.store,
		Params:    q.params,
		Secrets:   secretMap,
		Log: func(level, message string, data map[string]any) {
			dataJSON := marshalData(data)
			_, _ = r.store.AppendEvent(ctx, q.id, level, message, dataJSON)
			r.log.Info("job", "job", q.id, "level", level, "message", message)
		},
		Progress: func(current, total int, label string) {
			data := map[string]any{"current": current, "total": total}
			if label != "" {
				data["label"] = label
			}
			_, _ = r.store.AppendEvent(ctx, q.id, "progress", label, marshalData(data))
		},
		AddItem: func(it registry.JobItem) {
			_ = r.store.AddItem(ctx, q.id, toStoreItem(it))
		},
		AddItems: func(items []registry.JobItem) {
			list := make([]store.JobItem, 0, len(items))
			for _, it := range items {
				list = append(list, toStoreItem(it))
			}
			if err := r.store.AddItems(ctx, q.id, list); err != nil {
				r.log.Error("gagal menyimpan item job", "job", q.id, "error", err)
			}
		},
		Cancelled: func() bool {
			return cancelFlag.Load()
		},
	}

	summary, runErr := q.spec.Run(jobCtx)

	cancelled := cancelFlag.Load()
	switch {
	case cancelled:
		_, _ = r.store.AppendEvent(ctx, q.id, "warn", "Job dibatalkan oleh pengguna", "{}")
		_ = r.store.SetJobCancelled(ctx, q.id, "Dibatalkan oleh pengguna")
	case runErr != nil:
		msg := runErr.Error()
		_, _ = r.store.AppendEvent(ctx, q.id, "error", msg, "{}")
		_ = r.store.SetJobFailed(ctx, q.id, msg)
	default:
		summaryJSON := marshalData(summary)
		if summary == nil {
			summaryJSON = "{}"
		}
		_ = r.store.SetJobDone(ctx, q.id, summaryJSON)
		_, _ = r.store.AppendEvent(ctx, q.id, "success", "Job selesai", summaryJSON)
	}
}

func (r *Runner) cleanup(jobID string) {
	r.mu.Lock()
	delete(r.secrets, jobID)
	delete(r.cancels, jobID)
	r.mu.Unlock()
}

func toStoreItem(it registry.JobItem) store.JobItem {
	return store.JobItem{
		Group:        it.Group,
		Nama:         it.Nama,
		NISN:         it.NISN,
		RegistrasiID: it.RegistrasiID,
		OldNIPD:      it.OldNIPD,
		NIPD:         it.NIPD,
		Status:       it.Status,
		Message:      it.Message,
	}
}

func marshalData(v any) string {
	if v == nil {
		return "{}"
	}
	b, err := json.Marshal(v)
	if err != nil || string(b) == "null" {
		return "{}"
	}
	return string(b)
}

func newJobID() (string, error) {
	buf := make([]byte, 10)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
