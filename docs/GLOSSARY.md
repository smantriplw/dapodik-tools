# Glosarium

## Istilah Dapodik

| Istilah | Arti |
|---|---|
| **Dapodik** | Data Pokok Pendidikan; aplikasi/server data sekolah. |
| **NPSN** | Nomor Pokok Sekolah Nasional. |
| **sekolah_id** | UUID sekolah di Dapodik. Berbeda dari NPSN. |
| **NISN** | Nomor Induk Siswa Nasional, biasanya 10 digit. |
| **NIPD** | Nomor Induk Peserta Didik di sekolah. Diisi pada `RegistrasiPesertaDidik`. |
| **registrasi_id** | UUID registrasi siswa; dipakai di endpoint update NIPD. |
| **peserta_didik_id** | UUID identitas siswa. |
| **rombel** | Rombongan belajar/kelas, mis. `X A`, `XI C4`, `XII A2`. Bisa kosong. |
| **tingkat_pendidikan_id** | Kode tingkat, mis. `10`, `11`, `12`. |
| **pd_module** | Modul data PesertaDidik; tool memakai `pdterdaftar`. |
| **Webservice token** | Token Bearer Dapodik. Opsional di tool; tidak cukup tanpa cookie. |
| **Cookie Dapodik** | Sesi login Dapodik (`PHPSESSID`, `dapodik_rememberme`, dll.). |
| **Kredensial admin** | Cookie/token Dapodik yang disimpan admin terenkripsi dan dipakai otomatis oleh operator. |
| **Cookie manual** | Cookie yang diisi operator per job; hanya di memori dan tidak disimpan. |
| **cf_clearance** | Cookie Cloudflare yang mungkin diperlukan Dapodik. |

## Istilah aplikasi

| Istilah | Arti |
|---|---|
| **Preview** | Mengambil data dan menghitung rencana NIPD tanpa menulis. |
| **Apply** | Mengirim PUT NIPD ke Dapodik. |
| **Plan** | Hasil perhitungan grup, NIPD awal, dan pasangan siswa → NIPD baru. |
| **Grup/rombel** | Pengelompokan siswa berdasarkan field `rombel`. |
| **`(Tanpa Rombel)`** | Grup siswa yang `rombel`-nya kosong; diproses terakhir. |
| **NIPD awal** | Nomor pertama yang dipakai. Default: NIPD tertinggi + 1. |
| **Nomor terpakai** | NIPD yang sudah ada; dilewati saat alokasi. |
| **Fingerprint** | Hash data siswa untuk mendeteksi perubahan antara preview dan apply. |
| **Job** | Satu eksekusi background (preview/apply/modul lain). |
| **Event** | Baris log job untuk SSE dan riwayat. |
| **Item** | Baris hasil per siswa dalam job. |
| **Worker** | Satu goroutine yang menjalankan job dari antrean. |
| **SSE** | Server-Sent Events; streaming progres satu arah. |
| **Snapshot** | Ringkasan status job untuk polling fallback. |
| **Cancel** | Permintaan berhenti; dicek sebelum operasi tulis berikutnya. |
| **Fingerprint stale** | Data berubah sejak preview sehingga apply ditolak. |
| **Renumber** | Mengganti NIPD lama dengan nomor baru; hanya NIPD numerik yang memenuhi operator batas. |
| **Threshold/batas NIPD** | Nilai pembanding untuk operator `>`, `>=`, `<`, atau `<=`. |
| **Eligible** | Siswa yang memenuhi syarat dan akan diubah pada rencana renumber. |
| **Operator NIPD** | Pembanding batas renumber: `>`, `>=`, `<`, `<=`. |
| **Konflik** | NIPD non-target yang sudah memakai nomor di rentang baru; apply diblokir. |
| **Rollback CSV** | CSV `nipd_lama → nipd_baru` untuk mengembalikan perubahan renumber secara manual. |
| **RBAC** | Pembatasan akses berdasarkan role (`admin`/`guru`; label Operator). |
| **Admin** | Role pengelola akun, kredensial Dapodik, pengaturan, modul, dan seluruh job. |
| **Operator** | Role yang menjalankan modul dan melihat job sendiri; nilai internal `guru`. |
| **CSRF token** | Token anti-forgery untuk request non-GET. |
| **Session** | Cookie bertanda tangan berisi user, expiry, CSRF. |
| **Scope Dapodik** | Satu konfigurasi Dapodik (URL, sekolah, cookie, token) dengan nama bebas, mis. Dapodik A/B. |
| **Whitelist scope** | Daftar scope yang di-assign admin ke operator. |
| **Dynamic module toggle** | Aktif/nonaktif modul yang langsung berlaku tanpa restart. |
| **Modul** | Paket Go yang mendaftarkan menu/route/job/settings/template/schema. |
| **Registry** | Daftar modul yang dikompilasi dan aktif. |
| **Compile-time module** | Modul ikut binary; butuh rebuild, bukan upload runtime. |
| **Prune** | Hapus event/item job lama. |
| **AES-GCM** | Enkripsi terautentikasi untuk kredensial tersimpan. |
| **CREDENTIALS_KEY** | Env opsional untuk kunci enkripsi kredensial; fallback ke `SESSION_SECRET`. |
| **Encrypted at rest** | Data sensitif disimpan dalam bentuk ciphertext. |
| **Backup** | Salinan konsisten database via `VACUUM INTO`. |
| **WITA** | Waktu Indonesia Tengah (UTC+8); zona tampilan. |

## Singkatan

| Singkatan | Arti |
|---|---|
| PII | Personally Identifiable Information (data pribadi) |
| SSE | Server-Sent Events |
| CSRF | Cross-Site Request Forgery |
| RBAC | Role-Based Access Control |
| CSP | Content Security Policy |
| HSTS | HTTP Strict Transport Security |
| SSRF | Server-Side Request Forgery |
| WAL | Write-Ahead Logging (SQLite) |
| UUID | Universally Unique Identifier |
