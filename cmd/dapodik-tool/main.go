package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"

	"golang.org/x/term"

	"dapodik-tool/internal/app"
	"dapodik-tool/internal/auth"
	"dapodik-tool/internal/config"
	"dapodik-tool/internal/dapodik"
	"dapodik-tool/internal/registry"
	"dapodik-tool/internal/store"
	_ "time/tzdata"
)

const version = "1.3.1"

var usernamePattern = regexp.MustCompile(`^[A-Za-z0-9._-]{3,40}$`)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "serve":
		err = runServe(os.Args[2:])
	case "create-admin":
		err = runCreateAdmin(os.Args[2:])
	case "preview":
		err = runCLI(os.Args[2:], false)
	case "apply":
		err = runCLI(os.Args[2:], true)
	case "prune":
		err = runPrune(os.Args[2:])
	case "backup":
		err = runBackup(os.Args[2:])
	case "list-modules":
		err = runListModules(os.Args[2:])
	case "version", "--version", "-v":
		fmt.Println("dapodik-tool", version)
		return
	case "help", "--help", "-h":
		usage()
		return
	default:
		fmt.Fprintf(os.Stderr, "perintah tidak dikenal: %s\n\n", os.Args[1])
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "ERROR:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, `dapodik-tool %s

Pemakaian:
  dapodik-tool serve [--addr 127.0.0.1:8000]
  dapodik-tool create-admin [--username ...] [--name ...]
  dapodik-tool preview --base-url URL --sekolah-id UUID --npsn NPSN --cookie "..."
  dapodik-tool apply   --base-url URL --sekolah-id UUID --npsn NPSN --cookie "..." --yes
  dapodik-tool prune [--days 90]
  dapodik-tool backup [--out backup/app.db]
  dapodik-tool list-modules
  dapodik-tool version

Environment:
  APP_ADDR, APP_NAME, DATABASE_PATH, SESSION_SECRET, SESSION_COOKIE_SECURE,
  TRUST_PROXY, DEFAULT_BASE_URL, DEFAULT_SEKOLAH_ID, DEFAULT_NPSN, LOG_LEVEL
