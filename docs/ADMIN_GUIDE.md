# Panduan Admin Sekolah

Panduan ini untuk pemegang role `admin`. Admin mengelola akun, **scope Dapodik**, whitelist scope operator, pengaturan, modul, dan seluruh job. Setiap scope punya kredensial sendiri yang disimpan terenkripsi; operator tidak perlu menempel cookie.

## 1. Membuat admin pertama

Admin pertama dibuat dari server, bukan dari web:

```bash
cd /opt/dapodik-tool
sudo -u dapodik ./dapodik-tool create-admin
```

Anda akan diminta username, nama, dan password. Alternatif non-interaktif:

```bash
sudo -u dapodik ./dapodik-tool create-admin \
  --username admin --name "Administrator" --password 'PASSWORD_KUAT'
```

Atau isi `BOOTSTRAP_ADMIN_USERNAME`, `BOOTSTRAP_ADMIN_PASSWORD`, dan `BOOTSTRAP_ADMIN_NAME` di env sebelum `serve` pertama. Bootstrap hanya berjalan jika tabel `users` masih kosong.

Syarat:

- Username 3–40 karakter: huruf, angka, titik, garis bawah, strip.
- Password minimal 8 karakter. Gunakan minimal 12 karakter untuk admin.
- Setelah admin pertama ada, buat akun operator lewat UI.

## 2. Halaman Pengguna

Menu **Administrasi → Pengguna**.

- **Tambah pengguna** — username, nama, role, password, status aktif, dan **scope Dapodik**.
- **Edit** — ubah nama, role, status, reset password, dan assignment scope.
- Username tidak bisa diubah setelah dibuat.
- Role: Admin atau Operator (nilai internal `guru`).
- **Scope Dapodik** hanya berlaku untuk Operator. Admin otomatis bisa memakai semua scope.
- Operator tanpa scope tidak bisa memakai modul NIPD/Cek Siswa.
- Admin tidak bisa menonaktifkan akun sendiri.
- Sistem menolak menonaktifkan/menurunkan admin terakhir yang aktif.
- Tidak ada fitur hapus permanen. Nonaktifkan akun agar riwayat job tetap utuh.
- Tidak ada daftar sesi per pengguna. Ganti `SESSION_SECRET` + restart untuk membatalkan semua sesi.

Rekomendasi:

- Satu akun per operator; jangan berbagi akun.
- Minimal dua admin aktif.
- Assign hanya scope yang benar-benar dibutuhkan operator.
- Tinjau akses scope setiap awal semester atau saat operator mutasi.

## 3. Kredensial Dapodik

Menu **Administrasi → Dapodik (Scopes)** (`/admin/scopes`). Satu scope = satu konfigurasi Dapodik, biasanya satu sekolah.

Setiap scope memiliki:

- **Nama** — bebas, mis. `Dapodik A`, `Dapodik B`, `SMAN 3 Palu`.
- **Base URL** — mis. `https://dapodik2.sekolah.sch.id`.
- **sekolah_id** — UUID sekolah.
- **NPSN** — opsional.
- **Cookie Dapodik** — seluruh cookie sesi.
- **Bearer token** — opsional.

### 3.1 Membuat / mengedit scope

1. Buka **Administrasi → Dapodik (Scopes)**.
2. Klik **Tambah scope**.
3. Isi nama, base URL, sekolah_id, NPSN.
4. Tempel cookie (dan token jika ada), lalu **Simpan scope**.
5. Buka kembali scope tersebut dan klik **Tes koneksi**.
6. Assign scope ke operator lewat **Administrasi → Pengguna → Edit**.

Nama scope harus unik. Menghapus scope akan menghapus kredensial dan assignment operator terkait; riwayat job tetap ada. Job yang sedang berjalan dapat selesai, tetapi apply berikutnya akan gagal jika scope sudah dihapus.

### 3.2 Langkah mengambil cookie Dapodik

1. Login ke Dapodik di browser, mis. `https://dapodik2.sekolah.sch.id`. Gunakan akun/sesi khusus tool bila tersedia.
2. Buka Developer Tools: tekan `F12` (atau klik kanan → Inspect).
3. Pilih tab **Network**.
4. Muat ulang halaman Dapodik (`Ctrl+R` / `Cmd+R`).
5. Klik salah satu request ke server Dapodik, mis. request `PesertaDidik` atau halaman utama.
6. Di panel kanan, buka **Headers → Request Headers**.
7. Cari header **Cookie**.
8. Salin **seluruh nilai Cookie**, bukan hanya `PHPSESSID`. Biasanya berisi:
   - `PHPSESSID`
   - `killme`
   - `dapodik_rememberme`
   - `dapodik_username`
   - `dapodik_password`
   - `cf_clearance` jika Dapodik memakai Cloudflare
