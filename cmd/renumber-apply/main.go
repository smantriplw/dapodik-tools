package main

import (
	"context"
	"encoding/csv"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"dapodik-tool/internal/dapodik"
)

type row struct {
	group, nama, nisn, regID, oldNIPD string
	newNIPD                           int
}

func main() {
	baseURL := flag.String("base-url", env("DAPODIK_BASE_URL"), "base URL Dapodik")
	cookie := flag.String("cookie", env("DAPODIK_COOKIE"), "cookie Dapodik")
	token := flag.String("token", env("DAPODIK_TOKEN"), "bearer token")
	sekolahID := flag.String("sekolah-id", env("DAPODIK_SEKOLAH_ID"), "sekolah_id")
	npsn := flag.String("npsn", env("DAPODIK_NPSN"), "NPSN")
	limit := flag.Int("limit", 20000, "limit per halaman")
	threshold := flag.Int("threshold", 17978, "batas NIPD")
	start := flag.Int("start", 17979, "NIPD baru pertama")
	operator := flag.String("operator", ">", "operator batas: >, >=, <, <=")
	delay := flag.Duration("delay", 300*time.Millisecond, "jeda antar PUT")
	rollbackPath := flag.String("rollback", "", "CSV mapping lama→baru (wajib untuk apply)")
	reportPath := flag.String("report", "", "CSV hasil apply (wajib untuk apply)")
	apply := flag.Bool("apply", false, "benar-benar mengirim PUT")
	yes := flag.Bool("yes", false, "konfirmasi tanpa prompt")
	flag.Parse()

	if *cookie == "" || *baseURL == "" || *sekolahID == "" {
		fail("--base-url, --cookie, --sekolah-id wajib diisi")
	}
	if *apply && (!*yes || *rollbackPath == "" || *reportPath == "") {
		fail("apply memerlukan --yes --rollback FILE --report FILE")
	}
	if *threshold < 0 || *start < 0 {
		fail("threshold/start tidak boleh negatif")
	}
	normalizedOperator, err := dapodik.NormalizeNIPDOperator(*operator)
	if err != nil {
		fail(err.Error())
	}
	*operator = normalizedOperator

	client := dapodik.New(dapodik.Config{
		BaseURL: strings.TrimRight(*baseURL, "/"), Cookie: *cookie, Token: *token,
		Timeout: 120 * time.Second, Retries: 2,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	fmt.Println("RENUMBER NIPD", map[bool]string{true: "— APPLY", false: "— DRY-RUN"}[*apply])
	fmt.Println("Base URL :", *baseURL)
	fmt.Println("Aturan   : NIPD", *operator, *threshold, "→", *start)
	fmt.Println()

	students, err := client.FetchStudents(ctx, dapodik.FetchOptions{
		SekolahID: *sekolahID, NPSN: *npsn, PDModule: "pdterdaftar",
		Limit: *limit, MaxPages: 50,
		Progress: func(p dapodik.FetchProgress) {
			total := ""
			if p.Total != nil {
				total = fmt.Sprintf(", total=%d", *p.Total)
			}
			fmt.Printf("  GET page=%d: %d baris, terkumpul %d%s\n", p.Page, p.Rows, p.Collected, total)
		},
	})
	if err != nil {
		fail(err.Error())
	}
	plan, err := dapodik.BuildPlan(students, dapodik.PlanOptions{StartNIPD: start})
	if err != nil {
		fail(err.Error())
	}

	var selected []row
	selectedIDs := map[string]bool{}
	next := *start
	for _, g := range plan.Groups {
		for _, s := range g.Students {
			n, ok := s.NIPD.Int()
			if !ok || !dapodik.MatchesNIPDOperator(n, *threshold, *operator) {
				continue
			}
			regID := strings.TrimSpace(s.RegistrasiID)
			if regID == "" {
				continue
			}
			selected = append(selected, row{
				group: g.Label, nama: strings.TrimSpace(s.Nama),
				nisn: strings.TrimSpace(s.NISN.Value), regID: regID,
				oldNIPD: strconv.Itoa(n), newNIPD: next,
			})
			selectedIDs[regID] = true
			next++
		}
	}
	if len(selected) == 0 {
		fmt.Println("Tidak ada siswa dengan NIPD", *operator, *threshold)
		return
	}

	for _, s := range students {
		n, ok := s.NIPD.Int()
		if !ok {
			continue
		}
		if !selectedIDs[strings.TrimSpace(s.RegistrasiID)] && n >= *start && n < next {
			fail(fmt.Sprintf("konflik: NIPD %d dipakai siswa yang tidak diproses; pilih start lain", n))
		}
	}

	fmt.Println()
	fmt.Printf("Akan memproses %d siswa, NIPD %d - %d\n", len(selected), *start, next-1)
	fmt.Println("Mapping lama → baru disimpan untuk rollback.")
	if !*apply {
		fmt.Println("DRY-RUN: tidak ada PUT. Gunakan --apply --yes --rollback ... --report ... untuk eksekusi.")
		return
	}

	if err := writeRollback(*rollbackPath, selected); err != nil {
		fail("gagal menulis rollback CSV: " + err.Error())
	}
	report, err := os.OpenFile(*reportPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		fail("gagal membuat report CSV: " + err.Error())
	}
	defer report.Close()
	rw := csv.NewWriter(report)
	defer rw.Flush()
	_ = rw.Write([]string{"group", "nama", "nisn", "registrasi_id", "old_nipd", "new_nipd", "status", "message"})

	updated, failed := 0, 0
	for i, it := range selected {
		if i%25 == 0 {
			fmt.Printf("  progress %d/%d\n", i, len(selected))
		}
		status, message := "ok", "Berhasil mengupdate RegistrasiPesertaDidik"
		if err := client.UpdateNIPD(ctx, it.regID, strconv.Itoa(it.newNIPD)); err != nil {
			status, message = "failed", err.Error()
			failed++
			fmt.Printf("  GAGAL %s %s → %d: %v\n", it.group, it.nama, it.newNIPD, err)
		} else {
			updated++
		}
		_ = rw.Write([]string{
			it.group, it.nama, it.nisn, it.regID, it.oldNIPD,
			strconv.Itoa(it.newNIPD), status, message,
		})
		rw.Flush()
		if i < len(selected)-1 && *delay > 0 {
			time.Sleep(*delay)
		}
	}
	fmt.Println()
	fmt.Printf("SELESAI: %d berhasil, %d gagal dari %d siswa.\n", updated, failed, len(selected))
	fmt.Println("Rollback :", *rollbackPath)
	fmt.Println("Report   :", *reportPath)
	if failed > 0 {
		os.Exit(1)
	}
}

func writeRollback(path string, rows []row) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	defer w.Flush()
	_ = w.Write([]string{"group", "nama", "nisn", "registrasi_id", "old_nipd", "new_nipd"})
	for _, r := range rows {
		_ = w.Write([]string{
			r.group, r.nama, r.nisn, r.regID, r.oldNIPD, strconv.Itoa(r.newNIPD),
		})
	}
	return w.Error()
}

func env(key string) string { return strings.TrimSpace(os.Getenv(key)) }

func fail(message string) {
	fmt.Fprintln(os.Stderr, "ERROR:", message)
	os.Exit(2)
}
