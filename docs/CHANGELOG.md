# Changelog

Format: Keep a Changelog. Versi mengikuti rilis binary.

## [1.3.1] — 2026-11-17

### Ditambahkan

- Operator pembanding renumber NIPD: **`>`**, **`>=`**, **`<`**, **`<=`** di web, API, dan CLI.
- `start_nipd` tidak lagi dipaksa lebih besar dari batas; konflik tetap dideteksi sebelum apply.

### Diubah

- Validasi UI/API menggunakan operator; `>=`/`<=` mengikutkan NIPD yang tepat sama dengan batas.
- Dokumentasi, OpenAPI, dan test diperbarui.
- Versi aplikasi dan modul menjadi 1.3.1.

## [1.3.0] — 2026-11-17

### Ditambahkan

- **Mode Renumber NIPD lama** di modul Pengisian NIPD: mengganti NIPD numerik > batas, mulai NIPD baru, urutan X A → XII Z lalu `(Tanpa Rombel)`.
- Preview renumber dengan jumlah eligible/kept/skipped, rentang NIPD lama/baru, deteksi konflik NIPD non-target, dan fingerprint data.
- Job `nipd.renumber.preview` dan `nipd.renumber.apply`, SSE realtime, pembatalan, serta pemilihan rombel.
- Apply renumber menolak data berubah dan konflik tanpa menulis, lalu menyediakan **CSV rollback** (`/jobs/{id}/download.rollback.csv`).
- API: `/nipd-filler/api/renumber/preview`, `/renumber/preview/{job_id}`, `/renumber/recalc`, `/renumber/apply`; OpenAPI diperbarui.
- Dokumentasi `docs/RENUMBER.md`, panduan pengguna, API reference, dan job detail.

### Diubah

- Modul Pengisian NIPD memiliki pemilih mode **Isi NIPD kosong** / **Renumber NIPD lama**.
- Halaman preview menampilkan status konflik dan memblokir apply jika konflik.
- Versi aplikasi dan modul menjadi 1.3.0.

## [1.2.0] — 2026-11-17

### Ditambahkan

- **Multi-scope Dapodik**: admin dapat membuat scope Dapodik A/B/C (nama bebas), masing-masing dengan base URL, sekolah_id, NPSN, cookie, dan token sendiri.
- **Whitelist scope per operator**: form pengguna memiliki pilihan scope; operator hanya bisa memilih scope yang di-assign. Admin otomatis bisa memakai semua scope.
- **Pemilihan scope di modul**: NIPD dan Cek Siswa menampilkan dropdown scope; kredensial dan target mengikuti scope yang dipilih.
- Job menyimpan `scope_id`/`scope_name`; apply memvalidasi ulang akses scope dan mendeteksi perubahan pengaturan scope sejak preview.
- Migrasi otomatis konfigurasi kredensial tunggal lama menjadi scope **Dapodik Utama**, dan operator lama di-assign ke scope tersebut.
- **Toggle modul dinamis**: aktif/nonaktif modul langsung berlaku pada route, job submission, menu, dan dashboard tanpa restart.
- Test store scope, kredensial per scope, whitelist, dan toggle modul dinamis.

### Diubah

- Halaman `/admin/dapodik` diganti menjadi `/admin/scopes` (redirect lama dipertahankan).
- Modul NIPD/Cek Siswa tidak lagi meminta cookie manual; operator memilih scope.
- Halaman pengguna menampilkan scope yang di-assign.
- Modul didaftarkan saat startup meski nonaktif; akses dijaga oleh gate dinamis.
- Versi aplikasi menjadi 1.2.0.

### Keamanan

- Kredensial dienkripsi per scope; operator tidak bisa mengakses scope di luar whitelist.
- Admin dapat mencabut akses scope dengan mengubah assignment user.
- Menghapus scope menghapus kredensial dan assignment terkait; riwayat job tetap ada.

## [1.1.1] — 2026-11-17

### Diubah

- Label role `Guru` diganti menjadi **Operator** di UI (sidebar/user menu, daftar pengguna, form tambah/edit pengguna), dokumentasi, dan OpenAPI.
- Nilai internal role tetap `guru` untuk kompatibilitas database/API lama; tidak ada migrasi data.
- Versi aplikasi menjadi 1.1.1.

## [1.1.0] — 2026-11-17

### Ditambahkan

