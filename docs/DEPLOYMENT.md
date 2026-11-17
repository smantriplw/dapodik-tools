# Deployment VPS + Domain

Target: satu VPS kecil, satu proses aplikasi, SQLite, HTTPS. Tidak perlu Node, Redis, Postgres, Docker, atau runtime Python.

## 1. Prasyarat

- VPS Linux (amd64/arm64) dengan akses sudo.
- Domain/subdomain, mis. `nipd.sekolah.sch.id`.
- DNS A/AAAA mengarah ke VPS.
- Go 1.22+ di mesin build (bukan syarat runtime; binary statis).
- Port `80`/`443` untuk reverse proxy.
- Akses server Dapodik dari VPS. Jika Dapodik membatasi IP/Cloudflare, sesuaikan kebijakan akses.

Binary yang dihasilkan tidak membutuhkan Go di VPS. Build di mesin lokal lalu salin, atau build langsung di VPS.

## 2. Build

Dari root project:

```bash
go generate ./...
go test ./...
CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o dapodik-tool ./cmd/dapodik-tool
```

Untuk arsitektur lain:

```bash
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 \
  go build -trimpath -ldflags="-s -w" -o dapodik-tool ./cmd/dapodik-tool
```

Cek:

```bash
file dapodik-tool        # ELF ... statically linked
./dapodik-tool version   # 1.3.1
```

## 3. Menyiapkan user, direktori, dan binary

```bash
sudo useradd --system --home /opt/dapodik-tool --shell /usr/sbin/nologin dapodik
sudo mkdir -p /opt/dapodik-tool/data /opt/dapodik-tool/backup /etc/dapodik-tool
sudo cp dapodik-tool /opt/dapodik-tool/
sudo chown -R dapodik:dapodik /opt/dapodik-tool
sudo chmod 750 /opt/dapodik-tool
```

Struktur:

```
/opt/dapodik-tool/
├─ dapodik-tool
├─ data/app.db
└─ backup/
/etc/dapodik-tool/env
```

## 4. Environment

Salin `deploy/env.example`:

```bash
sudo cp deploy/env.example /etc/dapodik-tool/env
sudo chmod 600 /etc/dapodik-tool/env
sudo editor /etc/dapodik-tool/env
```

Buat secret:

```bash
openssl rand -hex 32
```

| Variabel | Wajib | Default | Keterangan |
|---|---|---|---|
| `APP_ENV` | production | `development` | Set `production` agar secret wajib dan aman. |
| `APP_ADDR` | tidak | `127.0.0.1:8000` | Alamat listen. Jangan buka ke publik; pakai proxy. |
| `APP_NAME` | tidak | `SMANTI Tools` | Nama tampilan. |
| `DATABASE_PATH` | tidak | `./data/app.db` | Path SQLite. Gunakan absolut di production. |
| `SESSION_SECRET` | production | — | Minimal 32 karakter di production. Buat hex 32 byte. |
| `CREDENTIALS_KEY` | tidak | turunan `SESSION_SECRET` | Opsional; kunci enkripsi kredensial semua scope Dapodik. Disarankan terpisah, 32+ karakter. |
| `SESSION_COOKIE_SECURE` | production | `false` | Set `1` di belakang HTTPS. |
| `TRUST_PROXY` | production | `false` | Set `1` di belakang Caddy/Nginx agar IP asli dipakai. |
| `DEFAULT_BASE_URL` | tidak | kosong | Prefill URL Dapodik. |
| `DEFAULT_SEKOLAH_ID` | tidak | kosong | Prefill UUID sekolah. |
| `DEFAULT_NPSN` | tidak | kosong | Prefill NPSN. |
| `LOG_LEVEL` | tidak | `info` | `debug`, `info`, `warn`, `error`. |
| `BOOTSTRAP_ADMIN_*` | tidak | kosong | Admin otomatis saat DB kosong. |

Catatan:

