# Troubleshooting

Cari gejala di tabel ringkas, lalu buka bagian detail. Jangan pernah menempelkan cookie/token asli ke tiket atau chat saat meminta bantuan.

## 1. Tabel gejala

| Gejala | Kemungkinan penyebab | Tindakan |
|---|---|---|
| Halaman putih/500 | Panic, template rusak, DB error | Lihat `journalctl`, cek DB/permission |
| Redirect terus ke login | Cookie sesi hilang/secret berubah | Login ulang; set `SESSION_SECRET` stabil |
| Login selalu “username/password salah” | Akun nonaktif/salah password | Admin reset/aktifkan akun |
| “Terlalu banyak percobaan” | Rate limit 5/15 menit | Tunggu 15 menit |
| “Sesi formulir kedaluwarsa” | CSRF token lama | Muat ulang halaman |
| Dapodik “Session telah habis” | Cookie kedaluwarsa | Admin simpan cookie baru (atau manual jika belum dikelola) |
| Dapodik 401/403 | Cookie/token/Cloudflare | Login ulang Dapodik, cek `cf_clearance` |
| “Obyek tidak ditemukan” | Base URL/endpoint/ID salah | Periksa URL, sekolah_id, NPSN |
| Preview 0 siswa | Sekolah/modul salah | Cek sekolah_id, NPSN, `pd_module` |
| NISN tidak ditemukan | Bukan siswa sekolah ini/data beda | Cek NISN dan modul Dapodik |
| Scope tidak muncul | Operator belum di-assign | Admin assign scope di halaman Pengguna |
| “Scope Dapodik tidak diizinkan” | Scope bukan milik operator | Pilih scope lain/hubungi admin |
| “Scope … belum memiliki cookie” | Cookie scope kosong | Admin isi cookie scope |
| Status kredensial “Rusak” | Key enkripsi berubah | Simpan ulang cookie/token scope |
| Siswa tidak masuk grup | `rombel` kosong | Cari grup `(Tanpa Rombel)` |
| “Tidak ada NIPD numerik” | Belum ada NIPD angka | Isi NIPD awal |
| Apply disabled | 0 NIPD kosong / belum preview | Preview ulang |
| “Data berubah sejak preview” | Data Dapodik berubah | Preview ulang |
| Apply gagal sebagian | Validasi Dapodik per siswa | Baca pesan item; nomor tidak dipakai ulang |
| Cancel tidak langsung | PUT sedang berjalan | Tunggu; cancel dicek sebelum PUT berikut |
| Progres tidak bergerak | SSE dibuffer proxy | Matikan buffering Caddy/Nginx |
| Job queued lama | Job lain berjalan/macet | Tunggu; restart menandai gagal |
| Job running setelah restart | Proses mati | Startup menandai failed; ulangi |
| DB “database is locked” | Dua instance/proses lama | Hentikan instance lain |
| CSS/JS 404 | Binary lama setelah edit aset | Rebuild binary |
| Font tidak muncul | CSP/offline/Google Fonts | Cek network/CSP; fallback sistem |
| Modul tidak muncul | Belum generate/build/restart | `go generate`, build, restart |
| Modul 404 | Modul dinonaktifkan admin | Aktifkan di Admin → Modul (langsung berlaku) |
| Backup “file sudah ada” | Nama file sama | Gunakan timestamp unik |
| Prune tidak mengecilkan file | SQLite mempertahankan free page | Normal; `VACUUM` manual bila perlu |
| Build gagal versi Go | Go < 1.22 | Pakai Go 1.22+ |
| Port 8000 sudah dipakai | Proses lain | Ganti `APP_ADDR`/hentikan proses |

## 2. Login dan session

### Login berhasil tetapi kembali ke halaman login

- `SESSION_COOKIE_SECURE=1` dipakai saat mengakses lewat HTTP. Cookie `Secure` tidak dikirim di HTTP. Gunakan HTTPS atau set `0` untuk dev lokal.
- Pastikan `TRUST_PROXY=1` di belakang Caddy/Nginx.

### Selalu diminta login

