# Operasi & Pemeliharaan

Panduan untuk penanggung jawab server. Fokus: menjaga layanan hidup, data aman, dan kredensial tidak bocor.

## 1. Checklist rutin

### Harian

- Cek service: `systemctl is-active dapodik-tool`.
- Cek health: `curl -sS https://DOMAIN/healthz`.
- Cek disk: `df -h`.
- Cek log error: `journalctl -u dapodik-tool --since today -p warning`.
- Pastikan backup harian terbentuk.

### Mingguan

- Uji login admin dan satu akun operator.
- Tinjau job `failed` di halaman Riwayat Job.
- Cek ukuran database dan WAL.
- Pastikan tidak ada dua proses aplikasi berjalan.
- Tinjau akun yang tidak aktif.

### Bulanan

- Uji restore backup di lingkungan terpisah.
- Jalankan prune sesuai kebijakan retensi.
- Tinjau daftar admin/operator dan role.
- Rotasi password admin bila perlu.
- Cek pembaruan keamanan OS dan Caddy/Nginx.
- Tinjau audit log untuk aktivitas mencurigakan.

## 2. Perintah service

```bash
sudo systemctl status dapodik-tool
sudo systemctl restart dapodik-tool
sudo systemctl stop dapodik-tool
sudo systemctl start dapodik-tool
sudo journalctl -u dapodik-tool -f
sudo journalctl -u dapodik-tool --since "2 hours ago" --no-pager
```

Jangan menghentikan service saat apply sedang berjalan jika bisa dihindari. Jika terpaksa, job akan ditandai gagal saat startup berikutnya dan sebagian update mungkin sudah terjadi. Periksa hasil di Dapodik lalu muat preview ulang.

## 3. Lokasi data

| Data | Lokasi | Sensitivitas |
|---|---|---|
| Database | `/opt/dapodik-tool/data/app.db` + `-wal`/`-shm` | Tinggi (PII, hash password, audit, ciphertext kredensial) |
| Backup | `/opt/dapodik-tool/backup/` | Tinggi |
| Env/secret | `/etc/dapodik-tool/env` (`SESSION_SECRET`, `CREDENTIALS_KEY`) | Sangat tinggi |
| Log | journald | Sedang (jangan pernah berisi cookie/token) |
| Binary | `/opt/dapodik-tool/dapodik-tool` | Rendah |

Database dibuat mode `0600`; direktori data sebaiknya `0700`/`0750`. Backup juga `0600`.

## 4. Backup

### Manual

```bash
sudo -u dapodik /opt/dapodik-tool/dapodik-tool backup \
  --out /opt/dapodik-tool/backup/app-$(date +%Y%m%d-%H%M).db
```

Perintah memakai `VACUUM INTO`, aman saat aplikasi berjalan, dan menolak menimpa file yang sudah ada.

Backup berisi ciphertext kredensial semua scope. Simpan `CREDENTIALS_KEY`/`SESSION_SECRET` terpisah dari backup; tanpa key tersebut, kredensial tidak dapat didekripsi.

### Jadwal

Cron:

```cron
15 1 * * * cd /opt/dapodik-tool && sudo -u dapodik ./dapodik-tool backup --out /opt/dapodik-tool/backup/app-$(date +\%Y\%m\%d).db
```

Systemd timer (alternatif):

```ini
# /etc/systemd/system/dapodik-backup.service
[Unit]
Description=Backup Dapodik Tool
After=network.target

[Service]
Type=oneshot
User=dapodik
WorkingDirectory=/opt/dapodik-tool
ExecStart=/bin/sh -c '/opt/dapodik-tool/dapodik-tool backup --out /opt/dapodik-tool/backup/app-$(date +%%Y%%m%%d-%%H%%M).db'
```

```ini
# /etc/systemd/system/dapodik-backup.timer
[Unit]
Description=Jadwal backup Dapodik Tool

[Timer]
OnCalendar=*-*-* 01:15:00
Persistent=true

[Install]
WantedBy=timers.target
```