- `.env` di root project juga dibaca otomatis saat dev. Di production gunakan `EnvironmentFile` systemd.
- `SESSION_SECRET` yang berubah akan membatalkan semua sesi.
- `CREDENTIALS_KEY` mengubah enkripsi kredensial tersimpan. Jika kosong, nilainya diturunkan dari `SESSION_SECRET`; mengganti salah satunya membuat admin harus menyimpan ulang cookie/token.
- Jangan menaruh cookie/token Dapodik di env. Simpan lewat halaman **Admin → Dapodik (Scopes)**.
- `TRUST_PROXY=1` membuat aplikasi memakai `X-Forwarded-For` untuk IP klien. Hanya aktifkan di belakang proxy tepercaya; kalau tidak, rate limit login memakai IP proxy.
- `SESSION_COOKIE_SECURE=1` hanya untuk HTTPS. Jika diakses lewat HTTP, browser tidak mengirim cookie sesi dan login akan tampak berulang.

## 5. Membuat admin pertama

Jalankan dari direktori aplikasi dengan path database yang sama seperti production:

```bash
sudo -u dapodik sh -c 'cd /opt/dapodik-tool && \
  APP_ENV=development DATABASE_PATH=/opt/dapodik-tool/data/app.db \
  ./dapodik-tool create-admin'
```

`APP_ENV=development` hanya untuk perintah CLI ini; service tetap memakai `APP_ENV=production` dari systemd. `DATABASE_PATH` harus sama dengan yang dipakai service. Jangan mengandalkan `source /etc/dapodik-tool/env` di shell karena nilai seperti `APP_NAME=SMANTI Tools` mengandung spasi.

Alternatif bootstrap via env:

```env
BOOTSTRAP_ADMIN_USERNAME=admin
BOOTSTRAP_ADMIN_PASSWORD=GANTI_PASSWORD_KUAT
BOOTSTRAP_ADMIN_NAME=Administrator
```

Bootstrap hanya berjalan jika `users` kosong. Untuk non-interaktif, `create-admin` juga menerima `--username`, `--name`, dan `--password`.

