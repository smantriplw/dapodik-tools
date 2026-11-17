# Referensi API Internal

API ini dipakai oleh UI sendiri, bukan API publik. Kontrak dapat berubah antar versi. Semua contoh memakai placeholder.

Spesifikasi mesin: [openapi.yaml](openapi.yaml) (OpenAPI 3.0.3).

Admin mengelola beberapa **scope Dapodik** di `/admin/scopes`; setiap scope punya kredensial terenkripsi sendiri. Operator hanya bisa memakai scope yang di-assign. Endpoint modul menerima `scope_id` dan mengambil kredensial dari scope tersebut.

## 1. Konvensi

- Base URL: `https://DOMAIN`.
- Format JSON: `application/json; charset=utf-8`.
- Autentikasi: cookie sesi `smanti_session` (HttpOnly).
- Semua request non-GET wajib CSRF:
  - form: field `_csrf`;
  - fetch/JSON: header `X-CSRF-Token`.
- Error JSON:
  ```json
  {"error": "pesan"}
  ```
- Halaman/JSON memakai `Cache-Control: no-store`.
- Role: `admin`, `operator` (nilai internal `guru`).
- Semua endpoint selain login/health/static membutuhkan sesi aktif.
- Job hanya bisa diakses pemiliknya atau admin.

### Status code yang umum

| Code | Arti |
|---|---|
| 200 | OK |
| 202 | Job diterima/diantrekan |
| 303 | Redirect setelah form |
| 400 | Validasi input gagal |
| 401 | Belum login / kredensial salah |
| 403 | Role/CSRF/akses job ditolak |
| 404 | Resource tidak ditemukan |
| 409 | Konflik status (preview belum selesai, job selesai, dll.) |
| 502 | Gagal mengambil data dari Dapodik |
| 500 | Kesalahan internal |

## 2. Autentikasi

### `GET /login`

Menampilkan form login. Membuat session anonim berisi CSRF.

### `POST /login`

Form-urlencoded:

| Field | Wajib | Keterangan |
|---|---|---|
| `_csrf` | ya | token dari form |
| `username` | ya | case-insensitive |
| `password` | ya | |
| `next` | tidak | path internal untuk redirect |

Sukses: `303` ke `next` atau `/`, cookie sesi baru.
Gagal: `401` halaman login dengan pesan; rate limit 5/15 menit per IP+username.

### `POST /logout`

Butuh sesi + CSRF. Menghapus cookie sesi, redirect `/login`.

## 3. Dashboard dan akun

### `GET /`

Dashboard HTML.

### `GET /account/password`, `POST /account/password`

Form ganti password. Field: `_csrf`, `current`, `next`, `confirm`. Minimal 8 karakter.

## 4. Job

### `GET /jobs`

Daftar job. Query `page` (default 1, 20 per halaman). Operator melihat miliknya; admin melihat semua.

### `GET /jobs/{id}`

Halaman detail job: status, ringkasan, log awal, item terbaru, unduhan.

### `GET /jobs/{id}/snapshot?after=<seq>`

Snapshot JSON untuk polling fallback.

Respons:

```json
{
  "job": {
    "id": "JOB_ID",
    "status": "running",
    "error": "",
    "created_at": "2026-01-01T00:00:00Z",
    "started_at": "...",
    "finished_at": ""
  },
  "summary": {},
  "items": [
    {
      "id": 1,
      "group": "X A",
      "nama": "NAMA_SISWA",
      "nisn": "NISN",
      "registrasi_id": "REGISTRASI_ID",
      "old_nipd": "",
      "nipd": "168052",
      "status": "ok",
      "message": "Berhasil mengupdate RegistrasiPesertaDidik"
    }
  ],
  "events": [
    {
      "seq": 12,
      "ts": "2026-01-01T00:00:00Z",
      "level": "info",
      "message": "PUT ...",
      "data": {}
    }
  ],
  "counters": {
    "total": 10,
    "current": 4,
    "done": 3,
    "failed": 1,
    "skipped": 0
  }
}
```

`items` dibatasi 200 terbaru untuk tampilan. CSV/JSON berisi semua item.

### `GET /jobs/{id}/events`

SSE `text/event-stream`.

- Replay dari `Last-Event-ID` atau query `after`.
- Event:
  ```
  id: 12
  event: message
  data: {"seq":12,"ts":"...","level":"info","message":"...","data":{}}
  ```
- Saat job selesai dan tidak ada event lagi:
  ```
  event: end
  data: {"status":"done"}
  ```
