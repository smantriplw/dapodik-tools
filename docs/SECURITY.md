# Keamanan

Dokumen ini menjelaskan aset yang dilindungi, model ancaman, kontrol yang sudah ada, risiko yang diketahui, dan langkah hardening/insiden.

## 1. Aset dan klasifikasi data

| Aset | Klasifikasi | Lokasi |
|---|---|---|
| Kredensial scope Dapodik | Sangat sensitif | `dapodik_scope_credentials` terenkripsi AES-GCM; tidak pernah dirender |
| Assignment operator-scope | Internal | `user_dapodik_scopes` |
| `CREDENTIALS_KEY` | Sangat sensitif | env server |
| Password hash akun | Sensitif | `users.password_hash` (bcrypt) |
| Nama, NISN, NIPD, registrasi_id | PII | `job_items`, log job, laporan CSV/JSON |
| Audit aksi admin/operator | Internal | `audit_log` |
| `SESSION_SECRET` | Sangat sensitif | env server |
| Database & backup | Sensitif (PII) | `data/`, `backup/` |
| Binary & source | Internal | server/repo |

## 2. Model ancaman (ringkas)

| Aktor/ancaman | Dampak | Kontrol |
|---|---|---|
| Penyerang internet tanpa akun | Akses data/aksi | Login wajib, HTTPS, session HttpOnly, rate limit, CSP |
| Operator dengan akun valid | Menyalahgunakan akses Dapodik | RBAC, audit, batas role, pemilik job, SOP |
| Admin nakal | Mengubah akun/settings | Minimal dua admin, audit log, akses server terbatas |
| Pencurian cookie Dapodik | Akses sesi Dapodik | Ciphertext per scope + key env; rotasi |
| Pencurian key + DB | Dekripsi kredensial tersimpan | Pisahkan key dari backup, permission ketat, rotasi |
| Admin menyalahgunakan kredensial | Akses beberapa scope | Minimal dua admin, audit, batasi akses VPS |
| Operator mengakses scope lain | Data sekolah lain | Whitelist scope per user, validasi di provider |
| XSS | Mencuri sesi/aksi | `html/template` escaping, CSP tanpa inline script, JS memakai textContent |
| CSRF | Aksi atas nama operator | Token session + header/hidden field, SameSite=Lax |
| SQL injection | Baca/ubah DB | Query parameterized |
| SSRF lewat base URL | Server mengakses host internal | Validasi scheme, autentikasi, egress firewall (risiko diketahui) |
| Formula injection di CSV | Eksekusi formula saat dibuka Excel | Prefix `'` untuk `= + - @` |
| Kehilangan server | Data hilang | Backup + restore teruji |
| Log bocor | PII/kredensial | Tidak mencatat cookie/token; batasi akses log |
| Supply chain | Binary berbahaya | Dependency pinned, build dari source, `go vet`/test |

## 3. Kontrol autentikasi dan sesi

- Password hash bcrypt cost 12.
- Password minimal 8 karakter (disarankan 12+ untuk admin).
- Username unik case-insensitive.
- Login rate limit: 5 kegagalan / 15 menit per IP+username.
- Session cookie:
  - nama `smanti_session`;
  - payload bertanda HMAC-SHA256;
  - `HttpOnly`, `SameSite=Lax`, `Secure` di production;
  - umur 12 jam.
- Login membuat session baru dengan CSRF baru.
- `CurrentUser` memuat ulang user dari DB dan menolak akun nonaktif.
- Role dicek per route.
- Logout menghapus cookie. Karena session stateless, salinan cookie tetap valid sampai expiry; rotasi `SESSION_SECRET` membatalkan semua sesi.

## 4. CSRF

- Token per session, disimpan di payload session bertanda tangan.
- Form mengirim `_csrf`; fetch mengirim `X-CSRF-Token`.
- Dicek constant-time.
- Berlaku untuk POST/PUT/PATCH/DELETE.
- Login POST juga divalidasi.
- SameSite=Lax sebagai lapisan tambahan.

## 5. RBAC dan kepemilikan job

- Operator (internal `guru`): menjalankan modul yang diizinkan dan melihat job sendiri.
- `admin`: semua job, pengguna, settings, modul, semua scope, dan kredensial scope.
- Operator (internal `guru`): hanya scope yang di-assign dan job sendiri.
- Job detail, snapshot, SSE, cancel, dan download memeriksa pemilik atau admin.
- Modul menyatakan role yang diizinkan saat route didaftarkan.

## 6. Penanganan kredensial Dapodik (per scope)

