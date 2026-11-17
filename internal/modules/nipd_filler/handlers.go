package nipd_filler

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"dapodik-tool/internal/auth"
	"dapodik-tool/internal/dapodik"
	"dapodik-tool/internal/registry"
	"dapodik-tool/internal/store"
)

const (
	previewJobType = "nipd.preview"
	applyJobType   = "nipd.apply"
)

type previewRequest struct {
	ScopeID   string  `json:"scope_id"`
	Limit     int     `json:"limit"`
	Delay     float64 `json:"delay"`
	StartNIPD *int    `json:"start_nipd"`
}

type recalcRequest struct {
	JobID     string   `json:"job_id"`
	Groups    []string `json:"groups"`
	StartNIPD *int     `json:"start_nipd"`
}

type applyRequest struct {
	PreviewJobID string   `json:"preview_job_id"`
	Groups       []string `json:"groups"`
	Delay        float64  `json:"delay"`
	StartNIPD    *int     `json:"start_nipd"`
}

type apiGroup struct {
	Label    string `json:"label"`
	Total    int    `json:"total"`
	Empty    int    `json:"empty"`
	Eligible int    `json:"eligible"`
}

type apiItem struct {
	Group        string `json:"group"`
	Nama         string `json:"nama"`
	NISN         string `json:"nisn"`
	RegistrasiID string `json:"registrasi_id"`
	OldNIPD      string `json:"old_nipd"`
	NIPD         string `json:"nipd"`
	Status       string `json:"status"`
}

type previewSummary struct {
	Mode          string `json:"mode"`
	TotalStudents int    `json:"total_students"`
	Filled        int    `json:"filled"`
	Empty         int    `json:"empty"`
	HighestNIPD   int    `json:"highest_nipd"`
	StartNIPD     int    `json:"start_nipd"`
	NoRombel      int    `json:"no_rombel"`
	Groups        int    `json:"groups"`
	Items         int    `json:"items"`
	Fingerprint   string `json:"fingerprint"`

	Threshold     int    `json:"threshold,omitempty"`
	Operator      string `json:"operator,omitempty"`
	Eligible      int    `json:"eligible,omitempty"`
	Kept          int    `json:"kept,omitempty"`
	Skipped       int    `json:"skipped,omitempty"`
	OldMin        int    `json:"old_min,omitempty"`
	OldMax        int    `json:"old_max,omitempty"`
	NewMin        int    `json:"new_min,omitempty"`
	NewMax        int    `json:"new_max,omitempty"`
	ConflictCount int    `json:"conflict_count,omitempty"`
	Conflicts     []int  `json:"conflicts,omitempty"`
}

type previewData struct {
	Summary previewSummary `json:"summary"`
	Groups  []apiGroup     `json:"groups"`
	Items   []apiItem      `json:"items"`
}

func previewHandler(s *registry.Services) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req previewRequest
		if err := decodeJSON(r, &req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Body JSON tidak valid."})
			return
		}
		if s.Scopes == nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "Scope Dapodik belum tersedia."})
			return
		}
		creds, err := s.Scopes.Resolve(s.CurrentUser(r), strings.TrimSpace(req.ScopeID))
		if err != nil {
			writeJSON(w, scopeStatus(err), map[string]any{"error": err.Error()})
			return
		}
		u, err := url.Parse(creds.BaseURL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Base URL scope tidak valid."})
			return
		}
		if creds.SekolahID == "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "sekolah_id scope belum diatur."})
			return
		}
		if creds.Cookie == "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": fmt.Sprintf("Scope %s belum memiliki cookie.", creds.Name)})
			return
		}
		if req.Limit <= 0 {
			req.Limit = 20000
		}
		if req.Limit > 50000 {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Limit maksimal 50000."})
			return
		}
		if req.Delay < 0 || req.Delay > 10 {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Jeda harus antara 0 dan 10 detik."})
			return
		}
		if req.StartNIPD != nil && *req.StartNIPD < 0 {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "NIPD awal tidak boleh negatif."})
			return
		}

		public := map[string]string{
			"scope_id":   creds.ID,
			"scope_name": creds.Name,
			"base_url":   creds.BaseURL,
			"sekolah_id": creds.SekolahID,
			"npsn":       creds.NPSN,
			"limit":      strconv.Itoa(req.Limit),
			"delay":      strconv.FormatFloat(req.Delay, 'f', -1, 64),
			"pd_module":  "pdterdaftar",
		}
		if req.StartNIPD != nil {
			public["start_nipd"] = strconv.Itoa(*req.StartNIPD)
		}
		secrets := map[string]string{"cookie": creds.Cookie, "token": creds.Token}
		user := s.CurrentUser(r)
		jobID, err := s.Jobs.Submit(r.Context(), user, previewJobType, public, secrets)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
		s.Audit(r, "nipd_preview_submitted", map[string]any{
			"job_id": jobID, "scope_id": creds.ID, "sekolah_id": creds.SekolahID,
		})
		writeJSON(w, http.StatusAccepted, map[string]any{"job_id": jobID})
	}
}