- Heartbeat: `: keep-alive`.
- Poll DB 400 ms.
- Reverse proxy harus `proxy_buffering off` / Caddy `flush_interval -1`.

### `POST /jobs/{id}/cancel`

Membatalkan job milik sendiri (atau admin). Respons JSON:

```json
{"status": "cancelling"}
```

Job queued langsung `cancelled`; job running berhenti sebelum operasi tulis berikutnya.

### `GET /jobs/{id}/download.csv`

CSV UTF-8 BOM. Kolom:

```
rombel,nama,nisn,registrasi_id,nipd_lama,nipd_baru,status,pesan
```

Nilai yang diawali `= + - @` diberi prefix `'` untuk mencegah formula injection.

### `GET /jobs/{id}/download.json`

JSON lengkap job + summary + items + public params (tanpa cookie/token).

### `GET /jobs/{id}/download.rollback.csv`

Khusus job `nipd.renumber.apply`. CSV UTF-8 BOM berisi siswa yang berhasil diubah:

```
registrasi_id,nama,nisn,rombel,nipd_lama,nipd_baru,status
```

Hanya item `status=ok` yang ditulis. Simpan file ini untuk rollback manual. Job tipe lain mendapat `400`.

## 5. Modul NIPD Filler

### `GET /nipd-filler/`

Halaman form/preview/apply dengan mode **Isi NIPD kosong** dan **Renumber NIPD lama**.

### `POST /nipd-filler/api/preview`

Body JSON:

```json
{
  "scope_id": "SCOPE_ID",
  "limit": 20000,
  "delay": 0.3,
  "start_nipd": null
}
```

Aturan:

- `scope_id` scope yang di-assign ke operator. Jika hanya ada satu scope, boleh dikosongkan.
- Kredensial (URL, sekolah, cookie, token) diambil dari scope dan disimpan terenkripsi.
- Operator tidak bisa memakai scope di luar whitelist (`403`).
- `limit` 1–50000; default 20000.
- `delay` 0–10 detik. Form default 0.3; jika field tidak dikirim lewat API, nilainya 0.
- `start_nipd` opsional, tidak boleh negatif.

Respons `202`:

```json
{"job_id": "JOB_ID"}
```

Job type: `nipd.preview`. Public params tidak memuat cookie/token. Kesalahan Dapodik (cookie habis, 401/403, respons tidak valid) tidak membatalkan HTTP 202; job akan berstatus `failed` dan pesannya terlihat di snapshot/SSE/log.

### `GET /nipd-filler/api/preview/{job_id}`

Data preview lengkap untuk semua grup.

Respons:

```json
{
  "summary": {
    "mode": "preview",
    "total_students": 1607,
    "filled": 1607,
    "empty": 0,
    "highest_nipd": 168657,
    "start_nipd": 168658,
    "no_rombel": 6,
    "groups": 43,
    "items": 0,
    "fingerprint": "SHA256"
  },
  "groups": [
    {"label": "X A", "total": 40, "empty": 0},
    {"label": "(Tanpa Rombel)", "total": 6, "empty": 0}
  ],
  "items": [
    {
      "group": "X A",
      "nama": "NAMA_SISWA",
      "nisn": "NISN",
      "registrasi_id": "REGISTRASI_ID",
      "old_nipd": "",
      "nipd": "168658",
      "status": "planned"
    }
  ]
}
```

Job harus `done` dan milik user/admin.

### `POST /nipd-filler/api/recalc`

Body:

```json
{
  "job_id": "PREVIEW_JOB_ID",
  "groups": ["X A", "X B"],
  "start_nipd": null
}
```

- `groups` tidak dikirim → semua grup.
- `groups: []` → tidak ada nomor dialokasikan.
- `start_nipd` tidak dikirim → memakai start preview.

Respons sama seperti preview data, dihitung ulang dari item preview tanpa fetch Dapodik.

### `POST /nipd-filler/api/apply`

Body:

```json
{
  "preview_job_id": "PREVIEW_JOB_ID",
  "groups": ["X A"],
  "delay": 0.3,
  "start_nipd": null
}
```

Aturan:

- Preview harus `done`, milik user/admin, dan bertipe `nipd.preview`.
- Minimal satu grup; semua label harus ada di preview.
- Kredensial diambil dari scope preview; operator harus masih punya akses.
- Jika pengaturan scope (URL/sekolah/NPSN) berubah sejak preview → `409`, tanpa menulis.
- Respons `202 {"job_id": "..."}`.
- Job type: `nipd.apply`.
- Apply fetch ulang dan membandingkan fingerprint preview. Jika berubah → gagal tanpa menulis.