- Di dev, jika `SESSION_SECRET` tidak diset, secret dibuat acak setiap restart → semua sesi batal. Buat `.env` dengan secret stabil.
- Di production, pastikan `SESSION_SECRET` tidak berubah.
- Pastikan cookie tidak diblokir browser dan HTTPS valid.
- Jika di belakang proxy, pastikan `TRUST_PROXY=1` hanya jika proxy tepercaya.
- Cek jam server; cookie expiry memakai waktu sistem.

### Lupa password admin terakhir

Buat admin baru lewat CLI dengan username berbeda:

```bash
sudo -u dapodik /opt/dapodik-tool/dapodik-tool create-admin
```

Tidak ada reset password mandiri via email.

### `create-admin` gagal di production

Jika `APP_ENV=production` dan `SESSION_SECRET` kosong/terlalu pendek, config menolak start. Set secret dulu:

```bash
openssl rand -hex 32
```

## 3. CSRF

Gejala: “Sesi formulir kedaluwarsa” atau API `403 {"error":"CSRF token tidak valid"}`.

Tindakan:

- Muat ulang halaman.
- Login ulang.
- Pastikan fetch mengirim header `X-CSRF-Token` (UI sudah melakukannya).
- Jangan membuka dua tab lama lalu submit form dari tab yang sudah kedaluwarsa.

## 4. Dapodik

### “Session telah habis. harap melakukan login kembali”

- Cookie tidak valid/kedaluwarsa.
- Token Bearer saja tidak cukup; cookie wajib.
- Login ulang Dapodik untuk scope terkait, ambil cookie baru, simpan di **Admin → Dapodik (Scopes) → Edit scope**, lalu tes koneksi.
- Pastikan operator memakai scope yang benar.
- Pastikan `PHPSESSID`, `dapodik_rememberme`, `dapodik_username`, `dapodik_password` ikut tersalin.

### HTTP 401/403 dari Dapodik

- Sesi ditolak.
- Cloudflare challenge; `cf_clearance` mungkin terikat IP/UA.
- Coba dari jaringan yang sama dengan browser yang menyelesaikan challenge.
- Pastikan server tool tidak diblokir oleh firewall/geo policy Dapodik.

### “Obyek tidak ditemukan”

- Base URL salah (mis. menunjuk halaman lain).
- `sekolah_id`/NPSN salah.
- Endpoint Dapodik berbeda versi.
- Pastikan memakai `https://` dan tanpa path tambahan.

### Data tidak lengkap

- `limit` terlalu kecil? Tool paginasi, tetapi cek total server.
- `pd_module` hanya `pdterdaftar`; siswa mutasi/lulus tidak masuk.
- Siswa soft-deleted tidak ditampilkan server.
- NISN yang dicari mungkin milik sekolah lain.

### Scope dan kredensial Dapodik

- **Dropdown scope kosong**: admin belum meng-assign scope ke operator. Buka Pengguna → Edit → centang scope.
- **Hanya sebagian scope muncul**: whitelist operator memang dibatasi; admin melihat semua.
- **Tes koneksi gagal**: pastikan base URL/sekolah_id scope benar, cookie lengkap, dan server dapat menjangkau Dapodik.
- **Status “Rusak”**: `CREDENTIALS_KEY`/`SESSION_SECRET` berubah atau ciphertext rusak. Simpan ulang cookie/token.
- **Setelah restart kredensial hilang**: di dev, `SESSION_SECRET` mungkin acak karena tidak diset. Set secret stabil atau `CREDENTIALS_KEY`.
- **Operator logout dari Dapodik**: jika memakai cookie scope, logout dapat membatalkan sesi yang dipakai tool. Login ulang dan simpan cookie baru, atau gunakan akun khusus.
- **Ganti key**: semua kredensial scope harus diisi ulang.
- **Hapus kredensial**: centang Hapus cookie/token pada scope; operator tidak bisa lagi memakai scope tersebut sampai cookie diisi.
- **Hapus scope**: kredensial dan assignment operator terhapus; riwayat job tetap ada.
- **Operator pindah tugas**: edit user, hapus centang scope yang tidak berhak.

