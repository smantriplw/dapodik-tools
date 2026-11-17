package nipd_filler

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"dapodik-tool/internal/dapodik"
	"dapodik-tool/internal/registry"
)

func runRenumberApply(j *registry.JobContext) (map[string]any, error) {
	previewID := strings.TrimSpace(j.Params["preview_job_id"])
	if previewID == "" {
		return nil, fmt.Errorf("preview_job_id kosong")
	}
	preview, err := j.Store.GetJob(j.Ctx, previewID)
	if err != nil {
		return nil, fmt.Errorf("preview tidak ditemukan")
	}
	if preview.JobType != renumberPreviewJobType {
		return nil, fmt.Errorf("job sumber bukan preview renumber NIPD")
	}
	if preview.Status != "done" {
		return nil, fmt.Errorf("preview belum selesai")
	}

	var previewParams map[string]string
	if err := json.Unmarshal([]byte(preview.PublicParams), &previewParams); err != nil {
		return nil, fmt.Errorf("parameter preview tidak valid")
	}
	var previewSummary map[string]any
	_ = json.Unmarshal([]byte(preview.Summary), &previewSummary)
	expectedFingerprint, _ := previewSummary["fingerprint"].(string)

	threshold, err := strconv.Atoi(strings.TrimSpace(previewParams["threshold"]))
	if err != nil || threshold < 0 {
		return nil, fmt.Errorf("batas NIPD pada preview tidak valid")
	}
	start, err := strconv.Atoi(strings.TrimSpace(previewParams["start_nipd"]))
	if err != nil || start < 0 {
		return nil, fmt.Errorf("NIPD awal pada preview tidak valid")
	}
	operator, err := dapodik.NormalizeNIPDOperator(previewParams["operator"])
	if err != nil {
		return nil, err
	}

	var groups []string
	if err := json.Unmarshal([]byte(j.Params["groups"]), &groups); err != nil || len(groups) == 0 {
		return nil, fmt.Errorf("daftar rombel tidak valid")
	}
	selected := make(map[string]bool, len(groups))
	for _, g := range groups {
		selected[g] = true
	}

	baseURL := strings.TrimSpace(previewParams["base_url"])
	sekolahID := strings.TrimSpace(previewParams["sekolah_id"])
	npsn := strings.TrimSpace(previewParams["npsn"])
	pdModule := strings.TrimSpace(previewParams["pd_module"])
	if pdModule == "" {
		pdModule = "pdterdaftar"
	}
	limit := 20000
	if v, err := strconv.Atoi(previewParams["limit"]); err == nil && v > 0 {
		limit = v
	}
	delaySeconds := paramFloat(j.Params, "delay", 0.3)
	if delaySeconds < 0 || delaySeconds > 10 {
		delaySeconds = 0.3
	}
	delay := time.Duration(delaySeconds * float64(time.Second))

	j.Log("info", "Memverifikasi ulang data sebelum renumber apply", map[string]any{
		"preview_job_id": preview.ID, "groups": groups, "threshold": threshold, "operator": operator, "start_nipd": start,
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
			j.Progress(p.Collected, total, fmt.Sprintf("Verifikasi halaman %d", p.Page))
			j.Log("info", fmt.Sprintf("Verifikasi page=%d: %d baris, terkumpul %d", p.Page, p.Rows, p.Collected), nil)
		},
	})
	if err != nil {
		return nil, err
	}
	freshFingerprint := dapodik.Fingerprint(students)
	if expectedFingerprint != "" && freshFingerprint != expectedFingerprint {
		j.Log("warn", "Data Dapodik berubah sejak preview; renumber dibatalkan tanpa menulis", nil)
		return nil, fmt.Errorf("data berubah sejak preview, muat preview ulang")
	}
	if j.Cancelled() {
		j.Log("warn", "Job dibatalkan sebelum renumber apply", nil)
		return map[string]any{"mode": "renumber_apply"}, nil
	}

	plan, err := dapodik.BuildRenumberPlan(students, dapodik.RenumberOptions{
		Threshold: threshold, Operator: operator, StartNIPD: start, SelectedGroups: selected,
	})
	if err != nil {
		return nil, err
	}
	if len(plan.Conflicts) > 0 {
		j.Log("error", fmt.Sprintf(
			"Renumber ditolak: %d NIPD non-target berada di rentang %d - %d",
			len(plan.Conflicts), start, plan.NewMax,
		), map[string]any{"conflicts": plan.Conflicts})
		return nil, fmt.Errorf("konflik NIPD pada rentang baru: %v", plan.Conflicts)
	}
	total := len(plan.Items)
	if total == 0 {
		j.Log("info", "Tidak ada NIPD lama yang memenuhi syarat renumber", nil)
		return map[string]any{
			"mode": "renumber_apply", "threshold": threshold, "start_nipd": start,
			"eligible": 0, "updated": 0, "failed": 0, "skipped": plan.SkippedNoReg,
			"total": 0, "last_nipd": 0,
		}, nil
	}

	j.Log("info", fmt.Sprintf(
		"Renumber apply %d siswa mulai NIPD %d (rentang %s - %s)",
		total, plan.StartNIPD, plan.Items[0].NewNIPD, plan.Items[total-1].NewNIPD,
	), nil)

	updated, failed := 0, 0
	for i, it := range plan.Items {
		if j.Cancelled() {
			j.Log("warn", fmt.Sprintf("Dibatalkan setelah %d/%d item", i, total), nil)
			break
		}
		j.Progress(i, total, fmt.Sprintf("%s (%d/%d)", it.Nama, i+1, total))
		j.Log("info", fmt.Sprintf(
			"PUT %s: NIPD %s -> %s (%d/%d)", it.Nama, it.OldNIPD, it.NewNIPD, i+1, total,
		), map[string]any{"registrasi_id": it.RegistrasiID})

		status := "ok"
		message := "Berhasil mengupdate RegistrasiPesertaDidik"
		if err := client.UpdateNIPD(j.Ctx, it.RegistrasiID, it.NewNIPD); err != nil {
			status = "failed"
			message = err.Error()
			failed++
			j.Log("error", fmt.Sprintf("Gagal %s: %s", it.Nama, message), nil)
		} else {
			updated++
			j.Log("success", fmt.Sprintf("Berhasil %s -> %s", it.Nama, it.NewNIPD), nil)
		}
		j.AddItem(registry.JobItem{
			Group: it.Group, Nama: it.Nama, NISN: it.NISN,
			RegistrasiID: it.RegistrasiID, OldNIPD: it.OldNIPD,
			NIPD: it.NewNIPD, Status: status, Message: message,
		})
		j.Progress(i+1, total, fmt.Sprintf("%s (%d/%d)", it.Nama, i+1, total))
		if delay > 0 && i < total-1 {
			time.Sleep(delay)
		}
	}

	lastNIPD := plan.Items[total-1].NewNIPD
	j.Log("success", fmt.Sprintf(
		"Renumber apply selesai: %d berhasil, %d gagal, NIPD terakhir %s", updated, failed, lastNIPD,
	), nil)
	return map[string]any{
		"mode": "renumber_apply", "scope_id": j.Params["scope_id"], "scope_name": j.Params["scope_name"],
		"threshold": threshold, "operator": operator, "start_nipd": plan.StartNIPD, "eligible": total,
		"updated": updated, "failed": failed, "skipped": plan.SkippedNoReg,
		"total": total, "old_min": plan.OldMin, "old_max": plan.OldMax,
		"new_min": plan.StartNIPD, "new_max": plan.NewMax, "last_nipd": lastNIPD,
		"rollback_available": updated > 0,
	}, nil
}
