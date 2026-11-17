package nipd_filler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"dapodik-tool/internal/dapodik"
	"dapodik-tool/internal/registry"
	"dapodik-tool/internal/store"
)

const (
	renumberPreviewJobType = "nipd.renumber.preview"
	renumberApplyJobType   = "nipd.renumber.apply"
)

type renumberPreviewRequest struct {
	ScopeID   string  `json:"scope_id"`
	Limit     int     `json:"limit"`
	Delay     float64 `json:"delay"`
	Threshold int     `json:"threshold"`
	Operator  string  `json:"operator"`
	StartNIPD int     `json:"start_nipd"`
}

type renumberRecalcRequest struct {
	JobID  string   `json:"job_id"`
	Groups []string `json:"groups"`
}

type renumberApplyRequest struct {
	PreviewJobID string   `json:"preview_job_id"`
	Groups       []string `json:"groups"`
	Delay        float64  `json:"delay"`
}

func resolveRenumberScope(s *registry.Services, r *http.Request, scopeID string) (registry.ScopeCredentials, error) {
	if s.Scopes == nil {
		return registry.ScopeCredentials{}, fmt.Errorf("Scope Dapodik belum tersedia.")
	}
	creds, err := s.Scopes.Resolve(s.CurrentUser(r), strings.TrimSpace(scopeID))
	if err != nil {
		return registry.ScopeCredentials{}, err
	}
	u, err := url.Parse(creds.BaseURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return registry.ScopeCredentials{}, fmt.Errorf("Base URL scope tidak valid.")
	}
	if creds.SekolahID == "" {
		return registry.ScopeCredentials{}, fmt.Errorf("sekolah_id scope belum diatur.")
	}
	if creds.Cookie == "" {
		return registry.ScopeCredentials{}, fmt.Errorf("Scope %s belum memiliki cookie.", creds.Name)
	}
	return creds, nil
}

func renumberPreviewHandler(s *registry.Services) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req renumberPreviewRequest
		if err := decodeJSON(r, &req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Body JSON tidak valid."})
			return
		}
		creds, err := resolveRenumberScope(s, r, req.ScopeID)
		if err != nil {
			writeJSON(w, scopeStatus(err), map[string]any{"error": err.Error()})
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
		if req.Threshold < 0 {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Batas NIPD tidak boleh negatif."})
			return
		}
		if req.StartNIPD < 0 {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "NIPD awal tidak boleh negatif."})
			return
		}
		operator, err := dapodik.NormalizeNIPDOperator(req.Operator)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}

		public := map[string]string{
			"mode":       "renumber",
			"scope_id":   creds.ID,
			"scope_name": creds.Name,
			"base_url":   creds.BaseURL,
			"sekolah_id": creds.SekolahID,
			"npsn":       creds.NPSN,
			"limit":      strconv.Itoa(req.Limit),
			"delay":      strconv.FormatFloat(req.Delay, 'f', -1, 64),
			"threshold":  strconv.Itoa(req.Threshold),
			"operator":   operator,
			"start_nipd": strconv.Itoa(req.StartNIPD),
			"pd_module":  "pdterdaftar",
		}
		secrets := map[string]string{"cookie": creds.Cookie, "token": creds.Token}
		jobID, err := s.Jobs.Submit(r.Context(), s.CurrentUser(r), renumberPreviewJobType, public, secrets)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
		s.Audit(r, "nipd_renumber_preview_submitted", map[string]any{
			"job_id": jobID, "scope_id": creds.ID, "sekolah_id": creds.SekolahID,
			"threshold": req.Threshold, "start_nipd": req.StartNIPD,
		})
		writeJSON(w, http.StatusAccepted, map[string]any{"job_id": jobID})
	}
}

