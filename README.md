# SMANTI Tools — Dapodik

Web tool internal SMA Negeri 3 Palu untuk preview dan pengisian NIPD Dapodik, pengecekan NISN, riwayat job, dan unduhan CSV/JSON. Dibangun sebagai **satu binary Go** dengan SQLite dan SSE realtime.

Versi: **1.3.1** · Stack: Go 1.22+ · Lisensi internal sekolah.

## Fitur

- Login akun per operator; role `admin` dan `operator`.
- **Pengisian NIPD**: isi NIPD kosong atau renumber NIPD lama, preview read-only, pilih rombel, hitung ulang rencana, deteksi konflik, apply dengan pengaman fingerprint, progres SSE realtime, cancel, unduhan CSV/JSON + rollback.
- **Cek Siswa**: pencarian hingga 100 NISN, penanda `(Tanpa Rombel)` dan NIPD kosong.
- Riwayat job semua pengguna (admin) atau milik sendiri (operator), log realtime, detail item.
- Admin: **multi-scope Dapodik** (URL, sekolah, NPSN, cookie, token per scope), whitelist scope per operator, pengguna, pengaturan, dan toggle modul dinamis tanpa restart.
- Kredensial Dapodik disimpan terenkripsi AES-GCM per scope; operator memilih scope dan tidak perlu menempel cookie.
- Operator hanya melihat scope yang di-assign admin; admin otomatis melihat semua scope.
- Modul compile-time ala Odoo: menu, route, job, settings, template, static, dan schema dimiliki modul.
- UI mengikuti token SMAN 3 Palu (merah/oranye/kuning, Poppins/Sora, radius 2px).

## Dokumentasi

| Dokumen | Untuk |
|---|---|
| [docs/USER_GUIDE.md](docs/USER_GUIDE.md) | Operator: login, pilih scope, preview/apply/renumber, cek NISN |
| [docs/ADMIN_GUIDE.md](docs/ADMIN_GUIDE.md) | Admin: akun, scope Dapodik, whitelist, settings, modul, audit |
| [docs/DEPLOYMENT.md](docs/DEPLOYMENT.md) | VPS, domain, HTTPS, systemd, Caddy/Nginx, backup |
| [docs/OPERATIONS.md](docs/OPERATIONS.md) | Checklist rutin, restore, rotasi kredensial, insiden |
| [docs/RENUMBER.md](docs/RENUMBER.md) | Renumber NIPD via CLI admin, rollback, peringatan |
| [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) | Paket, alur request, job, SSE, DB, algoritma NIPD |
| [docs/ADDING_MODULES.md](docs/ADDING_MODULES.md) | Menambah modul/menu/job/settings/schema |
| [docs/API_REFERENCE.md](docs/API_REFERENCE.md) | Endpoint internal, payload, SSE, CSV/JSON |
| [docs/openapi.yaml](docs/openapi.yaml) | Spesifikasi OpenAPI 3.0.3 |
| [docs/SECURITY.md](docs/SECURITY.md) | Model ancaman, kontrol, risiko, hardening |
| [docs/TESTING.md](docs/TESTING.md) | Test otomatis, fake Dapodik, QA manual, CI |
| [docs/TROUBLESHOOTING.md](docs/TROUBLESHOOTING.md) | Gejala, penyebab, tindakan |
| [docs/GLOSSARY.md](docs/GLOSSARY.md) | Istilah Dapodik dan aplikasi |
| [docs/CHANGELOG.md](docs/CHANGELOG.md) | Riwayat versi |
| [docs/README.md](docs/README.md) | Indeks dokumentasi |

## Menjalankan lokal

Prasyarat: Go 1.22+.

```bash
go generate ./...
go test ./...
go build -o dapodik-tool ./cmd/dapodik-tool

./dapodik-tool create-admin
./dapodik-tool serve
```

Buka <http://127.0.0.1:8000>. Database default `./data/app.db` (dibuat otomatis, mode `0600`). Admin pertama hanya dibuat lewat CLI atau env bootstrap.

