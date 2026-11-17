package app

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"dapodik-tool/internal/auth"
	"dapodik-tool/internal/store"
)

func (a *App) registerJobRoutes() {
	a.route("GET /jobs/{id}", nil, a.handleJobDetail)
	a.route("GET /jobs/{id}/snapshot", nil, a.handleJobSnapshot)
	a.route("GET /jobs/{id}/events", nil, a.handleJobEvents)
	a.route("POST /jobs/{id}/cancel", nil, a.handleJobCancel)
	a.route("GET /jobs/{id}/download.csv", nil, a.handleJobDownloadCSV)
	a.route("GET /jobs/{id}/download.json", nil, a.handleJobDownloadJSON)
	a.route("GET /jobs/{id}/download.rollback.csv", nil, a.handleJobDownloadRollbackCSV)
}

func (a *App) jobForRequest(w http.ResponseWriter, r *http.Request) (*store.Job, bool) {
	id := r.PathValue("id")
	job, err := a.Store.GetJob(r.Context(), id)
	if err != nil {
		if wantsJSON(r) {
			writeJSON(w, http.StatusNotFound, map[string]any{"error": "job tidak ditemukan"})
		} else {
			a.renderError(w, r, http.StatusNotFound, "Job tidak ditemukan.")
		}
		return nil, false
	}
	user := userFrom(r)
	if user == nil || (!auth.HasRole(user, auth.RoleAdmin) && job.CreatedBy != user.ID) {
		if wantsJSON(r) {
			writeJSON(w, http.StatusForbidden, map[string]any{"error": "akses ditolak"})
		} else {
			a.renderError(w, r, http.StatusForbidden, "Anda tidak memiliki akses ke job ini.")
		}
		return nil, false
	}
	return job, true
}

type counters struct {
	Total   int `json:"total"`
	Current int `json:"current"`
	Done    int `json:"done"`
	Failed  int `json:"failed"`
	Skipped int `json:"skipped"`
}

func computeCounters(summary map[string]any, counts store.ItemCounts) counters {
	c := counters{Current: counts.Total, Done: counts.Done, Failed: counts.Failed, Skipped: counts.Skipped}
	if v, ok := intFromAny(summary["planned"]); ok && v > 0 {
		c.Total = v
	} else if v, ok := intFromAny(summary["total_students"]); ok && v > 0 {
		c.Total = v
	} else if v, ok := intFromAny(summary["total"]); ok && v > 0 {
		c.Total = v
	} else {
		c.Total = counts.Total
	}
	return c
}

func normalizeEventData(raw string) json.RawMessage {
	if strings.TrimSpace(raw) == "" || strings.TrimSpace(raw) == "null" || !json.Valid([]byte(raw)) {
		return json.RawMessage("{}")
	}
	return json.RawMessage(raw)
}

func intFromAny(v any) (int, bool) {
	switch n := v.(type) {
	case float64:
		return int(n), true
	case int:
		return n, true
	case json.Number:
		i, err := n.Int64()
		return int(i), err == nil
	case string:
		i, err := strconv.Atoi(n)
		return i, err == nil
	default:
		return 0, false
	}
}

func parseSummary(raw string) map[string]any {
	out := map[string]any{}
	if strings.TrimSpace(raw) == "" {
		return out
	}
	_ = json.Unmarshal([]byte(raw), &out)
	return out
}

type summaryRow struct {
	Label string
	Value string
}

func summaryRows(summary map[string]any) []summaryRow {
	specs := []struct{ key, label string }{
		{"mode", "Mode"},
		{"scope_name", "Scope Dapodik"},
		{"total_students", "Total siswa"},
		{"filled", "Sudah punya NIPD"},
		{"empty", "NIPD kosong"},
		{"highest_nipd", "NIPD tertinggi"},
		{"threshold", "Batas NIPD"},
		{"start_nipd", "NIPD awal"},
		{"eligible", "Eligible diubah"},
		{"kept", "Dipertahankan"},
		{"old_min", "NIPD lama minimum"},
		{"old_max", "NIPD lama maksimum"},
		{"new_max", "NIPD baru maksimum"},
		{"conflict_count", "Konflik NIPD"},
		{"no_rombel", "Siswa tanpa rombel"},
		{"planned", "Direncanakan"},
		{"updated", "Berhasil"},
		{"failed", "Gagal"},
		{"skipped", "Dilewati"},
		{"last_nipd", "NIPD terakhir"},
		{"groups", "Grup dipilih"},
		{"saved", "Tersimpan"},
	}
	var out []summaryRow
	for _, s := range specs {
		v, ok := summary[s.key]
		if !ok || v == nil {
			continue
		}
		value := fmt.Sprint(v)
		if value == "" || value == "[]" {
			continue
		}
		out = append(out, summaryRow{Label: s.label, Value: value})
	}
	return out
}