func renumberApplyHandler(s *registry.Services) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req renumberApplyRequest
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
		preview, ok := loadRenumberPreviewForUser(s, r, req.PreviewJobID, w)
		if !ok {
			return
		}
		var previewParams map[string]string
		if err := json.Unmarshal([]byte(preview.PublicParams), &previewParams); err != nil {
			writeJSON(w, http.StatusConflict, map[string]any{"error": "Parameter preview tidak valid. Muat preview ulang."})
			return
		}
		scopeID := strings.TrimSpace(previewParams["scope_id"])
		if scopeID == "" {
			writeJSON(w, http.StatusConflict, map[string]any{"error": "Preview tidak memiliki scope Dapodik. Muat preview ulang."})
			return
		}
		creds, err := resolveRenumberScope(s, r, scopeID)
		if err != nil {
			writeJSON(w, scopeStatus(err), map[string]any{"error": err.Error()})
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
		groups, err := normalizeRequestedGroups(items, req.Groups)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
		groupsJSON, _ := json.Marshal(groups)
		public := map[string]string{
			"mode":           "renumber",
			"preview_job_id": preview.ID,
			"groups":         string(groupsJSON),
			"delay":          strconv.FormatFloat(req.Delay, 'f', -1, 64),
			"scope_id":       creds.ID,
			"scope_name":     creds.Name,
			"threshold":      previewParams["threshold"],
			"operator":       previewParams["operator"],
			"start_nipd":     previewParams["start_nipd"],
		}
		secrets := map[string]string{"cookie": creds.Cookie, "token": creds.Token}
		jobID, err := s.Jobs.Submit(r.Context(), s.CurrentUser(r), renumberApplyJobType, public, secrets)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
		s.Audit(r, "nipd_renumber_apply_submitted", map[string]any{
			"job_id": jobID, "preview_job_id": preview.ID, "scope_id": creds.ID, "groups": groups,
		})
		writeJSON(w, http.StatusAccepted, map[string]any{"job_id": jobID})
	}
}

func renumberRecalcHandler(s *registry.Services) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req renumberRecalcRequest
		if err := decodeJSON(r, &req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Body JSON tidak valid."})
			return
		}
		preview, ok := loadRenumberPreviewForUser(s, r, req.JobID, w)
		if !ok {
			return
		}
		items, err := s.Store.ListItems(r.Context(), preview.ID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "Gagal memuat data preview."})
			return
		}
		groups, err := normalizeRequestedGroups(items, req.Groups)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
		start, threshold := previewStart(preview), previewThreshold(preview)
		if start == nil || threshold == nil {
			writeJSON(w, http.StatusConflict, map[string]any{"error": "Parameter preview tidak lengkap. Muat preview ulang."})
			return
		}
		data, err := buildRenumberPreviewData(items, groups, *threshold, previewOperator(preview), *start)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, data)
	}
}

func renumberPreviewDataHandler(s *registry.Services) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		preview, ok := loadRenumberPreviewForUser(s, r, r.PathValue("job_id"), w)
		if !ok {
			return
		}
		items, err := s.Store.ListItems(r.Context(), preview.ID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "Gagal memuat data preview."})
			return
		}
		start, threshold := previewStart(preview), previewThreshold(preview)
		if start == nil || threshold == nil {
			writeJSON(w, http.StatusConflict, map[string]any{"error": "Parameter preview tidak lengkap. Muat preview ulang."})
			return
		}
		data, err := buildRenumberPreviewData(items, nil, *threshold, previewOperator(preview), *start)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, data)
	}
}

func loadRenumberPreviewForUser(s *registry.Services, r *http.Request, jobID string, w http.ResponseWriter) (*store.Job, bool) {
	job, err := s.Store.GetJob(r.Context(), strings.TrimSpace(jobID))
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "Preview tidak ditemukan."})
		return nil, false
	}
	if !canAccessJob(s.CurrentUser(r), job) {
		writeJSON(w, http.StatusForbidden, map[string]any{"error": "Preview bukan milik Anda."})
		return nil, false
	}
	if job.JobType != renumberPreviewJobType {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Job tersebut bukan preview renumber NIPD."})
		return nil, false
	}
	if job.Status != "done" {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "Preview belum selesai."})
		return nil, false
	}
	return job, true
}