### `POST /nipd-filler/api/renumber/preview`

Mode renumber: mengganti NIPD lama yang nilainya **lebih besar dari** `threshold`. Body:

```json
{
  "scope_id": "SCOPE_ID",
  "limit": 20000,
  "delay": 0.3,
  "threshold": 17978,
  "operator": ">",
  "start_nipd": 17979
}
```

Aturan:

- `threshold` ≥ 0; `start_nipd` ≥ 0.
- `operator` salah satu `>`, `>=`, `<`, `<=`; default `>` jika tidak dikirim.
- Hanya NIPD numerik yang memenuhi operator terhadap threshold yang diubah.
- `start_nipd` tidak harus di atas threshold; konflik tetap dicek.
- NIPD baru berurutan mulai `start_nipd`; urutan grup X A → XII Z, lalu `(Tanpa Rombel)`.
- NIPD kosong/non-numerik tidak diubah.
- Job type: `nipd.renumber.preview`. Respons `202 {"job_id":"..."}`.

### `GET /nipd-filler/api/renumber/preview/{job_id}`

Data preview renumber. `summary.mode = "renumber"`; field tambahan:

```json
{
  "threshold": 17978,
  "operator": ">",
  "eligible": 607,
  "kept": 1000,
  "skipped": 0,
  "old_min": 168051,
  "old_max": 168657,
  "new_min": 17979,
  "new_max": 18585,
  "conflict_count": 0,
  "conflicts": []
}
```

`groups[]` memiliki field `eligible`; `items[]` hanya siswa yang akan diubah (status `preview`).

`conflict_count > 0` berarti ada NIPD non-target di rentang baru. Apply akan ditolak; ubah threshold/operator/start atau pilihan grup dan muat preview ulang.

### `POST /nipd-filler/api/renumber/recalc`

Body:

```json
{
  "job_id": "PREVIEW_JOB_ID",
  "groups": ["X A", "X B"]
}
```

Hanya memilih grup; `threshold` dan `start_nipd` mengikuti preview. Respons sama dengan preview data. `groups` tidak dikirim berarti semua grup; array kosong berarti tidak ada grup terpilih.

### `POST /nipd-filler/api/renumber/apply`

Body:

```json
{
  "preview_job_id": "PREVIEW_JOB_ID",
  "groups": ["X A", "X B"],
  "delay": 0.3
}
```

Aturan:

- Preview harus `done`, bertipe `nipd.renumber.preview`, dan milik user/admin.
- Minimal satu grup; semua label harus ada di preview.
- Scope harus sama dengan preview; perubahan URL/sekolah/NPSN → `409`.
- `threshold`/`operator`/`start_nipd` selalu diambil dari preview; tidak bisa dioverride di apply.
- Job fetch ulang, membandingkan fingerprint, lalu menolak jika ada konflik. Tidak ada PUT saat konflik/stale.
- Job type: `nipd.renumber.apply`. Setelah selesai, unduh rollback di `/jobs/{id}/download.rollback.csv`.

## 6. Modul Cek Siswa

### `GET /student-lookup/`

Halaman pencarian.

### `POST /student-lookup/api/search`

Body:

```json
{
  "nisn": "NISN1, NISN2, NISN3",
  "scope_id": "SCOPE_ID",
  "limit": 20000
}
```

- Pemisah NISN: koma, spasi, titik koma, baris baru, tab.
- Maksimal 100 NISN.
- Setiap NISN 4–20 digit.
- `scope_id` scope yang di-assign; boleh kosong jika hanya ada satu scope.
- Cache memori 5 menit per hash(scope, kredensial, limit).
- Kredensial diambil dari scope, tidak dari request.

Respons:

```json
{
  "results": [
    {
      "nama": "NAMA_SISWA",
      "nisn": "NISN",
      "rombel": "",
      "tingkat": "",
      "nipd": "168652",
      "registrasi_id": "REGISTRASI_ID",
      "has_rombel": false,
      "nipd_empty": false
    }
  ],
  "missing": ["NISN_TIDAK_DITEMUKAN"],
  "total_students": 1607,
  "fetched_at": "2026-01-01T00:00:00Z",
  "from_cache": false
}
```

## 7. Admin

Semua endpoint admin membutuhkan role `admin` dan CSRF untuk POST.

