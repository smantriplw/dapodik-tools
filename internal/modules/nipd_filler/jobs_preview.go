package nipd_filler

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"dapodik-tool/internal/dapodik"
	"dapodik-tool/internal/registry"
)

func runPreview(j *registry.JobContext) (map[string]any, error) {
	baseURL := strings.TrimSpace(j.Params["base_url"])
	sekolahID := strings.TrimSpace(j.Params["sekolah_id"])
	npsn := strings.TrimSpace(j.Params["npsn"])
	pdModule := strings.TrimSpace(j.Params["pd_module"])
	if pdModule == "" {
		pdModule = "pdterdaftar"
	}
	limit := paramInt(j.Params, "limit", 20000)
	delay := time.Duration(paramFloat(j.Params, "delay", 0.3) * float64(time.Second))
	start := paramIntPtr(j.Params, "start_nipd")

	j.Log("info", "Mengambil data PesertaDidik dari Dapodik", map[string]any{
		"base_url": baseURL, "sekolah_id": sekolahID, "npsn": npsn, "limit": limit,
	})

	client := dapodik.New(dapodik.Config{
		BaseURL: baseURL,
		Cookie:  j.Secrets["cookie"],
		Token:   j.Secrets["token"],
		Timeout: 120 * time.Second,
		Retries: 2,
	})

	students, err := client.FetchStudents(j.Ctx, dapodik.FetchOptions{
		SekolahID: sekolahID,
		NPSN:      npsn,
		PDModule:  pdModule,
		Limit:     limit,
		Delay:     delay,
		MaxPages:  50,
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
		return map[string]any{"mode": "preview"}, nil
	}

	j.Log("info", fmt.Sprintf("Total siswa terkumpul: %d", len(students)), nil)

	plan, err := dapodik.BuildPlan(students, dapodik.PlanOptions{StartNIPD: start})
	if err != nil {
		return nil, err
	}
	j.Log("info", fmt.Sprintf(
		"NIPD tertinggi %d, NIPD awal %d, kosong %d, tanpa rombel %d",
		plan.HighestNIPD, plan.StartNIPD, plan.Empty, plan.NoRombel,
	), nil)

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
		if s.NIPD.IsEmpty() {
			status = "preview"
			if v, ok := newByReg[strings.TrimSpace(s.RegistrasiID)]; ok {
				nipd = v
			}
		}
		items = append(items, registry.JobItem{
			Group:        group,
			Nama:         strings.TrimSpace(s.Nama),
			NISN:         strings.TrimSpace(s.NISN.Value),
			RegistrasiID: strings.TrimSpace(s.RegistrasiID),
			OldNIPD:      old,
			NIPD:         nipd,
			Status:       status,
		})
	}
	j.AddItems(items)

	j.Log("success", fmt.Sprintf(
		"Preview selesai: %d rombel, %d NIPD kosong, rencana mulai NIPD %d",
		len(plan.Groups), plan.Empty, plan.StartNIPD,
	), nil)

	return map[string]any{
		"mode":           "preview",
		"scope_id":       j.Params["scope_id"],
		"scope_name":     j.Params["scope_name"],
		"total_students": plan.TotalStudents,
		"filled":         plan.Filled,
		"empty":          plan.Empty,
		"highest_nipd":   plan.HighestNIPD,
		"start_nipd":     plan.StartNIPD,
		"no_rombel":      plan.NoRombel,
		"groups":         len(plan.Groups),
		"items":          len(plan.Items),
		"fingerprint":    plan.Fingerprint,
	}, nil
}

func paramInt(params map[string]string, key string, fallback int) int {
	v, err := strconv.Atoi(strings.TrimSpace(params[key]))
	if err != nil {
		return fallback
	}
	return v
}

func paramFloat(params map[string]string, key string, fallback float64) float64 {
	v, err := strconv.ParseFloat(strings.TrimSpace(params[key]), 64)
	if err != nil {
		return fallback
	}
	return v
}

func paramIntPtr(params map[string]string, key string) *int {
	raw := strings.TrimSpace(params[key])
	if raw == "" {
		return nil
	}
	v, err := strconv.Atoi(raw)
	if err != nil || v < 0 {
		return nil
	}
	return &v
}