Aktifkan:

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now dapodik-backup.timer
```

### Verifikasi backup

- File ada dan ukurannya wajar.
- Salin ke storage terpisah.
- Uji buka dengan SQLite read-only:
  ```bash
  sqlite3 /path/backup.db "PRAGMA integrity_check; SELECT COUNT(*) FROM users;"
  ```
- Uji restore di server lain/staging.

### Retensi

Simpan minimal 7 backup harian dan beberapa backup bulanan, sesuai kebijakan sekolah. Hapus backup lama secara aman; file berisi PII.

## 5. Restore

```bash
sudo systemctl stop dapodik-tool
sudo mv /opt/dapodik-tool/data/app.db /opt/dapodik-tool/data/app.db.broken
sudo rm -f /opt/dapodik-tool/data/app.db-wal /opt/dapodik-tool/data/app.db-shm
sudo cp /path/backup.db /opt/dapodik-tool/data/app.db
sudo chown dapodik:dapodik /opt/dapodik-tool/data/app.db
sudo chmod 600 /opt/dapodik-tool/data/app.db
sudo systemctl start dapodik-tool
```

Setelah restore:

- Login admin.
- Cek halaman pengguna dan riwayat job.
- Cek `journalctl` untuk error migrasi.
- Jika secret session berubah, semua operator harus login ulang (normal).

## 6. Prune & retensi

```bash
sudo -u dapodik /opt/dapodik-tool/dapodik-tool prune --days 90
```

Perilaku:

- Menghapus `job_events` dan `job_items` untuk job yang lebih lama dari N hari.
- Baris `jobs` tetap ada (riwayat ringkas).
- File database tidak langsung mengecil; SQLite memakai ruang kosong untuk data berikutnya. Jangan jalankan `VACUUM` saat ramai.

Jadwalkan bulanan. Sesuaikan `retention_days` di Pengaturan agar konsisten dengan kebijakan.

Systemd timer prune (opsional):

```ini
# /etc/systemd/system/dapodik-prune.service
[Unit]
Description=Prune riwayat Dapodik Tool
After=network.target

[Service]
Type=oneshot
User=dapodik
WorkingDirectory=/opt/dapodik-tool
ExecStart=/opt/dapodik-tool/dapodik-tool prune --days 90
```

```ini
# /etc/systemd/system/dapodik-prune.timer
[Unit]
Description=Jadwal prune Dapodik Tool

[Timer]
OnCalendar=monthly
Persistent=true

[Install]
WantedBy=timers.target
```

Aktifkan dengan `systemctl daemon-reload && systemctl enable --now dapodik-prune.timer`.

### Audit log

`prune` hanya menghapus `job_events` dan `job_items`, **bukan** `audit_log`. Audit log tumbuh terus. Jika kebijakan retensi mengharuskan pemangkasan, backup dulu lalu hapus manual dengan cutoff eksplisit:

```bash
sudo -u dapodik sqlite3 /opt/dapodik-tool/data/app.db \
  "DELETE FROM audit_log WHERE ts < '2025-01-01T00:00:00Z';"