- Halaman admin **Kredensial Dapodik** (`/admin/dapodik`):
  - base URL, sekolah_id, NPSN, cookie, dan token diatur sekali oleh admin;
  - cookie/token disimpan terenkripsi AES-GCM di tabel `credentials`;
  - status terkonfigurasi dan waktu pembaruan tanpa menampilkan nilai rahasia;
  - tombol **Tes koneksi** read-only ke Dapodik;
  - panduan langkah demi langkah mengambil cookie dari browser.
- `CREDENTIALS_KEY` opsional; jika kosong diturunkan dari `SESSION_SECRET`.
- Fallback kredensial admin di modul NIPD dan Cek Siswa: guru tidak perlu menempel cookie/token.
- Form guru otomatis menyembunyikan field kredensial dan menampilkan target Dapodik saat dikelola admin.
- Test enkripsi/dekripsi, penyimpanan kredensial, dan RBAC halaman kredensial.
- OpenAPI dan dokumentasi diperbarui untuk kredensial admin.

### Diubah

- Base URL/sekolah_id/NPSN dipindahkan dari halaman Pengaturan umum ke halaman Kredensial Dapodik.
- Cookie/token manual per job tetap didukung (override), tetapi UI default memakai kredensial admin bila tersedia.
- Versi aplikasi menjadi 1.1.0.

### Keamanan

- Kredensial admin disimpan terenkripsi; key berasal dari `CREDENTIALS_KEY` atau `SESSION_SECRET`.
- Nilai cookie/token tidak pernah dirender kembali ke HTML, log, audit, atau CSV.
- Mengubah key enkripsi membuat kredensial lama tidak dapat didekripsi; admin harus menyimpan ulang.
- Gunakan akun/sesi Dapodik khusus tool; jangan logout dari sesi yang cookie-nya disimpan.

## [1.0.0] — 2026-11-17

Rilis awal web tool Go.

### Ditambahkan

- Aplikasi Go single-binary + SQLite + SSE.
- Login akun per guru, role `admin` dan `guru`.
- Session HMAC, CSRF, rate limit login, RBAC.
- Modul **Pengisian NIPD**:
  - preview read-only;
  - pengelompokan semua rombel X→XII, natural, `(Tanpa Rombel)` terakhir;
  - pilih rombel dan hitung ulang;
  - pengaman fingerprint preview/apply;
  - apply PUT per siswa, nomor gagal tidak dipakai ulang;
  - progres/log realtime, cancel, unduhan CSV/JSON.
- Modul **Cek Siswa**:
  - pencarian hingga 100 NISN;
  - penanda `(Tanpa Rombel)` dan NIPD kosong;
  - cache memori 5 menit.
- Riwayat job, snapshot, SSE dengan `Last-Event-ID`/`after`, fallback polling.
- Admin pengguna, pengaturan, dan daftar/toggle modul.
- Ganti password sendiri.
- Registry modul compile-time + generator `go generate` + skeleton `_template`.
- Template/CSS mengikuti token SMAN 3 Palu (merah/oranye/kuning, Poppins/Sora).
- CLI `serve`, `create-admin`, `preview`, `apply`, `prune`, `backup`, `list-modules`, `version`.
- Deploy: systemd, Caddy, Nginx, env example.
- Dokumentasi lengkap: pengguna, admin, deployment, operasi, arsitektur, API, keamanan, testing, troubleshooting, glosarium.
- Spesifikasi OpenAPI 3.0.3 di `docs/openapi.yaml`.
- Test unit/integration untuk Dapodik, plan, store, auth, job, app, dan apply fake.

### Keamanan

- Cookie/token Dapodik hanya di memori job; tidak di DB/log/audit/CSV.
- Password bcrypt cost 12.
- CSP, XFO, nosniff, referrer policy, HSTS.
- `no-store` untuk halaman/JSON/download.
- CSV formula injection dicegah.
- File DB/WAL/SHM `0600` bila memungkinkan.
- Job lama saat restart ditandai gagal.
- Penanganan error Dapodik PHP-style (`Session telah habis`, `success:true`).

### Diubah

- Logika NIPD dipindahkan dari CLI Python ke paket Go `internal/dapodik`.
- CLI Python lama dipertahankan sebagai referensi/legacy; token asli di docstring dihapus.

### Diketahui / batasan

- Modul compile-time; tidak ada upload runtime.
- Satu proses, satu sekolah per deployment.
- Tidak ada 2FA.
- Tidak ada enkripsi database at-rest dari aplikasi.
- Tidak ada UI audit log.
- Tidak ada scheduler backup/prune bawaan.
- SSRF melalui `base_url` mungkin; mitigasi dengan egress firewall/allowlist.
- Apply nyata ke Dapodik tidak dijalankan otomatis oleh test.
