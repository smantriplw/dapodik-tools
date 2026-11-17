# Arsitektur

Dokumen ini menjelaskan bagaimana aplikasi disusun, bagaimana request dan job mengalir, serta keputusan desain yang diambil.

## 1. Ringkasan

```
Browser
  │ HTTPS
  ▼
Caddy / Nginx
  │ 127.0.0.1:8000
  ▼
Go net/http (satu proses)
  ├─ middleware: recover, log, security headers, auth, RBAC, CSRF
  ├─ template html/template + embed (CSS/JS/logo)
  ├─ modul (compile-time registry): nipd_filler, student_lookup
  ├─ job worker (satu goroutine)
  ├─ SSE stream (poll job_events 400 ms)
  ├─ secret box AES-GCM (kredensial per scope)
  └─ SQLite (WAL)
        └─ data/app.db
```

Prinsip:

- Satu binary, satu proses, satu database.
- Tidak ada Redis/Celery/Postgres/Node.
- Realtime satu arah memakai SSE; perintah memakai POST/fetch.
- Modul dikompilasi, bukan diunggah saat runtime.
- Cookie/token per job hanya di memori. Cookie/token yang dikelola admin disimpan terenkripsi AES-GCM dan tidak pernah dirender kembali.

## 2. Paket Go

| Paket | Tanggung jawab |
|---|---|
| `cmd/dapodik-tool` | Entrypoint, subcommand `serve`, `preview`, `apply`, `create-admin`, `prune`, `backup`, `list-modules`, `version`. |
| `internal/config` | Membaca env dan `.env`, validasi, secret production. |
| `internal/store` | SQLite: buka, migrasi, user, settings, audit, job, event, item, modul, backup, prune. |
| `internal/auth` | Hash password, login, session HMAC, CSRF, rate limit, role. |
| `internal/secret` | Enkripsi/dekripsi AES-256-GCM kredensial tersimpan. |
| `internal/dapodik` | Klien REST Dapodik, parser respons, tipe siswa, algoritma plan, fingerprint. |
| `internal/registry` | Kontrak modul, global registry, tipe `Services`, `JobSpec`, `JobContext`. |
| `internal/jobs` | Antrean, worker, eksekusi job, event, cancel, recovery restart. |
| `internal/app` | App, route core, middleware, renderer, static, halaman job/admin. |
| `internal/web` | Template core, CSS, JS, logo (embed). |
| `internal/modules/nipd_filler` | Modul preview/apply isi NIPD dan renumber NIPD. |
| `internal/modules/student_lookup` | Modul pencarian NISN + cache. |
| `internal/modules/_template` | Skeleton modul (diabaikan generator). |
| `internal/modules/gen` | Generator `internal/app/modules_gen.go`. |

Dependency arah:

```
cmd → app → registry ← modules
             ↑
            jobs
             ↑
          store/auth
```

`registry` tidak mengimpor `jobs` atau `app`; `jobs` mengimpor `registry`; modul mengimpor `registry`; `app` merangkai semuanya. Ini mencegah import cycle.

## 3. Alur request HTTP

1. `recoverer` menangkap panic.
2. `requestLogger` mencatat method/path/status/durasi/IP.
3. `securityHeaders` menambahkan CSP, XFO, nosniff, referrer, HSTS (jika HTTPS).
4. `ServeMux` memilih route (Go 1.22 pattern, mis. `GET /jobs/{id}`).
5. Route terautentikasi:
   - baca cookie `smanti_session`;
   - verifikasi HMAC + expiry;
   - muat user dari DB dan cek `is_active`;
   - cek role;
   - untuk POST/PUT/PATCH/DELETE, validasi CSRF dari header `X-CSRF-Token` atau field `_csrf`;
   - simpan user/session di context.
6. Handler merender template atau JSON.
7. Halaman/JSON sensitif memakai `Cache-Control: no-store`.

Respons error:

- HTML tanpa login → redirect `/login?next=...`.
- JSON/API tanpa login → `401 {"error":"silakan login"}`.
- Role salah → `403`.
- CSRF salah → `403`.
- Job bukan milik user dan bukan admin → `403`.

## 4. Session dan autentikasi

- Cookie: `smanti_session`.
- Nilai: `base64url(payload).base64url(HMAC-SHA256(payload))`.
- Payload: `v1|user_id|expiry_unix|csrf_token`.
- Umur: 12 jam.
- Cookie: `HttpOnly`, `SameSite=Lax`, `Secure` mengikuti config.
- Token CSRF juga dirender di `<meta name="csrf-token">` dan hidden input form.
- Login membuat session baru (mencegah session fixation).
- Password: bcrypt cost 12, minimal 8 karakter.
- Rate limit: 5 kegagalan / 15 menit per `IP|username`.
- Tidak ada session store server-side; logout menghapus cookie, tetapi session stateless tetap valid sampai expiry jika cookie disalin. Ganti `SESSION_SECRET` untuk membatalkan semua sesi.