9. Tempel ke field **Cookie Dapodik** pada scope yang sesuai.
10. Klik **Simpan scope**.
11. Klik **Tes koneksi**. Jika berhasil, muncul jumlah baris yang diterima.
12. Jika muncul `Session telah habis`, ulangi dari langkah 1 atau pastikan cookie tidak terpotong.

Panduan yang sama tersedia langsung di halaman scope.

### 3.3 Hal penting tentang cookie per scope

- Cookie adalah kunci sesi Dapodik. Jangan kirim ke chat/email, jangan taruh di file publik.
- Jika cookie disimpan, **jangan logout** dari sesi Dapodik tersebut; operator yang di-assign memakainya. Gunakan akun/sesi Dapodik khusus tool bila tersedia.
- Cookie punya masa berlaku. Jika job gagal dengan `Session telah habis`, ambil cookie baru dan simpan ulang pada scope yang benar.
- `cf_clearance` dapat terikat pada IP/User-Agent. Jika server tool berada di IP berbeda dari browser, Cloudflare bisa menolak.
- Tool tidak menampilkan kembali nilai cookie/token. Field kosong berarti mempertahankan nilai tersimpan.
- Gunakan checkbox **Hapus cookie** / **Hapus token** untuk mencabut kredensial.
- Satu scope dipakai bersama operator yang di-assign ke scope tersebut. Jangan mencampur sekolah berbeda dalam satu scope.

### 3.4 Enkripsi dan kunci

- Cookie/token disimpan terenkripsi **AES-256-GCM** di tabel `dapodik_scope_credentials`.
- Kunci enkripsi berasal dari `CREDENTIALS_KEY`; jika kosong, diturunkan dari `SESSION_SECRET`.
- `CREDENTIALS_KEY` disarankan terpisah dan 32+ karakter. Simpan di `/etc/dapodik-tool/env` dengan permission ketat.
- Mengubah `CREDENTIALS_KEY` atau `SESSION_SECRET` membuat kredensial lama tidak bisa didekripsi. Halaman scope menampilkan status **Rusak**; simpan ulang cookie/token.
- Backup database berisi ciphertext. Tanpa key yang benar, isinya tidak dapat dibaca. Simpan key terpisah dari backup.
- Admin dengan akses server + env + database secara teknis dapat memakai kredensial. Perlakukan role admin dan akses VPS sebagai akses penuh.

### 3.5 Rotasi kredensial per scope

Jika cookie/token bocor atau kedaluwarsa:

1. Login ulang Dapodik untuk scope tersebut.
2. Buka **Admin → Dapodik (Scopes) → Edit scope**.
3. Tempel cookie/token baru, simpan.
4. Klik **Tes koneksi**.
5. Logout sesi Dapodik lama.
6. Periksa audit/job log.

Jika `CREDENTIALS_KEY`/`SESSION_SECRET` dicurigai bocor:

1. Buat key baru.
2. Perbarui env.
3. Restart service.
4. Simpan ulang kredensial semua scope.
5. Semua sesi login tool batal jika `SESSION_SECRET` berubah.

## 4. Halaman Pengaturan

Menu **Administrasi → Pengaturan**. Base URL, sekolah, kredensial, dan assignment scope **tidak** di sini.

| Key | Arti | Contoh |
|---|---|---|
| `nipd.limit` | Limit baris per halaman | `20000` |
| `nipd.delay` | Jeda request default (detik) | `0.3` |
| `retention_days` | Retensi event/item job | `90` |

Catatan:

- `retention_days` hanya dipakai `prune`; tidak ada penghapusan otomatis kecuali dijadwalkan.
- Perubahan langsung berlaku untuk form berikutnya.
- Nilai default env hanya dipakai saat key belum ada.

## 5. Halaman Modul

Menu **Administrasi → Modul**.

- Menampilkan modul yang dikompilasi ke binary, versi, deskripsi, dan status.
- **Nonaktifkan/Aktifkan** langsung berlaku **tanpa restart**.
- Modul nonaktif: route modul mengembalikan 404, menu/kartu dashboard hilang, dan submission job baru ditolak.
- Job yang sudah berjalan/queued tidak dibatalkan otomatis.
- Menambah modul baru tetap memerlukan folder modul, `go generate ./...`, dan rebuild binary. Lihat [ADDING_MODULES.md](ADDING_MODULES.md).

## 6. Riwayat Job

Menu **Riwayat Job**.