```

Jangan menghapus audit tanpa kebijakan/izin sekolah.

## 7. Rotasi kredensial

### Cookie/token Dapodik per scope

Setiap scope memiliki cookie/token sendiri dan dipakai bersama operator yang di-assign ke scope tersebut. Jika bocor atau kedaluwarsa:

1. Login ulang Dapodik untuk scope tersebut (akun/sesi khusus tool).
2. Buka **Admin → Dapodik (Scopes) → Edit scope**.
3. Tempel cookie/token baru dan simpan.
4. Klik **Tes koneksi**.
5. Logout sesi Dapodik lama.
6. Periksa audit/job log untuk aktivitas mencurigakan.

Jangan logout dari sesi Dapodik yang cookie-nya masih disimpan; itu akan membuat job untuk scope tersebut gagal.

### Token webservice Dapodik

Jika token bocor:

- Rotasi/ganti token di pengaturan webservice Dapodik untuk scope terkait.
- Simpan token baru lewat Admin → Dapodik (Scopes) → Edit scope.
- Periksa audit/job log untuk aktivitas mencurigakan.

### `SESSION_SECRET` dan `CREDENTIALS_KEY`

Jika bocor atau dicurigai:

```bash
openssl rand -hex 32   # SESSION_SECRET baru
openssl rand -hex 32   # CREDENTIALS_KEY baru (disarankan)
sudo editor /etc/dapodik-tool/env
sudo systemctl restart dapodik-tool
```

Semua sesi login dibatalkan. Kredensial semua scope juga tidak dapat didekripsi; admin harus menyimpan ulang cookie/token setiap scope lewat Admin → Dapodik (Scopes). Ini juga menghapus cookie sesi tool yang mungkin dicuri.

### Password admin/operator

- Admin reset lewat halaman Pengguna.
- Jika admin terakhir kehilangan akses, buat admin baru lewat CLI:
  ```bash
  sudo -u dapodik /opt/dapodik-tool/dapodik-tool create-admin
  ```
  Gunakan username baru.

### Renumber NIPD massal (prosedur khusus)

1. Pastikan ada persetujuan tertulis dan alasan perubahan.
2. Backup database: `dapodik-tool backup` (lihat bagian 4) dan simpan di luar server.
3. Hentikan/koordinasikan job NIPD lain; satu worker, jangan jalankan CLI apply bersamaan.
4. Buka mode **Renumber NIPD lama** di web, isi batas dan NIPD awal, lalu preview.
5. Periksa jumlah eligible, rentang lama/baru, skipped, dan **konflik**. Jangan apply jika konflik > 0.
6. Apply; pantau SSE sampai selesai. Unduh CSV, JSON, dan **Rollback CSV**.
7. Verifikasi sampel di Dapodik dan jumlah NIPD baru. Simpan rollback CSV mode `0600`; hapus setelah tidak diperlukan.
8. Jika ada kegagalan, jangan mengulang buta; baca report, gunakan rollback CSV, dan susun preview baru.

## 8. Log dan monitoring

- `journalctl -u dapodik-tool` untuk request, startup, job, error.
- `/healthz` untuk liveness sederhana.
- Pantau kode HTTP 5xx, job `failed`, dan disk.
- Jangan mengirim log mentah ke pihak ketiga tanpa memeriksa PII/secret.
- Request logger hanya mencatat method, path, status, durasi, IP. Cookie/token tidak dicatat.
- Job log dapat memuat nama siswa, NIPD, NISN, dan registrasi_id. Perlakukan sebagai PII.

Contoh pemeriksaan cepat:

```bash
sudo journalctl -u dapodik-tool --since "24 hours ago" | grep -Ei "error|panic|401|403|500"
```

## 9. Insiden

### Cookie/token bocor

1. Rotasi token Dapodik untuk scope terkait.
2. Login ulang Dapodik, simpan cookie baru pada scope tersebut lewat Admin → Dapodik (Scopes).
3. Logout sesi Dapodik lama.
4. Cabut/ubah assignment operator bila perlu.
5. Ganti `SESSION_SECRET`/`CREDENTIALS_KEY` bila key/cookie tool juga bocor.
6. Periksa job/audit.
7. Beri tahu admin sekolah.

### Database rusak/hilang

1. Hentikan service.
2. Simpan file rusak untuk investigasi.
3. Restore backup terbaru.
4. Cek `PRAGMA integrity_check`.
5. Mulai service dan verifikasi.

### Server dikompromikan

1. Isolasi VPS dari jaringan.
2. Rotasi semua kredensial: SSH, token Dapodik, `SESSION_SECRET`, `CREDENTIALS_KEY`, password akun.
3. Restore dari backup yang diketahui bersih.
4. Simpan ulang kredensial Dapodik setelah key baru.
5. Audit akses Dapodik.
6. Pertimbangkan membangun ulang VPS.

### Job macet

- Cek apakah ada job `running` lain; hanya satu worker.
- Cek Dapodik dapat diakses dari server: `curl -I https://dapodik...`.
- Restart service akan menandai job `queued/running` gagal.
- Operator harus preview ulang sebelum apply.

## 10. Perubahan domain / pindah server

1. Siapkan server baru, user, env (termasuk `CREDENTIALS_KEY` yang sama jika backup berisi kredensial tersimpan), binary, data.
2. Restore backup DB.
3. Set `APP_ADDR=127.0.0.1:8000` dan `TRUST_PROXY=1`.
4. Pasang Caddy/Nginx dengan domain baru.
5. Uji login, preview, SSE, backup.
6. Alihkan DNS.
7. Matikan server lama setelah yakin.

Jangan menjalankan dua instance aktif dengan DB yang sama.

## 11. Kapasitas dan pemeliharaan SQLite

- Satu proses, satu writer.
- WAL dapat tumbuh saat banyak job; prune dan backup membantu.
- Jangan menghapus `app.db-wal` saat service hidup.
- Jangan menaruh DB di filesystem jaringan.
- Jika DB sangat besar, lakukan maintenance di luar jam kerja dan backup dulu.
- `VACUUM` opsional; butuh ruang disk sementara dan lock. Tidak dijalankan otomatis.

## 12. Checklist go-live

- [ ] HTTPS aktif dan valid.
- [ ] `SESSION_SECRET` production ≥32 karakter.
- [ ] `CREDENTIALS_KEY` 32+ karakter, terpisah dan tersimpan aman.
- [ ] `SESSION_COOKIE_SECURE=1`, `TRUST_PROXY=1`.
- [ ] Admin pertama dibuat; minimal dua admin.
- [ ] Akun operator dibuat.
- [ ] Scope Dapodik dibuat dan kredensialnya diisi.
- [ ] Tes koneksi setiap scope berhasil.
- [ ] Operator di-assign ke scope yang sesuai.
- [ ] Preview read-only berhasil memakai scope yang di-assign tanpa cookie manual.
- [ ] SSE terlihat realtime.
- [ ] Backup manual + restore staging berhasil.
- [ ] Cron/timer backup dan prune aktif.
- [ ] Firewall hanya 22/80/443.
- [ ] Log tidak memuat cookie/token.
- [ ] Operator diberi [Panduan Pengguna](USER_GUIDE.md) dan aturan cookie.