## 5. Job engine

- Satu worker goroutine, antrean channel kapasitas 64.
- Status: `queued → running → done|failed|cancelled`.
- `Submit`:
  - validasi spec dan role;
  - tolak parameter publik yang mengandung `cookie`, `token`, atau `password`;
  - buat job di DB;
  - simpan secret di map memori;
  - masukkan ke antrean.
- Worker:
  - cek cancel;
  - set `running`;
  - membangun `JobContext` (`Store`, `Params`, `Secrets`, `Log`, `Progress`, `AddItem(s)`, `Cancelled`);
  - menjalankan handler;
  - menulis summary/error/status.
- Cancel:
  - set flag di DB dan atomic bool;
  - jika queued, langsung `cancelled`;
  - jika running, handler berhenti sebelum operasi tulis berikutnya.
- Restart:
  - saat startup, semua job `queued`/`running` diubah menjadi `failed` karena secret hanya di memori.
- Event:
  - level `info`, `warn`, `error`, `success`, `progress`;
  - `data` JSON;
  - disimpan di `job_events`.
- Item:
  - detail hasil per siswa di `job_items`;
  - status item: `ok`, `failed`, `skipped`, `preview`, `filled`.
- Counter dashboard job dihitung dengan SQL agregat, bukan dari 200 item yang ditampilkan.

## 6. SSE (realtime)

- Endpoint: `GET /jobs/{id}/events`.
- Handler mengirim event dari `job_events` dengan `id: <seq>`.
- `Last-Event-ID` atau query `after=<seq>` dipakai untuk replay.
- Poll DB setiap 400 ms; heartbeat `: keep-alive` setiap 15 detik.
- Jika job selesai dan tidak ada event baru, kirim `event: end`.
- Header `X-Accel-Buffering: no`.
- Fallback JS: polling `/jobs/{id}/snapshot?after=<seq>` jika EventSource gagal.
- Halaman job memakai `data-after-seq` agar event yang sudah dirender tidak terduplikasi.
- Halaman job yang sudah selesai tidak reload berulang.

## 7. Renderer dan aset

- `embed` menanam template, CSS, JS, dan logo ke binary.
- Core: `internal/web/templates`, `internal/web/static`.
- Modul: `templates/` dan `static/` masing-masing.
- Template modul memakai layout `base.html`, mendefinisikan `content` dan opsional `scripts`.
- Nama template modul: `<module_key>/<file>.html`.
- Renderer mem-parse base + partials + halaman per nama, lalu cache.
- Static core: `/static/...`.
- Static modul: `/static/modules/<key>/...`.
- Font: Google Fonts (Poppins/Sora) dengan fallback sistem.
- CSP mengizinkan stylesheet Google Fonts dan font gstatic; script hanya self.

## 8. Database

SQLite, WAL, `busy_timeout=5000`, `foreign_keys=ON`, pool maksimal 4 koneksi.

### Tabel

`users`
- `id`, `username` (unik, case-insensitive), `name`, `password_hash`, `role` (`admin`/`guru`; label Operator), `is_active`, `created_at`, `last_login_at`.

`settings`
- `key` (PK), `value`.

`credentials`
- `key` (PK), `ciphertext` (AES-GCM base64), `updated_at`.
- Tabel legacy untuk konfigurasi tunggal sebelum 1.2.0. Dimigrasikan otomatis ke scope `Dapodik Utama`; tidak dipakai modul baru.

`dapodik_scopes`
- `id` (PK, 24 hex), `name` (unik case-insensitive), `base_url`, `sekolah_id`, `npsn`, `created_at`, `updated_at`.

`dapodik_scope_credentials`
- `scope_id`, `kind` (`cookie`/`token`), `ciphertext`, `updated_at`; PK `(scope_id, kind)`; FK cascade ke scope.

`user_dapodik_scopes`
- `user_id`, `scope_id`; PK `(user_id, scope_id)`; FK cascade ke user/scope. Whitelist operator.

`audit_log`
- `id`, `ts`, `user_id`, `action`, `detail` JSON, `ip`.

`jobs`
- `id` (20 hex), `module_key`, `job_type`, `status`, `public_params` JSON, `summary` JSON, `error`, `created_by`, `created_at`, `started_at`, `finished_at`, `cancel_requested`.

`job_events`
- `seq` (PK autoincrement), `job_id`, `ts`, `level`, `message`, `data` JSON.
- Index `(job_id, seq)`.