func (a *App) handleJobDetail(w http.ResponseWriter, r *http.Request) {
	job, ok := a.jobForRequest(w, r)
	if !ok {
		return
	}
	items, err := a.Store.ListItems(r.Context(), job.ID)
	if err != nil {
		a.renderError(w, r, http.StatusInternalServerError, "Gagal memuat item job.")
		return
	}
	truncated := false
	if len(items) > 200 {
		items = items[len(items)-200:]
		truncated = true
	}
	events, err := a.Store.EventsAfter(r.Context(), job.ID, 0, 300)
	if err != nil {
		a.Log.Error("gagal memuat event", "job", job.ID, "error", err)
	}
	counts, err := a.Store.ItemCounts(r.Context(), job.ID)
	if err != nil {
		a.Log.Error("gagal menghitung item", "job", job.ID, "error", err)
	}
	summary := parseSummary(job.Summary)
	label := job.JobType
	moduleName := job.ModuleKey
	if spec, found := a.Jobs.Spec(job.JobType); found {
		if spec.Label != "" {
			label = spec.Label
		}
		if spec.ModuleKey != "" {
			moduleName = spec.ModuleKey
		}
	}
	for _, m := range a.modules {
		if m.Key == job.ModuleKey {
			moduleName = m.Name
		}
	}
	var lastSeq int64
	if len(events) > 0 {
		lastSeq = events[len(events)-1].Seq
	}
	a.RenderPage(w, r, "job_detail.html", map[string]any{
		"Title":          "Job " + shortID(job.ID),
		"LastSeq":        lastSeq,
		"Active":         "/jobs",
		"Job":            job,
		"Items":          items,
		"ItemsTruncated": truncated,
		"Events":         events,
		"SummaryRows":    summaryRows(summary),
		"Counters":       computeCounters(summary, counts),
		"ModuleName":     moduleName,
		"JobLabel":       label,
	})
}

func (a *App) handleJobSnapshot(w http.ResponseWriter, r *http.Request) {
	job, ok := a.jobForRequest(w, r)
	if !ok {
		return
	}
	items, err := a.Store.ListItems(r.Context(), job.ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "gagal memuat item"})
		return
	}
	if len(items) > 200 {
		items = items[len(items)-200:]
	}
	after, _ := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
	events, err := a.Store.EventsAfter(r.Context(), job.ID, after, 500)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "gagal memuat event"})
		return
	}
	sseEvents := make([]map[string]any, 0, len(events))
	for _, e := range events {
		sseEvents = append(sseEvents, map[string]any{
			"seq": e.Seq, "ts": e.TS, "level": e.Level, "message": e.Message, "data": normalizeEventData(e.Data),
		})
	}
	counts, err := a.Store.ItemCounts(r.Context(), job.ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "gagal menghitung item"})
		return
	}
	summary := json.RawMessage(job.Summary)
	if len(summary) == 0 || !json.Valid(summary) {
		summary = json.RawMessage("{}")
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"job": map[string]any{
			"id": job.ID, "status": job.Status, "error": job.Error,
			"created_at": job.CreatedAt, "started_at": job.StartedAt.String,
			"finished_at": job.FinishedAt.String,
		},
		"summary":  summary,
		"items":    items,
		"events":   sseEvents,
		"counters": computeCounters(parseSummary(job.Summary), counts),
	})
}

