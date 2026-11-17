# Menambah Modul (Compile-Time, Plug and Play)

Aplikasi memakai registry modul saat compile. Modul baru tidak mengubah core, tetapi harus di-generate dan di-build ulang. Tidak ada upload/install modul saat runtime.

Alur singkat:

```
copy _template → edit manifest/handler → go generate ./... → go test ./... → build → restart
```

## 1. Struktur modul

Contoh modul `kehadiran`:

```
internal/modules/kehadiran/
├─ module.go                 # manifest + init + register
├─ handlers.go               # HTTP handler (opsional)
├─ jobs.go                   # job handler (opsional)
├─ templates/
│  └─ index.html             # {{define "content"}}...
└─ static/
   └─ kehadiran.js
```

`module.go` wajib ada. Generator memakai keberadaan file ini untuk mendeteksi modul.

## 2. Mulai dari template

```bash
cp -r internal/modules/_template internal/modules/kehadiran
```

Lalu:

1. Rename `package example` menjadi `package kehadiran`.
2. Ganti `Key` menjadi `kehadiran`.
3. Ganti `Name`, `Description`, `Version`, `Icon`, `Order`.
4. Ganti route `/example/` menjadi `/kehadiran/`.
5. Ganti nama template `example/index.html` menjadi `kehadiran/index.html`.
6. Ganti URL static menjadi `/static/modules/kehadiran/...`.
7. Jalankan:
   ```bash
   go generate ./...
   go test ./...
   go build -o dapodik-tool ./cmd/dapodik-tool
   ```
8. Restart service. Modul muncul di sidebar/dashboard dan `/admin/modules`.

Folder berawalan `_` (`_template`) dan folder `gen` dilewati generator.

## 3. Manifest

```go
var moduleRoles = []auth.Role{auth.RoleAdmin, auth.RoleGuru}

func init() {
    registry.Register(registry.Module{
        Key:         "kehadiran",
        Name:        "Kehadiran",
        Description: "Rekap kehadiran siswa.",
        Version:     "1.0.0",
        Icon:        "clock",
        Order:       30,
        Roles:       moduleRoles,
        Nav: []registry.NavItem{
            {Label: "Kehadiran", Path: "/kehadiran/", Icon: "clock", Order: 10},
        },
        Templates: templatesFS,
        Static:    staticFS,
        Register:  register,
    })
}
```

| Field | Wajib | Keterangan |
|---|---|---|
| `Key` | ya | Unik; dipakai di URL static dan DB. Huruf kecil/underscore. |
| `Name` | ya | Nama tampilan. |
| `Description` | ya | Keterangan di dashboard/admin. |
| `Version` | ya | Versi modul. |
| `Icon` | ya | Nama icon dari `internal/app/render.go` (`clock`, `search`, dll.). |
| `Order` | tidak | Urutan menu/dashboard. |
| `Roles` | ya | Role yang boleh melihat/menjalankan. |
| `Nav` | tidak | Menu sidebar; path harus cocok dengan route. |
| `Templates` | tidak | `//go:embed templates/*.html`. |
| `Static` | tidak | `//go:embed static`. |
| `Register` | ya | Fungsi pendaftaran. |

`registry.Register` panic pada key duplikat atau manifest tidak valid. Ini terjadi saat startup/build test, bukan saat request.

## 4. Register

```go
//go:embed templates/*.html
var templatesFS embed.FS

//go:embed static
var staticFS embed.FS

func register(s *registry.Services) error {
    s.Routes.Handle("GET", "/kehadiran/", moduleRoles, indexHandler(s))
    s.Routes.Handle("POST", "/kehadiran/api/rekap", moduleRoles, rekapHandler(s))

    s.Jobs.Register(registry.JobSpec{
        Key:       "kehadiran.rekap",
        Label:     "Rekap Kehadiran",
        ModuleKey: "kehadiran",
        Roles:     moduleRoles,
        Run:       runRekap,
    })

    s.Settings.Register(registry.SettingField{
        Key: "kehadiran.jam_masuk", Label: "Jam masuk",
        Help: "Format HH:MM", Type: "text", Default: "07:00",
    })

    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()
    return s.Store.EnsureSchema(ctx, s.ModuleKey, 1, `
        CREATE TABLE IF NOT EXISTS kehadiran_harian (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            tanggal TEXT NOT NULL,
            registrasi_id TEXT NOT NULL,
            status TEXT NOT NULL
        );
    `)
}
```

Import yang dibutuhkan contoh di atas: `context`, `time`, `embed`, `net/http`, `auth`, dan `registry`.

`Services`:

| Field | Fungsi |
|---|---|
| `Store` | `*store.Store`: user, job, item, settings, schema. |
| `DB` | `*sql.DB` untuk query modul. |
| `Config` | Config aplikasi. |
| `Jobs` | `Register(JobSpec)` dan `Submit(...)`. |
| `Settings` | `Register(SettingField)`, `Get(key)`. |
| `Routes` | `Handle(method, pattern, roles, handler)`. |
| `Render` | Render template modul. |
| `Audit` | Catat audit (jangan masukkan secret). |
| `CurrentUser` | User aktif dari request. |
| `Scopes` | Provider scope Dapodik: `List(user)` dan `Resolve(user, scopeID)`; kredensial didekripsi untuk job. |