- Admin membuat scope di `/admin/scopes` dan menyimpan cookie/token per scope.
- Nilai dienkripsi **AES-256-GCM** dengan key dari `CREDENTIALS_KEY` (fallback: `SESSION_SECRET`).
- Ciphertext disimpan di `dapodik_scope_credentials`; nilai tidak pernah dirender kembali ke HTML.
- Operator hanya bisa me-resolve scope yang di-assign (`user_dapodik_scopes`); scope lain → 403.
- Admin otomatis bisa mengakses semua scope.
- Modul memakai `scope_id` yang dipilih; kredensial didekripsi hanya ke memori job.
- Tidak masuk `public_params`, log, audit, error, summary, atau CSV/JSON.
- Job queued kehilangan secret saat restart dan ditandai gagal.
- Setelah job selesai, secret dihapus dari memori.
- `Submit` menolak public param yang mengandung `cookie`, `token`, atau `password`.
- Halaman admin tidak menampilkan cookie/token; status hanya “ada/rusak/belum ada”.
- Jika key berubah, kredensial semua scope gagal didekripsi dan harus disimpan ulang.
- Satu cookie/token per scope dipakai bersama operator yang di-assign; worker serial mencegah apply paralel.

## 7. XSS dan CSP

- Template memakai `html/template` (auto-escaping).
- Semua data dinamis di JS dirender dengan `textContent`, bukan `innerHTML`.
- CSP:
  - `default-src 'self'`;
  - `script-src 'self'`;
  - `style-src 'self' https://fonts.googleapis.com`;
  - `font-src 'self' https://fonts.gstatic.com data:`;
  - `img-src 'self' data:`;
  - `connect-src 'self'`;
  - `frame-ancestors 'none'`;
  - `base-uri 'self'`;
  - `form-action 'self'`.
- Tidak ada inline script atau inline style.
- `X-Frame-Options: DENY`, `nosniff`, `Referrer-Policy: same-origin`.
- HSTS bila request HTTPS.

## 8. PII dan retensi

- Database menyimpan nama, NISN, NIPD, registrasi_id, log per siswa, dan ciphertext kredensial per scope.
- CSV/JSON juga berisi PII.
- File DB/WAL/SHM diupayakan `0600`; direktori data `0700`.
- Backup `0600` dan harus disimpan di storage terlindungi.
- Retensi default 90 hari; jalankan `prune` sesuai kebijakan.
- Jangan mengunggah laporan ke layanan publik.
- Hapus database dev/uji yang berisi data asli sebelum menyerahkan mesin.
- `hasil_nipd.json` lama berisi PII; file ini di-ignore dari git.

## 9. Risiko yang diketahui

### 9.1 SSRF melalui base URL

Admin menentukan `base_url` per scope (http/https). Operator tidak bisa mengganti host lewat modul. Server melakukan request ke URL scope dengan cookie/token scope; admin yang tidak tepercaya dapat mengarahkan ke host internal yang dapat dijangkau VPS.

Mitigasi saat ini:

- Hanya admin yang dapat mengubah base URL scope.
- Scheme dibatasi http/https.
- Operator hanya bisa memakai scope yang di-assign.
- Timeout dan limit respons.

Mitigasi lanjutan yang disarankan:

- Egress firewall VPS: hanya izinkan domain Dapodik yang disetujui.
- Allowlist host di aplikasi (belum tersedia).
- Pisahkan VPS tool dari jaringan sensitif.

### 9.2 Satu cookie admin dipakai bersama

Cookie admin adalah kredensial bersama. Jika bocor, semua job bisa memakai sesi tersebut. Gunakan akun Dapodik khusus tool, jangan logout dari sesi yang disimpan, batasi admin, dan rotasi cookie secara berkala.

### 9.3 Salah assign scope

Jika operator di-assign ke scope yang salah, ia dapat melihat/mengubah data sekolah lain. Tinjau assignment setiap semester; hapus scope dari user yang tidak berhak. Audit `user_updated` menyimpan daftar `scope_ids`.

### 9.4 Key enkripsi dan backup

Ciphertext tidak berguna tanpa `CREDENTIALS_KEY`/`SESSION_SECRET`. Namun jika penyerang mendapatkan DB **dan** env, kredensial dapat didekripsi. Simpan key terpisah dari backup, gunakan permission ketat, dan rotasi bila dicurigai.

### 9.5 Tidak ada 2FA

Password yang bocor langsung memberi akses. Gunakan password kuat, HTTPS, dan rotasi bila dicurigai.

### 9.6 Session stateless

Logout tidak membatalkan salinan cookie di tempat lain. Rotasi `SESSION_SECRET` adalah tombol darurat.

### 9.7 Enkripsi at-rest parsial