`, version)
}

func runServe(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	addr := fs.String("addr", "", "alamat listen, contoh 127.0.0.1:8000")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if *addr != "" {
		cfg.Addr = *addr
	}
	application, err := app.New(cfg)
	if err != nil {
		return err
	}
	defer application.Close()

	ensureBootstrapAdmin(application)

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           application.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	application.Log.Info("server berjalan", "addr", cfg.Addr, "env", cfg.Env)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func ensureBootstrapAdmin(a *app.App) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	count, err := a.Store.CountUsers(ctx)
	if err != nil || count > 0 {
		return
	}
	username := strings.TrimSpace(os.Getenv("BOOTSTRAP_ADMIN_USERNAME"))
	password := os.Getenv("BOOTSTRAP_ADMIN_PASSWORD")
	if username == "" || password == "" {
		a.Log.Warn("belum ada akun; jalankan: dapodik-tool create-admin")
		return
	}
	name := strings.TrimSpace(os.Getenv("BOOTSTRAP_ADMIN_NAME"))
	if name == "" {
		name = username
	}
	if err := createAdminWith(ctx, a.Store, username, name, password); err != nil {
		a.Log.Error("gagal membuat admin bootstrap", "error", err)
		return
	}
	a.Log.Info("admin bootstrap dibuat", "username", username)
}

func runCreateAdmin(args []string) error {
	fs := flag.NewFlagSet("create-admin", flag.ContinueOnError)
	username := fs.String("username", "", "username admin")
	name := fs.String("name", "", "nama lengkap")
	password := fs.String("password", "", "password (kosongkan untuk prompt)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	st, err := store.Open(cfg.DatabasePath)
	if err != nil {
		return err
	}
	defer st.Close()

	reader := bufio.NewReader(os.Stdin)
	u := strings.TrimSpace(*username)
	for !usernamePattern.MatchString(u) {
		if u != "" {
			fmt.Println("Username 3-40 karakter: huruf, angka, titik, garis bawah, atau strip.")
		}
		fmt.Print("Username admin: ")
		u, _ = reader.ReadString('\n')
		u = strings.TrimSpace(u)
	}
	n := strings.TrimSpace(*name)
	if n == "" {
		fmt.Print("Nama lengkap: ")
		n, _ = reader.ReadString('\n')
		n = strings.TrimSpace(n)
	}
	if n == "" {
		n = u
	}
	pw := *password
	if pw == "" {
		if term.IsTerminal(int(os.Stdin.Fd())) {
			fmt.Print("Password (minimal 8 karakter): ")
			raw, err := term.ReadPassword(int(os.Stdin.Fd()))
			fmt.Println()
			if err != nil {
				return err
			}
			pw = string(raw)
		} else {
			fmt.Print("Password: ")
			line, _ := reader.ReadString('\n')
			pw = strings.TrimSpace(line)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := createAdminWith(ctx, st, u, n, pw); err != nil {
		return err
	}
	fmt.Println("Admin dibuat:", u)
	return nil
}

func createAdminWith(ctx context.Context, st *store.Store, username, name, password string) error {
	if !usernamePattern.MatchString(strings.TrimSpace(username)) {
		return errors.New("username tidak valid")
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		return err
	}
	_, err = st.CreateUser(ctx, strings.TrimSpace(username), strings.TrimSpace(name), hash, string(auth.RoleAdmin))
	return err
}

func runPrune(args []string) error {
	fs := flag.NewFlagSet("prune", flag.ContinueOnError)
	days := fs.Int("days", 90, "hapus event/item job lebih lama dari N hari")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	st, err := store.Open(cfg.DatabasePath)
	if err != nil {
		return err
	}
	defer st.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	events, items, err := st.Prune(ctx, *days)
	if err != nil {
		return err
	}
	fmt.Printf("Prune selesai: %d event, %d item dihapus.\n", events, items)
	return nil
}

func runBackup(args []string) error {
	fs := flag.NewFlagSet("backup", flag.ContinueOnError)
	out := fs.String("out", "", "path file backup")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	st, err := store.Open(cfg.DatabasePath)
	if err != nil {
		return err
	}
	defer st.Close()
	dest := *out
	if dest == "" {
		dest = filepath.Join("backup", "app-"+time.Now().In(wita()).Format("20060102-150405")+".db")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := st.Backup(ctx, dest); err != nil {
		return err
	}
	fmt.Println("Backup ditulis:", dest)
	return nil
}

func runListModules(args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	st, err := store.Open(cfg.DatabasePath)
	if err != nil {
		return err
	}
	defer st.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	states, err := st.ListModuleStates(ctx)
	if err != nil {
		return err
	}
	for _, m := range registry.All() {
		state := "nonaktif"
		if states[m.Key] {
			state = "aktif"
		}
		fmt.Printf("%-20s %-28s v%-8s %s\n", m.Key, m.Name, m.Version, state)
	}
	return nil
}

func runCLI(args []string, apply bool) error {
	name := "preview"
	if apply {
		name = "apply"
	}
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	baseURL := fs.String("base-url", envOr("DAPODIK_BASE_URL", ""), "base URL Dapodik")
	cookie := fs.String("cookie", envOr("DAPODIK_COOKIE", ""), "cookie Dapodik")
	token := fs.String("token", envOr("DAPODIK_TOKEN", ""), "bearer token opsional")
	sekolahID := fs.String("sekolah-id", envOr("DAPODIK_SEKOLAH_ID", ""), "sekolah_id")
	npsn := fs.String("npsn", envOr("DAPODIK_NPSN", ""), "NPSN")
	limit := fs.Int("limit", 20000, "limit per halaman")
	delay := fs.Float64("delay", 0.3, "jeda antar request (detik)")
	start := fs.Int("start-nipd", 0, "override NIPD awal (0 = otomatis)")
	level := fs.String("level", "", "filter tingkat opsional, mis. X")
	groups := fs.String("groups", "", "daftar rombel dipisah koma (kosong = semua)")
	yes := fs.Bool("yes", false, "apply tanpa konfirmasi")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *cookie == "" || *baseURL == "" || *sekolahID == "" {
		return errors.New("--base-url, --cookie, dan --sekolah-id wajib diisi")
	}
	client := dapodik.New(dapodik.Config{
		BaseURL: strings.TrimRight(*baseURL, "/"),
		Cookie:  *cookie,
		Token:   *token,
	})
	fmt.Println(strings.Repeat("=", 68))
	fmt.Println(" Dapodik NIPD", strings.ToUpper(name))
	fmt.Println(strings.Repeat("=", 68))
	fmt.Println(" Base URL :", *baseURL)
	fmt.Println(" Sekolah  :", *sekolahID)
	fmt.Println(" NPSN     :", *npsn)

	fmt.Println("\n[1/3] Mengambil data PesertaDidik...")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	students, err := client.FetchStudents(ctx, dapodik.FetchOptions{
		SekolahID: *sekolahID, NPSN: *npsn, PDModule: "pdterdaftar",
		Limit: *limit, Delay: time.Duration(*delay * float64(time.Second)), MaxPages: 50,
		Progress: func(p dapodik.FetchProgress) {
			total := ""
			if p.Total != nil {
				total = fmt.Sprintf(", total server=%d", *p.Total)
			}
			fmt.Printf("      page=%d: %d baris, terkumpul %d%s\n", p.Page, p.Rows, p.Collected, total)
		},
	})
	if err != nil {
		return err
	}
	fmt.Println("      Total siswa:", len(students))

	var selected map[string]bool
	if strings.TrimSpace(*groups) != "" {
		selected = map[string]bool{}
		for _, g := range strings.Split(*groups, ",") {
			selected[strings.TrimSpace(g)] = true
		}
	}
	var startPtr *int
	if *start > 0 {
		startPtr = start
	}
	plan, err := dapodik.BuildPlan(students, dapodik.PlanOptions{
		StartNIPD: startPtr, SelectedGroups: selected, Level: *level,
	})
	if err != nil {
		return err
	}
	fmt.Println("\n[2/3] Ringkasan...")
	fmt.Println("      NIPD tertinggi :", plan.HighestNIPD)
	fmt.Println("      NIPD awal      :", plan.StartNIPD)
	fmt.Println("      Sudah ada NIPD :", plan.Filled)
	fmt.Println("      NIPD kosong    :", plan.Empty)

	fmt.Println("\n[3/3] Rombel (urut kelas bawah ke atas):")
	for _, g := range plan.Groups {
		fmt.Printf("      %-10s %4d siswa, %4d kosong\n", g.Label, g.Total, g.Empty)
	}
	if len(plan.Items) == 0 {
		fmt.Println("\nTidak ada NIPD kosong pada cakupan ini.")
		return nil
	}
	fmt.Printf("\nRencana: %d siswa, NIPD %s - %s\n", len(plan.Items), plan.Items[0].NewNIPD, plan.Items[len(plan.Items)-1].NewNIPD)

	if !apply {
		fmt.Println("Preview selesai. Tidak ada perubahan.")
		return nil
	}
	if !*yes {
		fmt.Printf("Ketik 'ISI' untuk mengirim %d PUT ke Dapodik: ", len(plan.Items))
		reader := bufio.NewReader(os.Stdin)
		answer, _ := reader.ReadString('\n')
		if strings.TrimSpace(answer) != "ISI" {
			fmt.Println("Dibatalkan.")
			return nil
		}
	}
	updated, failed := 0, 0
	for i, it := range plan.Items {
		err := client.UpdateNIPD(ctx, it.RegistrasiID, it.NewNIPD)
		if err != nil {
			failed++
			fmt.Printf("  [%d/%d] GAGAL %s -> %s: %v\n", i+1, len(plan.Items), it.Nama, it.NewNIPD, err)
		} else {
			updated++
			fmt.Printf("  [%d/%d] OK    %s -> %s\n", i+1, len(plan.Items), it.Nama, it.NewNIPD)
		}
		if *delay > 0 && i < len(plan.Items)-1 {
			time.Sleep(time.Duration(*delay * float64(time.Second)))
		}
	}
	fmt.Printf("\nSelesai: %d berhasil, %d gagal.\n", updated, failed)
	if failed > 0 {
		return fmt.Errorf("%d update gagal", failed)
	}
	return nil
}

func envOr(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func wita() *time.Location {
	loc, err := time.LoadLocation("Asia/Makassar")
	if err != nil {
		return time.FixedZone("WITA", 8*3600)
	}
	return loc
}
