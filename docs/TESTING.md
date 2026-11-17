# Testing & QA

Dokumen ini menjelaskan test otomatis, uji manual, dan batas pengujian.

## 1. Perintah wajib

```bash
go generate ./...
gofmt -l .
go vet ./...
go test ./...
CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o dapodik-tool ./cmd/dapodik-tool
```

Ekspektasi:

- `gofmt -l .` tidak mengeluarkan file.
- `go vet ./...` tanpa error.
- Semua paket `ok`.
- Binary statis terbentuk.

## 2. Cakupan test otomatis

| Paket | Test |
|---|---|
| `internal/dapodik` | Urutan grup X→XII + `(Tanpa Rombel)`, pilihan grup, collision NIPD, tidak ada NIPD numerik, fingerprint, `BuildRenumberPlan` (strict >, start, eligible/conflict), parser JSON, error PHP-style, paginasi + header cookie/token, payload PUT, success PHP-style. |
| `internal/store` | User/settings, scope Dapodik, kredensial per scope, whitelist user-scope, job/event/item, counter SQL, module state, migrasi modul idempotent, prune. |
| `internal/secret` | Enkripsi/dekripsi AES-GCM, nonce acak, wrong key, ciphertext rusak. |
| `internal/auth` | Hash password, login + session, CSRF, tamper cookie, rate limit. |
| `internal/jobs` | Job sukses, cancel, penolakan secret di public params, event/item tersimpan. |
| `internal/app` | Login, dashboard, modul, admin, halaman scope Dapodik, RBAC operator, toggle modul dinamis tanpa restart, CSRF 403, redirect belum login, job spec terdaftar, CSV formula injection. |
| `internal/modules/nipd_filler` | Preview menyimpan semua siswa, apply hanya grup terpilih, fingerprint usang menolak tanpa PUT. |

Test tidak memakai data siswa asli. Fake Dapodik memakai nama `Ani/Budi/Cici` dan NISN dummy.

## 3. Fake Dapodik

Test `internal/modules/nipd_filler/apply_test.go` menjalankan `httptest.Server` yang meniru:

- `GET /rest/PesertaDidik` (paginasi sederhana, total);
- `PUT /rest/RegistrasiPesertaDidik/{id}`;
- respons sukses JSON.

Skenario:

1. Preview 3 siswa: satu X A kosong, satu X B terisi, satu tanpa rombel kosong.
2. Apply grup `X A` → hanya satu PUT, NIPD 101.
3. Preview fingerprint salah → apply gagal, tidak ada PUT.
4. Preview menyimpan 3 item, termasuk `(Tanpa Rombel)` dengan NIPD 102.

Untuk menambah test serupa:

```bash
go test ./internal/modules/nipd_filler -run 'TestRunApply|TestRunRenumber' -v
```

## 4. Uji manual lokal

### 4.1 Siapkan

```bash
go generate ./...
go build -o dapodik-tool ./cmd/dapodik-tool
./dapodik-tool create-admin --username admin --name Admin --password 'rahasia123'
APP_ADDR=127.0.0.1:18080 ./dapodik-tool serve
```

Buka `http://127.0.0.1:18080`.

### 4.2 Checklist fungsional

- [ ] Login admin berhasil; cookie sesi HttpOnly.
- [ ] Dashboard menampilkan modul Pengisian NIPD dan Cek Siswa.
- [ ] Admin bisa membuat akun operator.
- [ ] Operator login; menu admin tidak muncul.
- [ ] Operator membuka `/admin/users` → 403.
- [ ] POST API tanpa CSRF → 403.
- [ ] Logout menghapus sesi; halaman terlindungi redirect login.
- [ ] Rate limit: 6 login gagal → pesan rate limit.
- [ ] Halaman Pengaturan bisa disimpan.
- [ ] Halaman Modul menampilkan dua modul; toggle langsung berlaku tanpa restart.
- [ ] Admin → Dapodik (Scopes) dapat membuat scope A/B.
- [ ] Simpan cookie/token per scope; nilai tidak muncul lagi di HTML.
- [ ] Tes koneksi tiap scope berhasil dengan fake/real Dapodik.
- [ ] Operator di-assign ke scope A/B; dropdown scope muncul di NIPD/Cek Siswa.
- [ ] Preview/apply memakai kredensial scope yang dipilih.
- [ ] Scope yang tidak di-assign menghasilkan 403.
- [ ] Hapus scope → operator kehilangan akses dan kredensial terhapus.
- [ ] Halaman Akun bisa mengganti password.

### 4.3 Preview Dapodik (read-only)

Gunakan scope uji/staging dengan data aman:

- [ ] Preview menampilkan total siswa, NIPD tertinggi, dan jumlah kosong.
- [ ] Grup berurutan X→XI→XII, abjad natural, `(Tanpa Rombel)` terakhir.
- [ ] Rombel dengan 0 kosong tidak bisa dicentang.
- [ ] Hitung ulang mengubah rentang NIPD sesuai pilihan.
- [ ] Mode Renumber menampilkan operator `>`, `>=`, `<`, `<=`; batas/start divalidasi.
- [ ] Preview renumber menampilkan eligible/kept/skipped dan range baru untuk tiap operator.
- [ ] Konflik NIPD non-target memblokir apply tanpa PUT.
- [ ] Apply renumber sukses dan tombol Rollback CSV menghasilkan mapping lama→baru.
- [ ] Filter nama/NISN bekerja.
- [ ] Job log dan SSE tampil realtime.
- [ ] Halaman boleh ditinggal; job tetap selesai dan tampil di Riwayat.
- [ ] Refresh halaman job tidak menduplikasi log dan tidak reload berulang.
- [ ] Cancel bekerja pada job panjang.
- [ ] Jika 0 kosong, tombol Apply disabled dan ada pesan.

