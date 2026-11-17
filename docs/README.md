# Dokumentasi SMANTI Tools — Dapodik

Versi aplikasi: **1.3.1**. Dokumentasi ini ditulis untuk empat audiens. Pilih jalur bacaan sesuai peran.

## Jalur bacaan

| Peran | Mulai dari | Lanjut ke |
|---|---|---|
| Operator | [Panduan Pengguna](USER_GUIDE.md) | [Troubleshooting](TROUBLESHOOTING.md), [Glosarium](GLOSSARY.md) |
| Admin sekolah | [Panduan Admin](ADMIN_GUIDE.md) | [Keamanan](SECURITY.md), [Operasi](OPERATIONS.md) |
| Teknisi / deployment | [Deployment](DEPLOYMENT.md) | [Operasi](OPERATIONS.md), [Troubleshooting](TROUBLESHOOTING.md) |
| Developer / kontributor | [Arsitektur](ARCHITECTURE.md) | [Menambah Modul](ADDING_MODULES.md), [API](API_REFERENCE.md), [OpenAPI](openapi.yaml), [Testing](TESTING.md) |

## Daftar dokumen

- [USER_GUIDE.md](USER_GUIDE.md) — login, pilih scope Dapodik, alur preview/apply/renumber, cek NISN, unduhan, kebiasaan aman.
- [ADMIN_GUIDE.md](ADMIN_GUIDE.md) — akun, scope Dapodik, whitelist scope, pengaturan, modul, riwayat, tanggung jawab admin.
- [DEPLOYMENT.md](DEPLOYMENT.md) — VPS, domain, HTTPS, systemd, Caddy/Nginx, backup, upgrade.
- [OPERATIONS.md](OPERATIONS.md) — checklist harian/mingguan/bulanan, restore, rotasi kredensial, insiden.
- [RENUMBER.md](RENUMBER.md) — operasi CLI khusus mengganti NIPD lama (dry-run, apply, rollback, peringatan).
- [ARCHITECTURE.md](ARCHITECTURE.md) — paket Go, alur request, job, SSE, skema database, algoritma NIPD.
- [ADDING_MODULES.md](ADDING_MODULES.md) — menambah menu, route, job, settings, template, static, schema modul.
- [API_REFERENCE.md](API_REFERENCE.md) — endpoint HTTP, payload, SSE, format CSV/JSON, status code.
- [openapi.yaml](openapi.yaml) — spesifikasi OpenAPI 3.0.3 untuk tooling/SDK/inspeksi.
- [SECURITY.md](SECURITY.md) — model ancaman, kontrol, risiko yang diketahui, hardening, respons insiden.
- [TESTING.md](TESTING.md) — unit/integration test, fake Dapodik, checklist QA manual, CI.
- [TROUBLESHOOTING.md](TROUBLESHOOTING.md) — gejala, penyebab, dan tindakan.
- [GLOSSARY.md](GLOSSARY.md) — istilah Dapodik dan istilah aplikasi.
- [CHANGELOG.md](CHANGELOG.md) — riwayat versi.

## Prinsip dokumentasi

- Semua contoh memakai placeholder: `https://dapodik.sekolah.sch.id`, `UUID_SEKOLAH`, `COOKIE_DAPODIK`, `TOKEN_WEBSERVICE`.
- Jangan pernah menaruh cookie, token, password, NISN, atau nama siswa asli di dokumentasi, issue, atau commit.
- Perintah `curl` di dokumentasi hanya contoh internal; jangan menjalankan apply ke data produksi tanpa preview dan persetujuan.
- Jika dokumentasi dan kode berbeda, kode adalah sumber kebenaran. Laporkan selisihnya agar dokumen diperbaiki.