Setelah login sebagai admin, buka **Administrasi → Dapodik (Scopes)** untuk membuat scope (mis. Dapodik A/B/C), mengisi base URL/sekolah/NPSN/cookie/token per scope, lalu assign scope ke operator lewat halaman Pengguna. Konfigurasi tunggal dari versi lama dimigrasikan otomatis menjadi scope **Dapodik Utama** saat startup. Lihat [ADMIN_GUIDE.md](ADMIN_GUIDE.md#3-kredensial-dapodik) untuk panduan cookie.

## 6. systemd

```bash
sudo cp deploy/dapodik-tool.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now dapodik-tool
sudo systemctl status dapodik-tool --no-pager
```

Log:

```bash
sudo journalctl -u dapodik-tool -f
```

Unit menggunakan:

- User non-root `dapodik`
- `ProtectSystem=strict`
- `ProtectHome=true`
- `ReadWritePaths` hanya `data/` dan `backup/`
- `Restart=always`

## 7. HTTPS dengan Caddy (disarankan)

Pasang Caddy, lalu:

```bash
sudo cp deploy/Caddyfile /etc/caddy/Caddyfile
sudo editor /etc/caddy/Caddyfile   # ganti domain
sudo systemctl reload caddy
```

Konfigurasi ini memakai `flush_interval -1` agar SSE tidak dibuffer. Caddy menoperators sertifikat otomatis.

Cek:

```bash
curl -I https://nipd.sekolah.sch.id/
curl -N https://nipd.sekolah.sch.id/jobs/JOB_ID/events   # butuh login/cookie
```

## 8. Alternatif Nginx

Gunakan `deploy/nginx.conf.example`:

- `location /` untuk aplikasi.
- `location ~ ^/jobs/[^/]+/events$` dengan `proxy_buffering off`, `proxy_cache off`, dan `proxy_read_timeout 1h`.
- `client_max_body_size 2m`.
- Sertifikat Let's Encrypt (certbot).

Tanpa konfigurasi SSE khusus, progres realtime bisa tertahan di buffer proxy.

## 9. Firewall

Contoh UFW:

```bash
sudo ufw allow OpenSSH
sudo ufw allow 80/tcp
sudo ufw allow 443/tcp
sudo ufw enable
```

Jangan membuka port `8000` ke publik. Aplikasi mendengarkan `127.0.0.1:8000`.

## 10. Backup

Buat backup konsisten tanpa menghentikan aplikasi:

```bash
sudo -u dapodik /opt/dapodik-tool/dapodik-tool backup \
  --out /opt/dapodik-tool/backup/app-$(date +%Y%m%d).db
```

Cron harian:

```cron
15 1 * * * cd /opt/dapodik-tool && sudo -u dapodik ./dapodik-tool backup --out /opt/dapodik-tool/backup/app-$(date +\%Y\%m\%d).db
```

Salin backup ke storage terpisah dan uji restore secara berkala. Backup berisi PII dan ciphertext kredensial semua scope; batasi akses. Simpan `CREDENTIALS_KEY`/`SESSION_SECRET` terpisah, karena tanpa key tersebut kredensial scope tidak dapat didekripsi.

Restore:

```bash
sudo systemctl stop dapodik-tool
sudo cp /opt/dapodik-tool/backup/app-20260101.db /opt/dapodik-tool/data/app.db
sudo chown dapodik:dapodik /opt/dapodik-tool/data/app.db
sudo chmod 600 /opt/dapodik-tool/data/app.db
sudo systemctl start dapodik-tool
```

Hapus `app.db-wal` dan `app.db-shm` lama jika ada, dan pastikan tidak ada proses lain memakai DB.

## 11. Prune

Hapus event/item job lebih lama dari 90 hari:

```bash
sudo -u dapodik /opt/dapodik-tool/dapodik-tool prune --days 90
```

Jadwalkan mingguan/bulanan. Ini tidak menghapus baris `jobs`, hanya detail event/item agar riwayat ringkas tetap ada.

## 12. Upgrade

1. Backup database.
2. Build binary baru.
3. Uji di staging bila ada perubahan skema/modul.
4. Ganti binary:
   ```bash
   sudo systemctl stop dapodik-tool
   sudo cp dapodik-tool.new /opt/dapodik-tool/dapodik-tool
   sudo chown dapodik:dapodik /opt/dapodik-tool/dapodik-tool
   sudo systemctl start dapodik-tool
   ```
5. Cek log dan halaman login.
6. Migrasi schema core/modul berjalan otomatis saat startup.

Rollback: kembalikan binary lama dan restore DB hanya jika migrasi tidak kompatibel. Simpan binary versi sebelumnya.

## 13. Verifikasi pasca-deploy

```bash
curl -sS https://nipd.sekolah.sch.id/healthz
```

Harus mengembalikan JSON `{"app":"SMANTI Tools","status":"ok"}`.

Checklist:

- Login admin berhasil.
- Satu atau lebih scope Dapodik dibuat dan **Tes koneksi** berhasil.
- Operator di-assign ke scope yang sesuai dan bisa preview tanpa menempel cookie.
- HTTPS valid dan HTTP redirect ke HTTPS.
- `SESSION_COOKIE_SECURE=1`, `TRUST_PROXY=1`.
- Cookie sesi HttpOnly.
- SSE berjalan (buka job, log bertambah tanpa refresh).
- Backup manual berhasil.
- Restore di lingkungan uji berhasil.
- Log tidak memuat cookie/token.
- Waktu server sinkron (NTP) dan tampilan memakai WITA.

## 14. Catatan kapasitas

- Tipikal satu scope/sekolah: ribuan siswa, puluhan ribu event job; SQLite cukup untuk beberapa scope sekaligus.
- Gunakan satu proses. Jangan menjalankan dua instance terhadap DB yang sama.
- Pantau ukuran `data/app.db`, `app.db-wal`, disk, dan pertumbuhan job. Prune/backup berkala.
- SSE membuka satu koneksi per halaman job. Puluhan operator bersamaan masih ringan; ini bukan aplikasi multi-tenant besar.
- Jangan menaruh DB di NFS/SMB. Gunakan disk lokal.