### 4.4 Cek NISN

- [ ] Beberapa NISN sekaligus (koma/spasi/baris baru).
- [ ] NISN ditemukan menampilkan rombel/NIPD.
- [ ] Siswa tanpa rombel menampilkan `(Tanpa Rombel)`.
- [ ] NIPD kosong menampilkan badge Kosong.
- [ ] NISN tidak ditemukan masuk daftar missing.
- [ ] Pencarian kedua memakai cache (`from_cache`).

### 4.5 Apply

Jangan apply ke produksi untuk pengujian. Gunakan fake Dapodik di test atau server staging.

- [ ] Modal meminta centang konfirmasi.
- [ ] Apply job menampilkan progress dan log per siswa.
- [ ] Download CSV dibuka di Excel (BOM, kolom benar).
- [ ] Download JSON valid.
- [ ] Item gagal tidak memakai ulang nomor.
- [ ] Setelah apply sukses, preview lama tidak bisa dipakai lagi karena fingerprint berubah.
- [ ] Kredensial scope tersimpan sebagai ciphertext; tes koneksi dan operator tanpa cookie berhasil.
- [ ] Operator yang tidak di-assign ke scope X mendapat 403 saat memakai scope X.

## 5. Uji UI/JS tanpa browser

Jika browser otomasi tidak tersedia:

```bash
node --check internal/web/static/js/app.js
node --check internal/modules/nipd_filler/static/nipd_filler.js
node --check internal/modules/student_lookup/static/student_lookup.js
```

Cocokkan ID elemen JS dengan template:

```bash
python3 - <<'PY'
import re, pathlib
checks = [
 ('internal/modules/nipd_filler/static/nipd_filler.js','internal/modules/nipd_filler/templates/index.html'),
 ('internal/modules/student_lookup/static/student_lookup.js','internal/modules/student_lookup/templates/index.html'),
 ('internal/web/static/js/app.js','internal/web/templates/job_detail.html'),
]
for js, html in checks:
    ids=set(re.findall(r'\$\("#([A-Za-z0-9_-]+)"\)', pathlib.Path(js).read_text()))
    body=pathlib.Path(html).read_text()
    print(js, [i for i in ids if f'id="{i}"' not in body])
PY
```

Ini tidak menggantikan uji browser, tetapi menangkap selektor salah dan sintaks rusak.

## 6. Uji keamanan manual

- [ ] `SESSION_COOKIE_SECURE=1` di production.
- [ ] Cookie manual tidak muncul plaintext di DB:
  ```bash
  strings data/app.db | grep -Ei 'PHPSESSID|cf_clearance|Bearer ' || echo bersih
  ```
  Kredensial admin memang tersimpan di tabel `credentials`, tetapi harus berupa ciphertext. Cek:
  ```bash
  python3 - <<'PY'
  import sqlite3
  db=sqlite3.connect('data/app.db')
  for key, cipher, _ in db.execute('select key,ciphertext,updated_at from credentials'):
      print(key, len(cipher), 'plaintext?' , 'PHPSESSID' in cipher or 'Bearer' in cipher)
  PY
  ```
- [ ] Cookie tidak muncul di log:
  ```bash
  journalctl -u dapodik-tool --no-pager | grep -Ei 'PHPSESSID|cf_clearance|Bearer ' || echo bersih
  ```
- [ ] Halaman/JSON memakai `Cache-Control: no-store`.
- [ ] CSP header ada.
- [ ] CSRF tanpa token ditolak.
- [ ] Operator tidak bisa job orang lain.
- [ ] CSV tidak mengeksekusi formula.
- [ ] SSRF: pastikan egress firewall membatasi host bila diperlukan.

## 7. CI contoh

```yaml
name: ci
on: [push, pull_request]
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.22.x'
      - run: go generate ./...
      - run: test -z "$(gofmt -l .)"
      - run: go vet ./...
      - run: go test ./...
      - run: CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o dapodik-tool ./cmd/dapodik-tool
```

Sesuaikan versi action dengan kebijakan repo.

## 8. Validasi OpenAPI

```bash
python3 - <<'PY'
import yaml
spec = yaml.safe_load(open('docs/openapi.yaml'))
assert spec['openapi'].startswith('3.')
assert spec['paths']
print('openapi OK:', len(spec['paths']), 'paths')
PY
```

Jika memakai tooling OpenAPI (Redocly/Swagger Editor), impor `docs/openapi.yaml` dan cek error referensi/skema.

## 9. Gap pengujian yang diketahui

- Tidak ada browser E2E otomatis (Playwright/agent-browser) di repo.
- Tidak ada test beban/SSE puluhan klien.
- Tidak ada test restore backup otomatis.
- Tidak ada test Cloudflare/`cf_clearance`.
- Test scope multi-sekolah terbatas pada store/provider; integrasi UI diuji manual.
- Apply nyata ke Dapodik tidak dijalankan otomatis; hanya fake server.
- Cakupan kode tidak diukur; fokus pada alur kritis.

Jika menambah modul, tambahkan test untuk:

- route dan role;
- job handler;
- validasi input;
- skema;
- tidak ada secret di public params/log.