Hanya tabel `credentials` yang dienkripsi aplikasi. PII dan data lain di SQLite/backup tetap plaintext. Gunakan disk terenkripsi (LUKS) dan backup terenkripsi.

### 9.8 Log job memuat PII

Log menampilkan nama siswa/NIPD untuk kebutuhan operasional. Batasi akses journald/server dan retensi.

### 9.9 Rate limit hanya login

Tidak ada rate limit job atau pencarian. User terautentikasi dipercaya; batasi akun dan pantau job.

### 9.11 Renumber NIPD massal

Mode Renumber NIPD menimpa NIPD lama. Kesalahan batas/start dapat mengubah ratusan record sekaligus. Kontrol saat ini: preview read-only, konfirmasi manual, fingerprint anti-stale, deteksi konflik, dan rollback CSV. Rollback CSV berisi PII; simpan mode `0600`, jangan commit, dan hapus setelah tidak diperlukan. Wajib persetujuan admin dan backup DB sebelum apply besar.

### 9.10 Multi-scope per deployment

Satu database dapat memuat beberapa scope; pastikan whitelist scope benar dan jangan mencampur data dalam satu scope.

### 9.11 Cloudflare/`cf_clearance`

Cookie `cf_clearance` dapat terikat pada IP/User-Agent. Server memakai User-Agent `dapodik-tool/1.3.1`. Jika Cloudflare menolak, gunakan jalur jaringan yang disetujui atau sesuaikan konfigurasi Dapodik.

### 9.12 Modul compile-time dan toggle dinamis

Modul bukan upload runtime, sehingga tidak ada eksekusi kode arbitrer lewat UI. Toggle aktif/nonaktif langsung berlaku; menambah modul tetap rebuild binary dan review kode.

## 10. Hardening checklist production

- [ ] `APP_ENV=production`
- [ ] `SESSION_SECRET` 32+ byte acak, disimpan `0600`
- [ ] `CREDENTIALS_KEY` 32+ byte, terpisah dari backup
- [ ] Scope Dapodik dibuat, kredensialnya diisi, dan tes koneksi berhasil
- [ ] Operator di-assign hanya ke scope yang berhak
- [ ] Jangan logout dari sesi Dapodik yang cookie-nya disimpan
- [ ] `SESSION_COOKIE_SECURE=1`
- [ ] `TRUST_PROXY=1` hanya di belakang proxy tepercaya
- [ ] HTTPS valid, HTTP redirect
- [ ] Aplikasi listen `127.0.0.1`
- [ ] Firewall hanya 22/80/443
- [ ] User service non-root
- [ ] `data/` dan `backup/` permission ketat
- [ ] Backup harian + restore teruji
- [ ] Prune terjadwal
- [ ] Log tidak memuat cookie/token
- [ ] Admin minimal dua
- [ ] Akun operator aktif sesuai kebutuhan
- [ ] Egress policy/allowlist bila risiko SSRF relevan
- [ ] Disk terenkripsi jika kebijakan mensyaratkan
- [ ] Dependency pinned, build dari source, `go vet`/test
- [ ] Rotasi token/cookie bila ada indikasi bocor

## 11. Respons insiden

### Kredensial Dapodik bocor

1. Rotasi token webservice Dapodik jika dipakai.
2. Login ulang Dapodik dan simpan cookie baru pada scope terkait lewat Admin → Dapodik (Scopes).
3. Logout sesi Dapodik lama.
4. Cabut assignment operator bila perlu.
5. Periksa job/audit untuk aktivitas tidak dikenal.
5. Jika key enkripsi/`SESSION_SECRET` juga bocor, ganti keduanya, restart, lalu simpan ulang kredensial.

### Akun tool bocor

1. Nonaktifkan akun.
2. Reset password.
3. Ganti `SESSION_SECRET` untuk membatalkan sesi.
4. Tinjau job/audit.

### Database/backup bocor

1. Identifikasi cakupan data (nama/NISN/NIPD).
2. Ikuti prosedur pelaporan insiden sekolah.
3. Rotasi kredensial terkait.
4. Perketat akses file dan backup.

### Server dikompromikan

1. Isolasi VPS.
2. Rotasi SSH, token Dapodik, secret, password.
3. Restore dari backup bersih.
4. Audit akses Dapodik.
5. Pertimbangkan rebuild total.

## 12. Pelaporan

Catat insiden: waktu, aktor, data terdampak, langkah mitigasi, dan bukti. Jangan menyalin cookie/token ke tiket; gunakan referensi aman.

Untuk deployment dan operasi, lihat [DEPLOYMENT.md](DEPLOYMENT.md) dan [OPERATIONS.md](OPERATIONS.md).