func applyHandler(s *registry.Services) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req applyRequest
		if err := decodeJSON(r, &req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Body JSON tidak valid."})
			return
		}
		req.PreviewJobID = strings.TrimSpace(req.PreviewJobID)
		if req.PreviewJobID == "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "preview_job_id wajib diisi."})
			return
		}
		if len(req.Groups) == 0 {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Pilih minimal satu rombel."})
			return
		}
		if req.Delay < 0 || req.Delay > 10 {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Jeda harus antara 0 dan 10 detik."})
			return
		}
		preview, err := s.Store.GetJob(r.Context(), req.PreviewJobID)
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]any{"error": "Preview tidak ditemukan."})
			return
		}
		if !canAccessJob(s.CurrentUser(r), preview) {
			writeJSON(w, http.StatusForbidden, map[string]any{"error": "Preview bukan milik Anda."})
			return
		}
		if preview.JobType != previewJobType {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Job tersebut bukan preview NIPD."})
			return
		}
		if preview.Status != "done" {
			writeJSON(w, http.StatusConflict, map[string]any{"error": "Preview belum selesai."})
			return
		}
		var previewParams map[string]string
		if err := json.Unmarshal([]byte(preview.PublicParams), &previewParams); err != nil {
			writeJSON(w, http.StatusConflict, map[string]any{"error": "Parameter preview tidak valid. Muat preview ulang."})
			return
		}
		scopeID := strings.TrimSpace(previewParams["scope_id"])
		if scopeID == "" {
			writeJSON(w, http.StatusConflict, map[string]any{"error": "Preview dibuat sebelum scope Dapodik diterapkan. Muat preview ulang."})
			return
		}
		if s.Scopes == nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "Scope Dapodik belum tersedia."})
			return
		}
		creds, err := s.Scopes.Resolve(s.CurrentUser(r), scopeID)
		if err != nil {
			writeJSON(w, scopeStatus(err), map[string]any{"error": err.Error()})
			return
		}
		if creds.Cookie == "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": fmt.Sprintf("Scope %s belum memiliki cookie.", creds.Name)})
			return
		}
		if previewParams["base_url"] != creds.BaseURL ||
			previewParams["sekolah_id"] != creds.SekolahID ||
			previewParams["npsn"] != creds.NPSN {
			writeJSON(w, http.StatusConflict, map[string]any{"error": "Pengaturan scope berubah sejak preview. Muat preview ulang."})
			return
		}
		items, err := s.Store.ListItems(r.Context(), preview.ID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "Gagal memuat data preview."})
			return
		}
		valid := map[string]bool{}
		for _, it := range items {
			valid[it.Group] = true
		}
		groups := make([]string, 0, len(req.Groups))
		seen := map[string]bool{}
		for _, g := range req.Groups {
			g = strings.TrimSpace(g)
			if g == "" || seen[g] {
				continue
			}
			if !valid[g] {
				writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Rombel tidak ada di preview: " + g})
				return
			}
			seen[g] = true
			groups = append(groups, g)
		}
		if len(groups) == 0 {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Pilih minimal satu rombel."})
			return
		}
		groupsJSON, _ := json.Marshal(groups)
		public := map[string]string{
			"preview_job_id": preview.ID,
			"groups":         string(groupsJSON),
			"delay":          strconv.FormatFloat(req.Delay, 'f', -1, 64),
			"scope_id":       creds.ID,
			"scope_name":     creds.Name,
		}
		if req.StartNIPD != nil {
			if *req.StartNIPD < 0 {
				writeJSON(w, http.StatusBadRequest, map[string]any{"error": "NIPD awal tidak boleh negatif."})
				return
			}
			public["start_nipd"] = strconv.Itoa(*req.StartNIPD)
		}
		secrets := map[string]string{"cookie": creds.Cookie, "token": creds.Token}
		jobID, err := s.Jobs.Submit(r.Context(), s.CurrentUser(r), applyJobType, public, secrets)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
		s.Audit(r, "nipd_apply_submitted", map[string]any{
			"job_id": jobID, "preview_job_id": preview.ID, "scope_id": creds.ID, "groups": groups,
		})
		writeJSON(w, http.StatusAccepted, map[string]any{"job_id": jobID})
	}
}