Setelah login sebagai admin, buka **Administrasi → Dapodik (Scopes)** (`/admin/scopes`) untuk membuat scope Dapodik (A/B/C atau nama bebas), mengisi kredensial, dan meng-assign scope ke operator. Panduan mengambil cookie ada di [docs/ADMIN_GUIDE.md](docs/ADMIN_GUIDE.md#3-kredensial-dapodik) dan langsung di halaman scope.

Untuk dev, opsional buat `.env`:

```env
APP_ENV=development
SESSION_SECRET=dev-secret-yang-cukup-panjang
# Opsional; jika kosong memakai SESSION_SECRET
# CREDENTIALS_KEY=dev-credentials-key-yang-cukup-panjang
DEFAULT_BASE_URL=https://dapodik.sekolah.sch.id
DEFAULT_SEKOLAH_ID=UUID_SEKOLAH
DEFAULT_NPSN=40203612
```

Tanpa `SESSION_SECRET`, dev membuat secret acak; semua sesi batal setiap restart.

Atau:

```bash
make build
./dapodik-tool serve
```

Target Makefile: `build` (generate + build), `run`, `test`, `fmt`, `vet`, `generate`, `clean` (hanya menghapus binary; data/backup tidak disentuh).

## CLI

| Perintah | Fungsi |
|---|---|
| `dapodik-tool serve [--addr HOST:PORT]` | Jalankan web |
| `dapodik-tool create-admin` | Buat admin pertama (interaktif) |
| `dapodik-tool preview --base-url ... --sekolah-id ... --cookie ...` | Preview read-only |
| `dapodik-tool apply ... --yes` | Apply dari CLI (hati-hati) |
| `dapodik-tool list-modules` | Daftar modul + status |
| `dapodik-tool backup --out FILE` | Backup SQLite via `VACUUM INTO` |
| `dapodik-tool prune --days 90` | Hapus event/item job lama |
| `dapodik-tool version` | Versi binary |

Flag apply/preview: `--token`, `--npsn`, `--limit`, `--delay`, `--start-nipd`, `--level`, `--groups`, `--yes`.
Env CLI: `DAPODIK_BASE_URL`, `DAPODIK_COOKIE`, `DAPODIK_TOKEN`, `DAPODIK_SEKOLAH_ID`, `DAPODIK_NPSN`.

## Deploy

Ringkas:

```bash
CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o dapodik-tool ./cmd/dapodik-tool
```

Lalu ikuti [docs/DEPLOYMENT.md](docs/DEPLOYMENT.md) dan file di [deploy/](deploy/):

- `deploy/env.example`
- `deploy/dapodik-tool.service`
- `deploy/Caddyfile` (disarankan, SSE unbuffered)
- `deploy/nginx.conf.example`

Satu proses saja; SQLite tidak untuk multi-instance. Jangan buka port aplikasi ke publik; gunakan reverse proxy HTTPS.

## Environment utama

| Variabel | Default | Keterangan |
|---|---|---|
| `APP_ENV` | `development` | Set `production` di server |
| `APP_ADDR` | `127.0.0.1:8000` | Alamat listen |
| `APP_NAME` | `SMANTI Tools` | Nama tampilan |
| `DATABASE_PATH` | `./data/app.db` | Path SQLite |
| `SESSION_SECRET` | acak di dev | Wajib 32+ karakter di production |
| `CREDENTIALS_KEY` | turunan `SESSION_SECRET` | Kunci enkripsi cookie/token Dapodik yang disimpan admin. Disarankan terpisah. |
| `SESSION_COOKIE_SECURE` | `false` | `1` di belakang HTTPS |
| `TRUST_PROXY` | `false` | `1` di belakang Caddy/Nginx |
| `DEFAULT_BASE_URL` | kosong | Prefill Dapodik |
| `DEFAULT_SEKOLAH_ID` | kosong | Prefill sekolah |
| `DEFAULT_NPSN` | kosong | Prefill NPSN |
| `LOG_LEVEL` | `info` | `debug`/`info`/`warn`/`error` |

Detail: [docs/DEPLOYMENT.md](docs/DEPLOYMENT.md).

## Keamanan

- Kredensial admin disimpan terenkripsi per scope dan tidak pernah ditampilkan kembali di UI/log/CSV; operator hanya bisa memakai scope yang di-whitelist.
- Password bcrypt, session HMAC, CSRF, RBAC, rate limit login.
- CSP, XFO, nosniff, referrer policy, HSTS; `no-store` untuk halaman/data sensitif.
- File DB/WAL/SHM `0600`, backup `0600`.
- CSV formula injection dicegah.
- Apply memakai fingerprint untuk menolak preview usang.

Baca [docs/SECURITY.md](docs/SECURITY.md) sebelum production. Risiko yang diketahui termasuk SSRF via `base_url`, tidak ada 2FA, dan session stateless.

## Batasan

- Modul compile-time; menambah modul perlu `go generate` + rebuild.
- Satu deployment bisa memiliki beberapa scope Dapodik (satu scope = satu sekolah/konfigurasi); satu proses; SQLite.
- Satu cookie/token per scope dipakai bersama operator yang di-assign; gunakan akun/sesi khusus per scope dan jangan logout dari sesi tersebut.
- Menghapus scope mencabut akses operator dan kredensialnya; riwayat job tetap ada.
- Kredensial tersimpan tidak dapat dipulihkan jika `CREDENTIALS_KEY`/`SESSION_SECRET` hilang; simpan ulang.
- Toggle modul langsung berlaku tanpa restart; menambah modul baru tetap perlu rebuild binary.
- Tidak ada 2FA, pendaftaran mandiri, reset password via email, UI audit, atau scheduler backup/prune bawaan.
- Apply nyata ke Dapodik tidak dijalankan oleh test otomatis; test memakai fake server.
- Browser harus mendukung EventSource/SSE dan `<dialog>`.

## Test

```bash
go generate ./...
gofmt -l .
go vet ./...
go test ./...
CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o dapodik-tool ./cmd/dapodik-tool
```

Cakupan dan checklist manual: [docs/TESTING.md](docs/TESTING.md).

## Struktur

```
cmd/dapodik-tool      entrypoint + subcommand
internal/app          HTTP app, middleware, render, route core/admin/job
internal/auth         password, session, CSRF, rate limit, role
internal/config       env + .env
internal/dapodik      klien Dapodik, parser, plan NIPD, fingerprint
internal/jobs         worker, event, cancel
internal/registry     kontrak modul
internal/store        SQLite, user, settings, audit, job, migrasi
internal/modules      nipd_filler, student_lookup, _template, gen
internal/web          template + CSS + JS + logo (embed)
deploy                systemd, Caddy, Nginx, env
docs                  dokumentasi
data/app.db           runtime (gitignored)
backup/               backup (gitignored)
```

## Legacy

`dapodik_nipd_filler.py` adalah CLI Python lama yang dipertahankan sebagai referensi. Web app tidak memakainya; logika inti sudah diport ke `internal/dapodik`. File `hasil_nipd.json` adalah artefak lama dan di-ignore dari git karena berisi PII.