### Cloudflare

Jika Dapodik memakai Cloudflare:

- `cf_clearance` bisa diperlukan.
- Cookie ini sering terikat pada IP/User-Agent.
- Server memakai User-Agent `dapodik-tool/1.3.1`.
- Solusi: gunakan jalur jaringan yang disetujui, atau minta admin Dapodik menyesuaikan kebijakan. Tidak ada tombol UA di UI.

## 5. NIPD

### Preview 0 siswa

- Base URL/sekolah_id/NPSN salah.
- Cookie tidak punya akses ke sekolah tersebut.
- Modul `pdterdaftar` kosong.
- Cek log job untuk detail respons.

### Siswa tertentu tidak muncul

- NISN tidak ada di data `pdterdaftar`.
- NISN salah ketik.
- Siswa di sekolah lain.
- `rombel` kosong → masuk `(Tanpa Rombel)`, bukan hilang.
- Filter pencarian di preview hanya menyaring item yang akan diisi.

### “Tidak ada NIPD numerik; tentukan NIPD awal secara manual”

Isi **Opsi lanjutan → NIPD awal** (web) atau `--start-nipd` (CLI). Nomor yang sudah dipakai tetap dilewati.

### Apply disabled

- 0 NIPD kosong, atau
- tidak ada rombel dipilih, atau
- preview belum selesai/gagal.

### “Data berubah sejak preview”

Fingerprint data berbeda. Muat preview ulang, periksa perubahan, lalu apply lagi. Tidak ada tulisan yang dilakukan pada percobaan yang gagal.

### Sebagian item gagal

- Baca pesan per item.
- Nomor NIPD yang gagal tidak dipakai ulang.
- Perbaiki penyebab (mis. sesi habis, validasi Dapodik), lalu preview ulang.
- Jangan mengulang apply dengan preview lama.

### Cancel

- Cancel mengeset flag; handler berhenti sebelum PUT berikutnya.
- PUT yang sedang berjalan bisa selesai.
- Job queued langsung dibatalkan.

### Renumber: “Konflik NIPD pada rentang baru”

Ada NIPD milik siswa yang tidak ikut di-renumber (tidak lolos operator, atau grupnya tidak dipilih) di rentang NIPD baru. Perbaiki dengan:

- naikkan NIPD awal baru,
- ubah batas, atau
- pilih grup yang mencakup pemilik NIPD tersebut.

Muat preview ulang; apply akan tetap ditolak selama konflik ada.

### Renumber: “Data berubah sejak preview”

Sama seperti mode isi: fetch ulang berbeda dari fingerprint preview. Muat preview renumber ulang. Tidak ada PUT pada percobaan yang gagal.

### Renumber: Rollback CSV kosong/tidak ada

Tombol rollback hanya muncul untuk job `nipd.renumber.apply`. File hanya berisi item `ok`; jika semua gagal atau job bukan renumber, file kosong/`400` adalah perilaku yang benar. Periksa report CSV job dan event.

### Renumber: angka baru bentrok setelah apply

Jangan mengulang apply buta. Gunakan rollback CSV, periksa NIPD yang sudah terpakai, lalu susun rencana baru dengan batas/start yang bebas konflik.

## 6. SSE dan job

### Log/progres tidak update

1. Buka DevTools → Network → `events`; pastikan status 200 dan `text/event-stream`.
2. Pastikan proxy tidak buffering:
   - Caddy: `flush_interval -1`.
   - Nginx: `proxy_buffering off`, `proxy_cache off`.
3. Cek CSP tidak memblokir (harus `connect-src 'self'`).
4. UI akan fallback ke polling jika EventSource gagal.

### Job queued lama

- Hanya satu worker. Lihat job running lain.
- Jika proses macet, restart service; job queued/running akan ditandai gagal.

### Job hilang setelah restart

- Secret hanya di memori. Startup menandai job gagal.
- Riwayat tetap ada; log menunjukkan alasan.

### Halaman job reload berulang

Versi lama memiliki bug ini. Pastikan memakai build terbaru yang memakai `data-after-seq` dan hanya reload jika status awal masih berjalan.