func recalcHandler(s *registry.Services) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req recalcRequest
		if err := decodeJSON(r, &req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Body JSON tidak valid."})
			return
		}
		preview, ok := loadPreviewForUser(s, r, req.JobID, w)
		if !ok {
			return
		}
		items, err := s.Store.ListItems(r.Context(), preview.ID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "Gagal memuat data preview."})
			return
		}
		start := req.StartNIPD
		if start == nil {
			start = previewStart(preview)
		}
		data, err := buildPreviewData(items, req.Groups, start)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, data)
	}
}

func previewDataHandler(s *registry.Services) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		preview, ok := loadPreviewForUser(s, r, r.PathValue("job_id"), w)
		if !ok {
			return
		}
		items, err := s.Store.ListItems(r.Context(), preview.ID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "Gagal memuat data preview."})
			return
		}
		data, err := buildPreviewData(items, nil, previewStart(preview))
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, data)
	}
}

func loadPreviewForUser(s *registry.Services, r *http.Request, jobID string, w http.ResponseWriter) (*store.Job, bool) {
	job, err := s.Store.GetJob(r.Context(), strings.TrimSpace(jobID))
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "Preview tidak ditemukan."})
		return nil, false
	}
	if !canAccessJob(s.CurrentUser(r), job) {
		writeJSON(w, http.StatusForbidden, map[string]any{"error": "Preview bukan milik Anda."})
		return nil, false
	}
	if job.JobType != previewJobType {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Job tersebut bukan preview NIPD."})
		return nil, false
	}
	if job.Status != "done" {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "Preview belum selesai."})
		return nil, false
	}
	return job, true
}

func scopeStatus(err error) int {
	if errors.Is(err, registry.ErrScopeNotAllowed) {
		return http.StatusForbidden
	}
	return http.StatusBadRequest
}

func canAccessJob(user *store.User, job *store.Job) bool {
	if user == nil || job == nil {
		return false
	}
	return auth.HasRole(user, auth.RoleAdmin) || job.CreatedBy == user.ID
}

func buildPreviewData(items []store.JobItem, groups []string, start *int) (*previewData, error) {
	students := make([]dapodik.Student, 0, len(items))
	for _, it := range items {
		rombel := it.Group
		if rombel == dapodik.NoRombelLabel {
			rombel = ""
		}
		students = append(students, dapodik.Student{
			Nama:         it.Nama,
			NISN:         dapodik.FlexString{Value: it.NISN, Valid: it.NISN != ""},
			NIPD:         dapodik.FlexString{Value: it.OldNIPD, Valid: it.OldNIPD != ""},
			Rombel:       dapodik.FlexString{Value: rombel, Valid: rombel != ""},
			RegistrasiID: it.RegistrasiID,
		})
	}
	var selected map[string]bool
	if groups != nil {
		selected = make(map[string]bool, len(groups))
		for _, g := range groups {
			selected[g] = true
		}
	}
	plan, err := dapodik.BuildPlan(students, dapodik.PlanOptions{StartNIPD: start, SelectedGroups: selected})
	if err != nil {
		return nil, err
	}
	out := &previewData{
		Summary: previewSummary{
			Mode:          "preview",
			TotalStudents: plan.TotalStudents,
			Filled:        plan.Filled,
			Empty:         plan.Empty,
			HighestNIPD:   plan.HighestNIPD,
			StartNIPD:     plan.StartNIPD,
			NoRombel:      plan.NoRombel,
			Groups:        len(plan.Groups),
			Items:         len(plan.Items),
			Fingerprint:   plan.Fingerprint,
		},
	}
	for _, g := range plan.Groups {
		out.Groups = append(out.Groups, apiGroup{Label: g.Label, Total: g.Total, Empty: g.Empty})
	}
	for _, it := range plan.Items {
		out.Items = append(out.Items, apiItem{
			Group: it.Group, Nama: it.Nama, NISN: it.NISN,
			RegistrasiID: it.RegistrasiID, OldNIPD: it.OldNIPD,
			NIPD: it.NewNIPD, Status: "planned",
		})
	}
	return out, nil
}

func previewStart(job *store.Job) *int {
	var summary map[string]any
	if err := json.Unmarshal([]byte(job.Summary), &summary); err != nil {
		return nil
	}
	v, ok := summary["start_nipd"].(float64)
	if !ok {
		return nil
	}
	n := int(v)
	if n < 0 {
		return nil
	}
	return &n
}

func decodeJSON(r *http.Request, dst any) error {
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	return dec.Decode(dst)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