`job_items`
- `id`, `job_id`, `group_label`, `nama`, `nisn`, `registrasi_id`, `old_nipd`, `nipd`, `status`, `message`.
- Index `(job_id)`.

`modules`
- `key` (PK), `enabled`, `updated_at`.

`module_migrations`
- `module_key`, `version`, `applied_at`, PK `(module_key, version)`.

### Catatan data

- Timestamp UTC RFC3339Nano; tampilan WITA.
- File DB/WAL/SHM dibuat `0600` bila memungkinkan; direktori data `0700`.
- Backup memakai `VACUUM INTO` dan `0600`.
- PII: nama, NISN, registrasi_id, NIPD, NIPD lama, log per siswa.
- Retensi: `prune` menghapus event/item lama; baris job tetap.
- Kredensial scope adalah ciphertext; key berasal dari `CREDENTIALS_KEY` atau turunan `SESSION_SECRET`. Backup tanpa key tidak dapat didekripsi.
- Migration startup: jika belum ada scope dan konfigurasi tunggal lama ada, buat scope `Dapodik Utama`, salin ciphertext, dan assign semua operator lama.

## 9. Algoritma NIPD

### Fetch

`internal/dapodik.Client.FetchStudents`:

- Endpoint: `/rest/PesertaDidik`.
- Query: `_dc`, `sekolah_id`, `pd_module`, `limit`, `ascending=nama`, `page`, `start`, `npsn`.
- Paginasi sampai:
  - baris kosong;
  - tidak ada baris baru (dedupe berdasarkan `peserta_didik_id`, fallback `registrasi_id|nisn`);
  - total server tercapai;
  - batas 50 halaman.
- Respons sukses JSON; error PHP-style single-quote dikenali (`Session telah habis`, `Obyek tidak ditemukan`, dll.).
- Field fleksibel `FlexString` menerima null/string/number.

### Plan

`internal/dapodik.BuildPlan`:

1. Hitung NIPD numerik tertinggi dari seluruh siswa.
2. Hitung jumlah siswa dengan NIPD (termasuk non-numerik) dan kosong.
3. Tentukan NIPD awal:
   - override jika diberikan;
   - jika tidak, `highest + 1`;
   - error jika tidak ada NIPD numerik dan tidak ada override.
4. Kelompokkan siswa per `rombel`; siswa tanpa rombel masuk `(Tanpa Rombel)`.
5. Urutkan grup:
   - tingkat: X (10) → XI (11) → XII (12);
   - suffix natural: `X A`, `X B`, `X K`, `XI A1`, dst;
   - `(Tanpa Rombel)` terakhir.
6. Di dalam grup, siswa kosong diurutkan berdasarkan nama lalu NISN.
7. Nomor dialokasikan global:
   - lewati nomor yang sudah dipakai;
   - grup yang tidak dipilih tidak memakai nomor;
   - jika update gagal, nomor tetap dianggap terpakai (tidak dipakai ulang).
8. Item hanya dibuat untuk siswa kosong yang punya `registrasi_id`.

### Renumber plan

`internal/dapodik.BuildRenumberPlan`:

1. Validasi `threshold`/`start_nipd` tidak negatif dan operator termasuk `>`, `>=`, `<`, `<=`.
2. Pakai grouping/urutuan yang sama dengan `BuildPlan` (X → XII, natural, `(Tanpa Rombel)` terakhir).
3. Pilih hanya NIPD numerik yang memenuhi operator terhadap `threshold` pada grup terpilih.
4. Lewati siswa tanpa `registrasi_id` (`skipped`).
5. Alokasikan NIPD baru berurutan mulai `start_nipd`; `start_nipd` tidak harus di atas threshold.
6. Deteksi konflik: NIPD non-target (tidak ikut diubah) yang berada di rentang baru. Apply ditolak jika ada konflik.
7. `group.eligible` menghitung seluruh siswa eligible di grup; `summary.eligible` menghitung item pada grup terpilih.
8. Fingerprint sama dengan mode isi; apply memverifikasi ulang sebelum PUT.
9. Rollback CSV dibuat dari item apply yang berhasil (`nipd_lama → nipd_baru`).

### Fingerprint

`Fingerprint(students)` = SHA-256 dari daftar terurut:

```
registrasi_id \x1f nipd \x1f rombel
```

Preview menyimpan fingerprint. Apply:

1. fetch ulang;
2. hitung fingerprint baru;
3. jika berbeda → gagal tanpa menulis;
4. jika sama → build plan dari data baru dan grup terpilih;
5. PUT satu per satu.

### Apply