| Endpoint | Fungsi |
|---|---|
| `GET /admin/users` | Daftar pengguna |
| `GET /admin/users/new` | Form tambah |
| `POST /admin/users/new` | Simpan pengguna |
| `GET /admin/users/{id}` | Form edit |
| `POST /admin/users/{id}` | Simpan perubahan |
| `GET /admin/scopes` | Daftar scope Dapodik |
| `GET /admin/scopes/new` | Form scope baru |
| `POST /admin/scopes/new` | Simpan scope baru |
| `GET /admin/scopes/{id}` | Form edit scope |
| `POST /admin/scopes/{id}` | Simpan perubahan scope |
| `POST /admin/scopes/{id}/test` | Tes koneksi read-only scope |
| `POST /admin/scopes/{id}/delete` | Hapus scope |
| `GET /admin/dapodik` | Redirect lama ke `/admin/scopes` |
| `GET /admin/settings` | Form pengaturan umum |
| `POST /admin/settings` | Simpan pengaturan umum |
| `GET /admin/modules` | Daftar modul |
| `POST /admin/modules/{key}/toggle` | Aktif/nonaktif modul |

Pengguna: `username`, `name`, `role`, `password`, `active`, dan `scope_ids` (berulang, hanya untuk Operator). Username tidak diubah saat edit; password kosong berarti tidak diubah. Admin otomatis memakai semua scope.

Scope Dapodik (`POST /admin/scopes/new` dan `POST /admin/scopes/{id}`) memakai form-urlencoded:

| Field | Wajib | Keterangan |
|---|---|---|
| `_csrf` | ya | token CSRF |
| `name` | ya | nama unik scope |
| `base_url` | ya | URL Dapodik |
| `sekolah_id` | ya | UUID sekolah |
| `npsn` | tidak | NPSN |
| `cookie` | tidak | kosong = pertahankan cookie tersimpan |
| `token` | tidak | kosong = pertahankan token tersimpan |
| `delete_cookie` | tidak | `1` untuk menghapus cookie |
| `delete_token` | tidak | `1` untuk menghapus token |

Cookie/token dienkripsi AES-GCM per scope; nilai tidak pernah dikembalikan ke UI. `POST /admin/scopes/{id}/test` mengembalikan:

```json
{"ok": true, "message": "Koneksi scope Dapodik A berhasil. 1 baris diterima.", "rows": 1, "total": 1607, "scope": "Dapodik A"}
```

Jika gagal, HTTP 502 dengan `{"error":"..."}`. Assignment scope disimpan lewat field `scope_ids` pada form pengguna. Pengaturan umum (`/admin/settings`) hanya berisi `nipd.limit`, `nipd.delay`, dan `retention_days`.

Toggle modul langsung berlaku tanpa restart. Modul nonaktif mengembalikan 404 dan tidak bisa menerima job baru.

## 8. Utility

| Endpoint | Fungsi |
|---|---|
| `GET /healthz` | `{"app":"SMANTI Tools","status":"ok"}` |
| `GET /static/...` | Aset core |
| `GET /static/modules/<key>/...` | Aset modul |
| `GET /favicon.ico` | Redirect logo |

## 9. Contoh curl

Login (simpan cookie):

```bash
curl -c cookies.txt https://DOMAIN/login -o login.html
CSRF=$(grep -o 'name="_csrf" value="[^"]*"' login.html | sed 's/.*value="//;s/"//')
curl -b cookies.txt -c cookies.txt \
  -d "_csrf=$CSRF&username=USER&password=PASSWORD" \
  -o /dev/null -w '%{http_code} %{redirect_url}\n' \
  https://DOMAIN/login
```

Preview (butuh CSRF dari meta halaman):

```bash
CSRF=$(curl -s -b cookies.txt https://DOMAIN/nipd-filler/ | \
  grep -o 'name="csrf-token" content="[^"]*"' | sed 's/.*content="//;s/"//')
curl -b cookies.txt -H "X-CSRF-Token: $CSRF" -H 'Content-Type: application/json' \
  -d '{"scope_id":"SCOPE_ID","limit":20000,"delay":0.3}' \
  https://DOMAIN/nipd-filler/api/preview
```

Jangan menyimpan cookie/token di histori shell, file publik, atau repository. Untuk penggunaan normal, simpan kredensial lewat halaman Admin → Dapodik (Scopes) agar tidak perlu menempel cookie di setiap request.

## 10. Kompatibilitas

- API internal, tanpa versi.
- Perubahan dapat terjadi bersamaan dengan UI.
- `JobSnapshot`/SSE dapat bertambah field.
- Jangan membangun integrasi eksternal jangka panjang tanpa kesepakatan.