- Operator hanya melihat job miliknya; admin melihat semua job.
- Detail job menampilkan status, ringkasan (termasuk scope), log, item, dan unduhan CSV/JSON.
- Job yang masih berjalan bisa dibatalkan oleh pemilik atau admin.
- Job yang berjalan saat server restart ditandai gagal pada startup berikutnya karena secret job hanya di memori.
- Job `queued` menunggu satu worker; tidak ada paralelisme tulis ke Dapodik.
- Apply memvalidasi ulang akses scope dan mendeteksi perubahan pengaturan scope sejak preview.
- Job **Renumber NIPD** menyediakan unduhan **Rollback CSV** berisi NIPD lama → baru untuk item yang berhasil. Simpan sebelum melakukan perubahan lain.

## 7. Audit log

Audit ditulis ke tabel `audit_log` di SQLite. Tidak ada halaman audit di UI versi 1.3.1. Contoh inspeksi read-only:

```bash
sudo -u dapodik sqlite3 /opt/dapodik-tool/data/app.db \
  "SELECT ts, user_id, action, ip FROM audit_log ORDER BY id DESC LIMIT 50;"
```

Aksi antara lain:

- `login_success`, `login_failed`, `logout`, `password_changed`
- `user_created`, `user_updated`
- `settings_updated`
- `module_toggled`
- `dapodik_scope_created`, `dapodik_scope_updated`, `dapodik_scope_deleted`, `dapodik_scope_tested`
- `nipd_preview_submitted`, `nipd_apply_submitted`, `nipd_renumber_preview_submitted`, `nipd_renumber_apply_submitted`, `job_cancelled`
- `student_lookup`

Detail audit tidak memuat cookie/token. `prune` tidak memangkas `audit_log`; lihat [OPERATIONS.md](OPERATIONS.md).

## 8. Tanggung jawab keamanan admin

- Pastikan HTTPS aktif dan `SESSION_COOKIE_SECURE=1`.
- Simpan `CREDENTIALS_KEY` dan `SESSION_SECRET` di env dengan permission ketat.
- Ganti key bila dicurigai bocor; semua kredensial scope perlu disimpan ulang.
- Jangan pernah meminta operator mengirim cookie/token lewat chat.
- Assign scope sesuai kebutuhan; cabut akses dengan mengedit user.
- Nonaktifkan akun operator yang tidak bertugas.
- Batasi akses SSH ke VPS.
- Lindungi file `/etc/dapodik-tool/env` (mode `600`).
- Lindungi backup database; backup berisi PII dan ciphertext kredensial.
- Uji restore backup secara berkala.
- Tinjau scope, assignment, akun, dan modul aktif setiap semester.
- Renumber NIPD hanya atas persetujuan tertulis kepala sekolah/operator berwenang. Wajib dry-run, periksa konflik, dan simpan rollback CSV.
- Jika token/cookie bocor, rotasi dan simpan ulang pada scope terkait.
- Gunakan akun Dapodik khusus tool per scope bila memungkinkan.

## 9. Backup dan retensi (ringkas)

Backup:

```bash
sudo -u dapodik /opt/dapodik-tool/dapodik-tool backup \
  --out /opt/dapodik-tool/backup/app-$(date +%Y%m%d).db
```

Prune:

```bash
sudo -u dapodik /opt/dapodik-tool/dapodik-tool prune --days 90
```

Detail jadwal, restore, dan insiden ada di [OPERATIONS.md](OPERATIONS.md).

## 10. Batasan yang perlu diketahui

- Satu scope = satu konfigurasi Dapodik/sekolah; satu deployment bisa punya banyak scope.
- Satu proses aplikasi; jangan menjalankan beberapa instance terhadap database yang sama.
- Satu cookie/token per scope dipakai bersama operator yang di-assign; gunakan akun Dapodik khusus dan jangan logout dari sesi tersebut.
- Operator tidak bisa mengakses scope di luar whitelist.
- Tidak ada 2FA, pendaftaran mandiri, atau reset password via email.
- Tidak ada UI audit log, UI backup, atau scheduler bawaan.
- Modul bersifat compile-time, tetapi toggle aktif/nonaktif dinamis tanpa restart.
- Session stateless berumur 12 jam; tidak ada refresh otomatis.
- Rate limit hanya untuk login, bukan untuk job.
- Base URL Dapodik ditentukan admin per scope; operator tidak bisa mengarahkan ke host lain.
- Kredensial tersimpan tidak bisa dipulihkan jika `CREDENTIALS_KEY`/`SESSION_SECRET` hilang; simpan ulang.
