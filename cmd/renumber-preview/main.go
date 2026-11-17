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

type selectedRow struct {
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
	report := flag.String("report", "", "path CSV mapping (opsional)")
	flag.Parse()

	if *cookie == "" || *baseURL == "" || *sekolahID == "" {
		fmt.Fprintln(os.Stderr, "ERROR: --base-url, --cookie, --sekolah-id wajib diisi")
		os.Exit(2)
	}
	normalizedOperator, err := dapodik.NormalizeNIPDOperator(*operator)
	if err != nil {
		fmt.Fprintln(os.Stderr, "ERROR:", err)
		os.Exit(2)
	}
	*operator = normalizedOperator

	client := dapodik.New(dapodik.Config{
		BaseURL: strings.TrimRight(*baseURL, "/"),
		Cookie:  *cookie,
		Token:   *token,
		Timeout: 120 * time.Second,
		Retries: 1,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	fmt.Println("DRY-RUN RENUMBER NIPD — tidak ada data yang ditulis")
	fmt.Println("Base URL :", *baseURL)
	fmt.Println("Sekolah  :", *sekolahID)
	fmt.Println("Aturan   : NIPD numerik", *operator, *threshold, "→ mulai", *start)
	fmt.Println()

	students, err := client.FetchStudents(ctx, dapodik.FetchOptions{
		SekolahID: *sekolahID,
		NPSN:      *npsn,
		PDModule:  "pdterdaftar",
		Limit:     *limit,
		MaxPages:  50,
		Progress: func(p dapodik.FetchProgress) {
			total := ""
			if p.Total != nil {
				total = fmt.Sprintf(", total server=%d", *p.Total)
			}
			fmt.Printf("  GET page=%d: %d baris, terkumpul %d%s\n", p.Page, p.Rows, p.Collected, total)
		},
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "ERROR:", err)
		os.Exit(1)
	}

	plan, err := dapodik.BuildPlan(students, dapodik.PlanOptions{StartNIPD: start})
	if err != nil {
		fmt.Fprintln(os.Stderr, "ERROR:", err)
		os.Exit(1)
	}

	var selected []selectedRow
	skippedNoRegID := 0
	exactThreshold := 0
	emptyNIPD := 0
	nonNumeric := 0
	for _, s := range students {
		if s.NIPD.IsEmpty() {
			emptyNIPD++
			continue
		}
		n, ok := s.NIPD.Int()
		if !ok {
			nonNumeric++
			continue
		}
		if n == *threshold {
			exactThreshold++
		}
	}

	next := *start
	selectedRegIDs := make(map[string]bool)
	for _, g := range plan.Groups {
		for _, s := range g.Students {
			n, ok := s.NIPD.Int()
			if !ok || !dapodik.MatchesNIPDOperator(n, *threshold, *operator) {
				continue
			}
			regID := strings.TrimSpace(s.RegistrasiID)
			if regID == "" {
				skippedNoRegID++
				continue
			}
			selected = append(selected, selectedRow{
				group: g.Label, nama: strings.TrimSpace(s.Nama), nisn: strings.TrimSpace(s.NISN.Value),
				regID: regID, oldNIPD: strconv.Itoa(n), newNIPD: next,
			})
			selectedRegIDs[regID] = true
			next++
		}
	}

	fmt.Println()
	fmt.Println("=== RINGKASAN ===")
	fmt.Println("Total siswa          :", len(students))
	fmt.Println("NIPD kosong          :", emptyNIPD, "(tidak diproses)")
	fmt.Println("NIPD non-numerik     :", nonNumeric, "(tidak diproses)")
	fmt.Println("NIPD = threshold (info):", exactThreshold)
	fmt.Println("NIPD memenuhi operator:", len(selected), "(akan direnumber)")
	fmt.Println("Dilewati tanpa reg_id:", skippedNoRegID)
	if exactThreshold > 0 {
		fmt.Println()
		fmt.Println("Siswa dengan NIPD tepat", *threshold, "(informasi ambang):")
		for _, g := range plan.Groups {
			for _, s := range g.Students {
				n, ok := s.NIPD.Int()
				if ok && n == *threshold {
					fmt.Printf("  - %-12s %-30s NISN=%s reg=%s\n", g.Label, s.Nama, s.NISN.Value, s.RegistrasiID)
				}
			}
		}
	}
	if len(selected) > 0 {
		minOld, maxOld := 0, 0
		for i, r := range selected {
			n, _ := strconv.Atoi(r.oldNIPD)
			if i == 0 || n < minOld {
				minOld = n
			}
			if n > maxOld {
				maxOld = n
			}
		}
		fmt.Printf("NIPD lama diproses  : %d - %d\n", minOld, maxOld)
	}
	if len(selected) > 0 {
		fmt.Printf("Rentang NIPD baru    : %d - %d\n", *start, next-1)
	}

	conflicts := make(map[int]int)
	for _, s := range students {
		n, ok := s.NIPD.Int()
		if !ok {
			continue
		}
		if !selectedRegIDs[strings.TrimSpace(s.RegistrasiID)] && n >= *start && n < next {
			conflicts[n]++
		}
	}
	if len(conflicts) > 0 {
		fmt.Println()
		fmt.Println("PERINGATAN: NIPD berikut sudah dipakai siswa yang TIDAK diproses dan akan bentrok:")
		keys := make([]int, 0, len(conflicts))
		for n := range conflicts {
			keys = append(keys, n)
		}
		for i := 0; i < len(keys); i++ {
			for j := i + 1; j < len(keys); j++ {
				if keys[j] < keys[i] {
					keys[i], keys[j] = keys[j], keys[i]
				}
			}
		}
		for _, n := range keys {
			fmt.Printf("  - %d\n", n)
		}
	}

	fmt.Println()
	fmt.Println("=== URUTAN GRUP (X A → XII Z; (Tanpa Rombel) terakhir) ===")
	pos := 0
	for _, g := range plan.Groups {
		var groupRows []selectedRow
		for ; pos < len(selected) && selected[pos].group == g.Label; pos++ {
			groupRows = append(groupRows, selected[pos])
		}
		if len(groupRows) == 0 {
			fmt.Printf("%-12s total=%d, diproses=0\n", g.Label, g.Total)
			continue
		}
		fmt.Printf("%-12s total=%d, diproses=%d, NIPD %d-%d\n",
			g.Label, g.Total, len(groupRows), groupRows[0].newNIPD, groupRows[len(groupRows)-1].newNIPD)
	}
	if len(selected) > 0 {
		fmt.Println()
		fmt.Println("=== CONTOH MAPPING ===")
		show := 5
		if len(selected) < show {
			show = len(selected)
		}
		for i := 0; i < show; i++ {
			r := selected[i]
			fmt.Printf("%-10s %-30s %s → %d\n", r.group, r.nama, r.oldNIPD, r.newNIPD)
		}
		if len(selected) > show {
			fmt.Println("...")
			for i := len(selected) - 5; i < len(selected); i++ {
				if i < show {
					continue
				}
				r := selected[i]
				fmt.Printf("%-10s %-30s %s → %d\n", r.group, r.nama, r.oldNIPD, r.newNIPD)
			}
		}
	}

	if *report != "" && len(selected) > 0 {
		if err := writeReport(*report, selected); err != nil {
			fmt.Fprintln(os.Stderr, "ERROR menulis report:", err)
			os.Exit(1)
		}
		fmt.Println()
		fmt.Println("Mapping CSV:", *report)
	}
	fmt.Println()
	fmt.Println("Selesai dry-run. Tidak ada PUT yang dikirim.")
}

func writeReport(path string, rows []selectedRow) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	defer w.Flush()
	_ = w.Write([]string{"group", "nama", "nisn", "registrasi_id", "old_nipd", "new_nipd"})
	for _, r := range rows {
		_ = w.Write([]string{r.group, r.nama, r.nisn, r.regID, r.oldNIPD, strconv.Itoa(r.newNIPD)})
	}
	return w.Error()
}

func env(key string) string { return strings.TrimSpace(os.Getenv(key)) }