func (a *App) handleJobEvents(w http.ResponseWriter, r *http.Request) {
	job, ok := a.jobForRequest(w, r)
	if !ok {
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming tidak didukung.", http.StatusInternalServerError)
		return
	}
	h := w.Header()
	h.Set("Content-Type", "text/event-stream; charset=utf-8")
	h.Set("Cache-Control", "no-cache, no-transform")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no")

	last, _ := strconv.ParseInt(r.Header.Get("Last-Event-ID"), 10, 64)
	if last == 0 {
		last, _ = strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
	}
	ticker := time.NewTicker(400 * time.Millisecond)
	defer ticker.Stop()
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()

	send := func(event string, id int64, payload any) bool {
		if id > 0 {
			if _, err := fmt.Fprintf(w, "id: %d\n", id); err != nil {
				return false
			}
		}
		if _, err := fmt.Fprintf(w, "event: %s\n", event); err != nil {
			return false
		}
		b, err := json.Marshal(payload)
		if err != nil {
			b = []byte("{}")
		}
		if _, err := fmt.Fprintf(w, "data: %s\n\n", b); err != nil {
			return false
		}
		return true
	}

	for {
		events, err := a.Store.EventsAfter(r.Context(), job.ID, last, 500)
		if err != nil {
			return
		}
		for _, e := range events {
			payload := map[string]any{
				"seq": e.Seq, "ts": e.TS, "level": e.Level,
				"message": e.Message, "data": normalizeEventData(e.Data),
			}
			if !send("message", e.Seq, payload) {
				return
			}
			last = e.Seq
		}
		if len(events) > 0 {
			flusher.Flush()
		}

		current, err := a.Store.GetJob(r.Context(), job.ID)
		if err == nil && (current.Status == "done" || current.Status == "failed" || current.Status == "cancelled") {
			if len(events) == 0 {
				_ = send("end", 0, map[string]any{"status": current.Status})
				flusher.Flush()
				return
			}
		}
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
		case <-heartbeat.C:
			if _, err := fmt.Fprint(w, ": keep-alive\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func (a *App) handleJobCancel(w http.ResponseWriter, r *http.Request) {
	job, ok := a.jobForRequest(w, r)
	if !ok {
		return
	}
	if job.Status != "queued" && job.Status != "running" {
		if wantsJSON(r) {
			writeJSON(w, http.StatusConflict, map[string]any{"error": "job sudah selesai"})
		} else {
			a.setFlash(w, "warning", "Job sudah selesai.")
			http.Redirect(w, r, "/jobs/"+job.ID, http.StatusSeeOther)
		}
		return
	}
	if err := a.Jobs.Cancel(r.Context(), job); err != nil {
		if wantsJSON(r) {
			writeJSON(w, http.StatusConflict, map[string]any{"error": err.Error()})
		} else {
			a.setFlash(w, "danger", "Gagal membatalkan job.")
			http.Redirect(w, r, "/jobs/"+job.ID, http.StatusSeeOther)
		}
		return
	}
	a.auditFromRequest(r, "job_cancelled", map[string]any{"job_id": job.ID})
	if wantsJSON(r) {
		writeJSON(w, http.StatusOK, map[string]any{"status": "cancelling"})
		return
	}
	a.setFlash(w, "warning", "Permintaan pembatalan dikirim.")
	http.Redirect(w, r, "/jobs/"+job.ID, http.StatusSeeOther)
}

func (a *App) handleJobDownloadCSV(w http.ResponseWriter, r *http.Request) {
	job, ok := a.jobForRequest(w, r)
	if !ok {
		return
	}
	items, err := a.Store.ListItems(r.Context(), job.ID)
	if err != nil {
		http.Error(w, "Gagal memuat item.", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Disposition", `attachment; filename="job-`+job.ID+`.csv"`)
	_, _ = w.Write([]byte{0xEF, 0xBB, 0xBF})
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"rombel", "nama", "nisn", "registrasi_id", "nipd_lama", "nipd_baru", "status", "pesan"})
	for _, it := range items {
		_ = cw.Write([]string{
			csvSafe(it.Group), csvSafe(it.Nama), csvSafe(it.NISN), csvSafe(it.RegistrasiID),
			csvSafe(it.OldNIPD), csvSafe(it.NIPD), csvSafe(it.Status), csvSafe(it.Message),
		})
	}
	cw.Flush()
}

func (a *App) handleJobDownloadRollbackCSV(w http.ResponseWriter, r *http.Request) {
	job, ok := a.jobForRequest(w, r)
	if !ok {
		return
	}
	if job.JobType != "nipd.renumber.apply" {
		http.Error(w, "Rollback hanya tersedia untuk job renumber NIPD.", http.StatusBadRequest)
		return
	}
	items, err := a.Store.ListItems(r.Context(), job.ID)
	if err != nil {
		http.Error(w, "Gagal memuat item.", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Disposition", `attachment; filename="rollback-`+job.ID+`.csv"`)
	_, _ = w.Write([]byte{0xEF, 0xBB, 0xBF})
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"registrasi_id", "nama", "nisn", "rombel", "nipd_lama", "nipd_baru", "status"})
	for _, it := range items {
		if it.Status != "ok" {
			continue
		}
		_ = cw.Write([]string{
			csvSafe(it.RegistrasiID), csvSafe(it.Nama), csvSafe(it.NISN), csvSafe(it.Group),
			csvSafe(it.OldNIPD), csvSafe(it.NIPD), csvSafe(it.Status),
		})
	}
	cw.Flush()
}

func (a *App) handleJobDownloadJSON(w http.ResponseWriter, r *http.Request) {
	job, ok := a.jobForRequest(w, r)
	if !ok {
		return
	}
	items, err := a.Store.ListItems(r.Context(), job.ID)
	if err != nil {
		http.Error(w, "Gagal memuat item.", http.StatusInternalServerError)
		return
	}
	summary := json.RawMessage(job.Summary)
	if !json.Valid(summary) {
		summary = json.RawMessage("{}")
	}
	payload := map[string]any{
		"id":            job.ID,
		"module_key":    job.ModuleKey,
		"job_type":      job.JobType,
		"status":        job.Status,
		"created_at":    job.CreatedAt,
		"started_at":    job.StartedAt.String,
		"finished_at":   job.FinishedAt.String,
		"summary":       summary,
		"error":         job.Error,
		"items":         items,
		"public_params": json.RawMessage(job.PublicParams),
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Disposition", `attachment; filename="job-`+job.ID+`.json"`)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(payload)
}

func csvSafe(v string) string {
	if v == "" {
		return v
	}
	switch v[0] {
	case '=', '+', '-', '@', '\t', '\r':
		return "'" + v
	}
	return v
}