## 7. Database

### `database is locked`

- Ada dua proses memakai DB yang sama.
- Proses lama belum mati.
- Backup/`VACUUM` berjalan bersamaan.
- Pastikan hanya satu instance; WAL dan `busy_timeout` membantu, bukan solusi multi-writer.

### Permission denied

```bash
sudo chown -R dapodik:dapodik /opt/dapodik-tool/data /opt/dapodik-tool/backup
sudo chmod 600 /opt/dapodik-tool/data/app.db
sudo chmod 700 /opt/dapodik-tool/data
```

### Backup gagal “file sudah ada”

Gunakan nama unik:

```bash
... --out backup/app-$(date +%Y%m%d-%H%M%S).db
```

### Prune tidak mengecilkan file

SQLite mempertahankan free pages. File bisa dipakai ulang. `VACUUM` manual memerlukan ruang dan lock; backup dulu.

## 8. Deploy dan aset

### CSS/JS 404 setelah edit

Aset di-embed saat build. Rebuild:

```bash
CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o dapodik-tool ./cmd/dapodik-tool
sudo systemctl restart dapodik-tool
```

### Port sudah dipakai

```bash
ss -ltnp | grep 8000
```

Ganti `APP_ADDR` atau hentikan proses lain.

### Halaman tidak bisa diakses dari internet

- Aplikasi listen `127.0.0.1`; akses lewat Caddy/Nginx.
- Firewall 80/443.
- DNS mengarah ke VPS.
- Cek `curl -I https://DOMAIN`.

### HTTPS/redirect

- Caddy menoperators TLS otomatis.
- Nginx perlu certbot.
- Set `SESSION_COOKIE_SECURE=1` dan `TRUST_PROXY=1`.

## 9. Modul

### Modul baru tidak muncul

1. Pastikan folder punya `module.go` dan `init()` memanggil `registry.Register`.
2. Jalankan `go generate ./...`.
3. Cek `internal/app/modules_gen.go` memuat import modul.
4. Build ulang binary.
5. Restart service (menambah modul baru masih butuh restart; toggle aktif/nonaktif tidak).
6. Cek `/admin/modules`; aktifkan jika nonaktif.

### Route modul 404

- Modul dinonaktifkan admin (langsung berlaku).
- Binary belum di-rebuild untuk modul baru.
- Path/pattern route berbeda.
- Cek `/admin/modules` dan log startup `modul terdaftar`.

### Template modul error

- File harus mendefinisikan `content`.
- Nama render harus `<key>/<file>.html`.
- `templates/` di-embed dengan pola yang benar.

## 10. Build

### `go: go.mod requires go >= ...`

Gunakan Go 1.22+ dan dependency yang sudah dipin. Jangan menjalankan `go get -u` tanpa menguji; versi baru `modernc.org/sqlite` bisa meminta Go lebih tinggi.

### Build tidak statis

Gunakan `CGO_ENABLED=0`. Cek:

```bash
file dapodik-tool
ldd dapodik-tool
```

### Test gagal

- Jalankan `go generate ./...` dulu.
- `gofmt -l .` harus kosong.
- Cek Go version.
- Jangan menjalankan test paralel terhadap DB produksi; test memakai temp dir.

## 11. Diagnostik aman

```bash
sudo systemctl status dapodik-tool
sudo journalctl -u dapodik-tool -n 100 --no-pager
curl -sS https://DOMAIN/healthz
ss -ltnp | grep 8000
df -h
sudo -u dapodik sqlite3 /opt/dapodik-tool/data/app.db "PRAGMA integrity_check;"
```

Jangan menjalankan:

- `cat` cookie dari browser ke terminal yang direkam.
- `journalctl | grep -i cookie` lalu menempel hasilnya.
- Apply ke produksi untuk “mencoba apakah jalan”.

Jika perlu bantuan, kirim:

- waktu kejadian;
- halaman/aksi;
- status job;
- pesan error yang sudah disensor;
- versi binary (`./dapodik-tool version`).
Jangan kirim cookie, token, password, atau daftar siswa.