- Endpoint: `PUT /rest/RegistrasiPesertaDidik/{registrasi_id}?_dc=...`.
- Body: `{"nipd": "...", "registrasi_id": "..."}`.
- Sukses JSON `success:true` atau PHP-style `success:true`.
- Gagal per siswa dicatat; job lanjut.
- Delay antar request mengikuti parameter.
- Cancel dicek sebelum PUT berikutnya.

## 10. Kredensial Dapodik & Scope

- Admin membuat scope (`dapodik_scopes`), mengisi cookie/token terenkripsi (`dapodik_scope_credentials`), dan meng-assign scope ke operator (`user_dapodik_scopes`).
- Admin otomatis bisa memakai semua scope; operator hanya scope yang di-assign.
- `App.ResolveScope(user, scopeID)`:
  1. memuat daftar scope yang boleh diakses user;
  2. memilih scope berdasarkan `scope_id` (atau satu-satunya scope);
  3. mendekripsi cookie/token ke memori;
  4. mengembalikan base URL/sekolah/NPSN + kredensial.
- Handler modul menaruh hasil resolusi di `JobContext.Secrets`; tidak masuk `public_params`/DB.
- Preview menyimpan `scope_id`/`scope_name` dan target efektif di public params.
- Apply me-resolve ulang scope (akses bisa dicabut; cookie bisa dirotasi) dan membandingkan target scope dengan preview. Jika berubah → 409.
- Jika key enkripsi berubah, ciphertext lama gagal didekripsi dan admin harus menyimpan ulang.
- Satu cookie/token per scope dipakai bersama operator yang di-assign; worker tetap serial sehingga tidak ada apply paralel.

## 11. Modul

- `registry.Register` dipanggil dari `init()` paket modul.
- `internal/modules/gen` memindai folder modul dan menulis blank import ke `internal/app/modules_gen.go`.
- App mendaftarkan **semua** modul saat startup, lalu menyimpan state enabled di memori.
- Modul mendaftarkan:
  - route (`Services.Routes.Handle`);
  - job spec (`Services.Jobs.Register`);
  - setting (`Services.Settings.Register`);
  - schema (`Store.EnsureSchema`);
  - template/static lewat `ModuleSpec`.
- Modul dapat membaca `Services.Scopes` (`List`, `Resolve`) untuk daftar scope user dan kredensial scope.
- Menu sidebar dan kartu dashboard dibangun dari registry, role, dan state enabled.
- Toggle modul langsung berlaku: route modul digate, job submission dicek, menu/kartu difilter memakai state DB yang di-cache di memori.
- Job yang sudah berjalan saat modul dinonaktifkan tetap diselesaikan.
- Detail ada di [ADDING_MODULES.md](ADDING_MODULES.md).

## 12. Keamanan (ringkas)

- Password bcrypt, session HMAC, CSRF, RBAC, rate limit login.
- Kredensial scope terenkripsi at-rest dan didekripsi hanya ke memori job; tidak ditampilkan.
- Akses halaman scope hanya admin; whitelist scope membatasi operator; perubahan diaudit tanpa nilai rahasia.
- CSP ketat, XFO, nosniff, referrer policy, permissions policy.
- HSTS jika HTTPS.
- `no-store` untuk halaman/JSON/download.
- CSV formula injection dicegah.
- Error Dapodik tidak memuat cookie.
- SSRF: base URL scope ditentukan admin; operator tidak bisa override. Risiko tetap ada pada admin. Mitigasi: firewall egress/allowlist.
- Tidak ada 2FA, enkripsi at-rest, audit UI, atau scheduler backup/prune bawaan.

Detail: [SECURITY.md](SECURITY.md).

## 13. Kinerja dan batasan

- Fetch 1.607 siswa biasanya 1–2 request; limit 20.000 diterima server.
- SQLite WAL cukup untuk puluhan operator dan ribuan event.
- Satu worker mencegah dua apply bersamaan.
- SSE memakai query ringan tiap 400 ms per klien.
- Database dan backup tumbuh mengikuti jumlah job dan item.
- Satu instance dapat memiliki beberapa scope Dapodik (satu scope = satu sekolah/konfigurasi).
- Modul compile-time.
- Tidak ada horizontal scaling; SQLite bukan untuk multi-instance.
- Job secrets hilang saat restart (by design).
- Satu cookie/token per scope dipakai bersama operator yang di-assign; gunakan akun Dapodik khusus dan jangan logout dari sesi tersebut.
- Kredensial scope tidak dapat dipulihkan jika `CREDENTIALS_KEY`/`SESSION_SECRET` hilang.
- Menghapus scope menghapus kredensial dan assignment; riwayat job tetap ada.
