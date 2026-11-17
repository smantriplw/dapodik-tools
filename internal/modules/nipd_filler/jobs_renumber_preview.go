package nipd_filler

import (
	"fmt"
	"strings"
	"time"

	"dapodik-tool/internal/dapodik"
	"dapodik-tool/internal/registry"
)

func runRenumberPreview(j *registry.JobContext) (map[string]any, error) {
	baseURL := strings.TrimSpace(j.Params["base_url"])
	sekolahID := strings.TrimSpace(j.Params["sekolah_id"])
	npsn := strings.TrimSpace(j.Params["npsn"])
	pdModule := strings.TrimSpace(j.Params["pd_module"])
	if pdModule == "" {
		pdModule = "pdterdaftar"
	}
	limit := paramInt(j.Params, "limit", 20000)
	delay := time.Duration(paramFloat(j.Params, "delay", 0.3) * float64(time.Second))
	threshold := paramInt(j.Params, "threshold", 0)
	start := paramInt(j.Params, "start_nipd", threshold+1)
	if start < 0 {
		return nil, fmt.Errorf("NIPD awal tidak boleh negatif")
	}
	operator, err := dapodik.NormalizeNIPDOperator(j.Params["operator"])
	if err != nil {
		return nil, err
	}

	j.Log("info", "Mengambil data PesertaDidik untuk renumber NIPD", map[string]any{
		"base_url": baseURL, "sekolah_id": sekolahID, "npsn": npsn, "limit": limit,
		"threshold": threshold, "operator": operator, "start_nipd": start,
	})
	client := dapodik.New(dapodik.Config{
		BaseURL: baseURL, Cookie: j.Secrets["cookie"], Token: j.Secrets["token"],
		Timeout: 120 * time.Second, Retries: 2,
	})
	students, err := client.FetchStudents(j.Ctx, dapodik.FetchOptions{
		SekolahID: sekolahID, NPSN: npsn, PDModule: pdModule,
		Limit: limit, Delay: delay, MaxPages: 50,
		Progress: func(p dapodik.FetchProgress) {
			total := 0
			if p.Total != nil {
				total = *p.Total
			}
			totalText := ""
			if p.Total != nil {
				totalText = fmt.Sprintf(", total server=%d", *p.Total)
			}
			j.Progress(p.Collected, total, fmt.Sprintf("Mengambil halaman %d", p.Page))
			j.Log("info", fmt.Sprintf(
				"GET /rest/PesertaDidik page=%d start=%d: %d baris, terkumpul %d%s",
				p.Page, p.Start, p.Rows, p.Collected, totalText,
			), nil)
		},
	})
	if err != nil {
		return nil, err
	}
	if j.Cancelled() {
		j.Log("warn", "Job dibatalkan setelah pengambilan data", nil)
		return map[string]any{"mode": "renumber_preview"}, nil
	}

	j.Log("info", fmt.Sprintf("Total siswa terkumpul: %d", len(students)), nil)
	plan, err := dapodik.BuildRenumberPlan(students, dapodik.RenumberOptions{
		Threshold: threshold, Operator: operator, StartNIPD: start,
	})
	if err != nil {
		return nil, err
	}
	j.Log("info", fmt.Sprintf(
		"Renumber: %d siswa %s %d, rentang baru %d - %d, konflik %d, tanpa registrasi %d",
		plan.Eligible, plan.Operator, plan.Threshold, plan.StartNIPD, plan.NewMax, len(plan.Conflicts), plan.SkippedNoReg,
	), nil)
	if len(plan.Conflicts) > 0 {
		j.Log("warn", fmt.Sprintf(
			"Ada %d NIPD non-target di rentang baru; apply akan ditolak.", len(plan.Conflicts),
		), map[string]any{"conflicts": plan.Conflicts})
	}

	newByReg := make(map[string]string, len(plan.Items))
	for _, it := range plan.Items {
		newByReg[it.RegistrasiID] = it.NewNIPD
	}
	items := make([]registry.JobItem, 0, len(students))
	for _, s := range students {
		group := strings.TrimSpace(s.Rombel.Value)
		if group == "" {
			group = dapodik.NoRombelLabel
		}
		old := strings.TrimSpace(s.NIPD.Value)
		nipd := old
		status := "filled"
		message := ""
		regID := strings.TrimSpace(s.RegistrasiID)
		if v, ok := newByReg[regID]; ok && regID != "" {
			nipd = v
			status = "preview"
		} else if n, ok := s.NIPD.Int(); ok && dapodik.MatchesNIPDOperator(n, threshold, operator) && regID == "" {
			status = "skipped"
			message = "registrasi_id kosong; tidak bisa diubah"
		}
		items = append(items, registry.JobItem{
			Group: group, Nama: strings.TrimSpace(s.Nama),
			NISN: strings.TrimSpace(s.NISN.Value), RegistrasiID: regID,
			OldNIPD: old, NIPD: nipd, Status: status, Message: message,
		})
	}
	j.AddItems(items)

	j.Log("success", fmt.Sprintf(
		"Preview renumber selesai: %d rombel, %d siswa akan diubah, NIPD %d - %d",
		len(plan.Groups), plan.Eligible, plan.StartNIPD, plan.NewMax,
	), nil)

	out := map[string]any{
		"mode":           "renumber_preview",
		"scope_id":       j.Params["scope_id"],
		"scope_name":     j.Params["scope_name"],
		"threshold":      plan.Threshold,
		"operator":       plan.Operator,
		"total_students": plan.TotalStudents,
		"filled":         plan.Filled,
		"empty":          plan.Empty,
		"highest_nipd":   plan.HighestNIPD,
		"start_nipd":     plan.StartNIPD,
		"no_rombel":      plan.NoRombel,
		"groups":         len(plan.Groups),
		"items":          plan.Eligible,
		"eligible":       plan.Eligible,
		"kept":           plan.Kept,
		"skipped":        plan.SkippedNoReg,
		"old_min":        plan.OldMin,
		"old_max":        plan.OldMax,
		"new_min":        plan.StartNIPD,
		"new_max":        plan.NewMax,
		"conflict_count": len(plan.Conflicts),
		"fingerprint":    plan.Fingerprint,
	}
	if len(plan.Conflicts) > 0 {
		out["conflicts"] = plan.Conflicts
	}
	if plan.Eligible == 0 {
		out["new_min"] = 0
	}
	return out, nil
}