## 5. Route

Gunakan pola `net/http` Go 1.22:

```go
s.Routes.Handle("GET", "/kehadiran/", moduleRoles, indexHandler(s))
s.Routes.Handle("GET", "/kehadiran/{id}", moduleRoles, detailHandler(s))
s.Routes.Handle("POST", "/kehadiran/api/simpan", moduleRoles, saveHandler(s))
```

- Route otomatis melewati auth, role, dan CSRF.
- `r.PathValue("id")` untuk path parameter.
- Handler menerima `w,r`; user dari `s.CurrentUser(r)`.
- Jangan mendaftarkan route yang bertabrakan dengan core/modul lain; `ServeMux` akan panic saat startup.
- Method harus uppercase (`GET`, `POST`, dst).
- Untuk API JSON, set `Content-Type: application/json` dan `Cache-Control: no-store`.
- CSRF untuk fetch: header `X-CSRF-Token`; form: `_csrf`.

## 6. Template

File template modul:

```html
{{define "content"}}
<div class="pagehead">
  <div>
    <p class="eyebrow">Kehadiran</p>
    <h1>Rekap Kehadiran</h1>
    <p class="subtitle">...</p>
  </div>
</div>
<section class="card">
  ...
</section>
{{end}}

{{define "scripts"}}
<script src="/static/modules/kehadiran/kehadiran.js"></script>
{{end}}
```

- Wajib mendefinisikan `content`.
- `scripts` opsional.
- Layout `base.html` sudah menyediakan header, sidebar, flash, CSS, dan `app.js`.
- Render:
  ```go
  s.Render(w, r, "kehadiran/index.html", map[string]any{
      "Title":  "Kehadiran",
      "Active": "/kehadiran/",
      "Data":   data,
  })
  ```
- Data dasar `.Base` (user, nav, CSRF, flash) ditambahkan otomatis.
- Gunakan `html/template` escaping; jangan membangun HTML manual dari input.
- Jangan memakai inline `style`/`script` karena CSP.
- Icon tersedia lewat `{{icon "clock"}}`.
- Komponen CSS yang tersedia: `card`, `pagehead`, `eyebrow`, `btn`, `table`, `badge`, `flash`, `progress`, `console`, `group-list`, `modal`, `stats-grid`, dll. Lihat `internal/web/static/css/app.css`.

## 7. Static

- Folder `static/` modul di-embed dan tersedia di `/static/modules/<key>/...`.
- Gunakan `//go:embed static` (bukan `static/*`) agar subfolder ikut.
- File JS modul memakai helper global `window.SMANTI`:
  - `S.api(url, {method, json})` menangani CSRF/JSON/error;
  - `S.streamJob(jobId, handlers, after)`;
  - `S.appendLog`, `S.setProgress`, `S.statusClass`, `S.statusLabel`;
  - `S.$`, `S.$$`.
- Jangan menyimpan cookie/token di `localStorage`/`sessionStorage`.

## 8. Settings

```go
s.Settings.Register(registry.SettingField{
    Key:     "kehadiran.mode",
    Label:   "Mode rekap",
    Help:    "Pilih salah satu",
    Type:    "select",
    Default: "harian",
    Options: []string{"harian", "mingguan"},
})
nilai := s.Settings.Get("kehadiran.mode")
```

Tipe yang didukung UI saat ini: `text`, `int`, `select`. Field muncul otomatis di `/admin/settings`. Jangan simpan secret di settings.

## 9. Schema dan migrasi

```go
ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
defer cancel()
return s.Store.EnsureSchema(ctx, s.ModuleKey, 1, `
CREATE TABLE IF NOT EXISTS kehadiran_harian (...);
CREATE INDEX IF NOT EXISTS idx_kehadiran_tanggal ON kehadiran_harian(tanggal);
`)
```

- Versi disimpan di `module_migrations` per modul.
- Naikkan versi (mis. 2) untuk migrasi baru.
- Jangan mengubah tabel core dari modul.
- `EnsureSchema` idempotent untuk versi yang sama.
- Pisahkan statement dengan `;`; jangan menaruh `;` di dalam string DDL.
- Backup sebelum migrasi production.

## 10. Job

Job berjalan di satu worker, punya event, progress, cancel, riwayat, dan SSE.

```go
func runRekap(j *registry.JobContext) (map[string]any, error) {
    total := 100
    for i := 0; i < total; i++ {
        if j.Cancelled() {
            return nil, errors.New("dibatalkan")
        }
        // kerja...
        j.Progress(i+1, total, "Memproses")
        j.Log("info", "Baris diproses", map[string]any{"i": i})
    }
    j.AddItem(registry.JobItem{
        Group: "X A", Nama: "NAMA", NISN: "NISN",
        NIPD: "1", Status: "ok", Message: "selesai",
    })
    return map[string]any{"updated": total}, nil
}
```

`JobContext`:

| Field/Method | Keterangan |
|---|---|
| `Ctx` | context untuk HTTP request/IO. |
| `JobID`, `ModuleKey`, `User` | identitas job/user. |
| `Store` | akses DB, termasuk job lain. |
| `Params` | parameter publik; aman disimpan. |
| `Secrets` | cookie/token; hanya memori. |
| `Log(level, msg, data)` | event log (`info`, `warn`, `error`, `success`). |
| `Progress(current,total,label)` | event progress. |
| `AddItem(item)` | simpan satu hasil. |
| `AddItems([]item)` | batch hasil. |
| `Cancelled()` | cek permintaan cancel. |

Aturan:

- Jangan menaruh cookie/token di `Params`, log, audit, summary, atau item.
- `Params` divalidasi: key yang mengandung `cookie`/`token`/`password` ditolak.
- Submit:
  ```go
  jobID, err := s.Jobs.Submit(r.Context(), user, "kehadiran.rekap",
      map[string]string{"tanggal": "2026-01-01"},
      map[string]string{"cookie": cookie, "token": token})
  ```
- Job queued menyimpan secret di memori; restart menandai job gagal.
- Return map summary; disimpan sebagai JSON.
- Error handler → job `failed`; summary tidak disimpan.
- Cancel dicek manual di loop; tidak memaksa henti di tengah operasi.
- Halaman job generik otomatis menampilkan status, log, item, CSV/JSON.

## 11. Navigasi dan role

- Menu sidebar dibangun dari `Module.Nav` dan difilter `Module.Roles`.
- Dashboard menampilkan kartu modul otomatis.
- Path `Nav` harus sama dengan route yang didaftarkan.
- Gunakan `Order` untuk urutan.
- Module nonaktif: nav/kartu dashboard disembunyikan, route mengembalikan 404, dan submission job baru ditolak; job yang sudah berjalan tetap diselesaikan.

## 12. Generator

```bash
go generate ./...
```

Generator `internal/modules/gen`:

- mencari root dari `go.mod`;
- memindai `internal/modules/*/module.go`;
- melewati folder `_*` dan `gen`;
- menulis `internal/app/modules_gen.go` berisi blank import;
- memformat dengan `go/format`.

Menghapus modul: hapus folder, `go generate ./...`, build ulang. Baris di tabel `modules` boleh tertinggal; tidak berbahaya.

## 13. Enable/disable

- Admin `/admin/modules` menoggle status di tabel `modules`.
- Perubahan berlaku langsung. Route, menu, dashboard, dan job submission dicek terhadap state modul di memori yang diperbarui saat toggle.
- Semua modul tetap `Register()` saat startup; status nonaktif tidak membatalkan registrasi, tetapi gate runtime menolak akses. Ini memungkinkan toggle tanpa restart.
- Menambah/menghapus modul dari binary tetap memerlukan `go generate` + rebuild + restart.
- Modul tetap ada di binary.

## 14. Test modul

Minimal:

```go
func TestModuleRegistered(t *testing.T) {
    if _, ok := registry.Find("kehadiran"); !ok { t.Fatal("modul tidak terdaftar") }
}
```

Test handler dengan `httptest` dan store sementara. Test job dengan `registry.JobContext` langsung atau lewat runner. Test schema dengan `EnsureSchema`.

Jalankan:

```bash
go test ./internal/modules/kehadiran -v
```

## 15. Checklist keamanan modul

- [ ] Route memakai role yang benar.
- [ ] Validasi semua input (panjang, format, angka).
- [ ] Jangan menyimpan cookie/token.
- [ ] Jangan log secret/PII berlebihan.
- [ ] Gunakan query parameterized.
- [ ] Jangan memakai `template.HTML` untuk input pengguna.
- [ ] Jangan inline script/style (CSP).
- [ ] CSRF otomatis, jangan menonaktifkan middleware.
- [ ] Batasi ukuran body.
- [ ] Tangani error tanpa membocorkan detail sensitif.
- [ ] Tambah audit untuk aksi penting.
- [ ] Test role dan job.

## 16. Pitfall umum

| Masalah | Penyebab/solusi |
|---|---|
| Modul tidak muncul | `go generate` belum, build lama, atau modul nonaktif (toggle langsung) |
| Route panic | Pattern duplikat/bertabrakan |
| Template 500 | Tidak ada `content`, nama file salah, embed pattern salah |
| Static 404 | Path `/static/modules/<key>/...`, embed `static`, rebuild |
| Import cycle | Modul jangan impor `internal/app`/`internal/jobs`; pakai `registry` |
| Job tidak terdaftar | `Register` tidak dipanggil atau module nonaktif |
| Secret masuk DB | Jangan letakkan di `Params`/log/summary |
| CSS hilang | CSP/class salah; jangan inline style |
| Settings tidak tampil | `Register` tidak dipanggil atau key duplikat |

## 17. Batasan

Modul adalah compile-time. Jika suatu saat butuh install/uninstall tanpa rebuild, arsitektur harus diubah (microservice/plugin/Python). Untuk kebutuhan sekarang, rebuild adalah trade-off yang disengaja agar deployment tetap satu binary.