func normalizeRequestedGroups(items []store.JobItem, requested []string) ([]string, error) {
	if requested == nil {
		return nil, nil
	}
	valid := make(map[string]bool, len(items))
	for _, it := range items {
		valid[it.Group] = true
	}
	groups := make([]string, 0, len(requested))
	seen := make(map[string]bool)
	for _, g := range requested {
		g = strings.TrimSpace(g)
		if g == "" || seen[g] {
			continue
		}
		if !valid[g] {
			return nil, fmt.Errorf("Rombel tidak ada di preview: %s", g)
		}
		seen[g] = true
		groups = append(groups, g)
	}
	return groups, nil
}

func buildRenumberPreviewData(items []store.JobItem, groups []string, threshold int, operator string, start int) (*previewData, error) {
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
	plan, err := dapodik.BuildRenumberPlan(students, dapodik.RenumberOptions{
		Threshold: threshold, Operator: operator, StartNIPD: start, SelectedGroups: selected,
	})
	if err != nil {
		return nil, err
	}
	out := &previewData{
		Summary: previewSummary{
			Mode:          "renumber",
			TotalStudents: plan.TotalStudents,
			Filled:        plan.Filled,
			Empty:         plan.Empty,
			HighestNIPD:   plan.HighestNIPD,
			StartNIPD:     plan.StartNIPD,
			NoRombel:      plan.NoRombel,
			Groups:        len(plan.Groups),
			Items:         len(plan.Items),
			Fingerprint:   plan.Fingerprint,
			Threshold:     plan.Threshold,
			Operator:      plan.Operator,
			Eligible:      plan.Eligible,
			Kept:          plan.Kept,
			Skipped:       plan.SkippedNoReg,
			OldMin:        plan.OldMin,
			OldMax:        plan.OldMax,
			NewMin:        plan.StartNIPD,
			NewMax:        plan.NewMax,
			ConflictCount: len(plan.Conflicts),
			Conflicts:     plan.Conflicts,
		},
	}
	if plan.Eligible == 0 {
		out.Summary.NewMin = 0
	}
	for _, g := range plan.Groups {
		out.Groups = append(out.Groups, apiGroup{
			Label: g.Label, Total: g.Total, Empty: g.Empty, Eligible: g.Eligible,
		})
	}
	for _, it := range plan.Items {
		out.Items = append(out.Items, apiItem{
			Group: it.Group, Nama: it.Nama, NISN: it.NISN,
			RegistrasiID: it.RegistrasiID, OldNIPD: it.OldNIPD,
			NIPD: it.NewNIPD, Status: "preview",
		})
	}
	return out, nil
}

func previewOperator(job *store.Job) string {
	var summary map[string]any
	if err := json.Unmarshal([]byte(job.Summary), &summary); err == nil {
		if v, ok := summary["operator"].(string); ok {
			if op, err := dapodik.NormalizeNIPDOperator(v); err == nil {
				return op
			}
		}
	}
	var params map[string]string
	if err := json.Unmarshal([]byte(job.PublicParams), &params); err == nil {
		if op, err := dapodik.NormalizeNIPDOperator(params["operator"]); err == nil {
			return op
		}
	}
	return dapodik.NIPDOperatorGT
}

func previewThreshold(job *store.Job) *int {
	var summary map[string]any
	if err := json.Unmarshal([]byte(job.Summary), &summary); err == nil {
		if v, ok := summaryInt(summary["threshold"]); ok && v >= 0 {
			return &v
		}
	}
	var params map[string]string
	if err := json.Unmarshal([]byte(job.PublicParams), &params); err == nil {
		if v, err := strconv.Atoi(strings.TrimSpace(params["threshold"])); err == nil && v >= 0 {
			return &v
		}
	}
	return nil
}

func summaryInt(v any) (int, bool) {
	switch n := v.(type) {
	case float64:
		return int(n), true
	case int:
		return n, true
	case json.Number:
		i, err := n.Int64()
		return int(i), err == nil
	case string:
		i, err := strconv.Atoi(strings.TrimSpace(n))
		return i, err == nil
	default:
		return 0, false
	}
}
