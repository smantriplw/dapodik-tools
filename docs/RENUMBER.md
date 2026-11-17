# Renumber NIPD (CLI Admin)

Dokumen ini untuk operasi CLI administratif mengganti NIPD siswa yang **sudah ada**. Untuk penggunaan harian, gunakan mode **Renumber NIPD lama** di halaman web Pengisian NIPD; alur dan pengamannya sama (preview, konflik, fingerprint, rollback CSV).

## Aturan yang diterapkan

- Hanya siswa dengan **NIPD numerik yang memenuhi operator** (`>`, `>=`, `<`, `<=`) terhadap threshold yang diproses.
- NIPD baru dialokasikan berurutan mulai `--start`.
- Urutan grup: **X A → … → XII Z** (natural), lalu `(Tanpa Rombel)` terakhir.
- Di dalam grup, siswa diurutkan berdasarkan nama.
- NIPD tepat sama dengan threshold mengikuti operator: `>`/`<` tidak memprosesnya, `>=`/`<=` memprosesnya.
- NIPD kosong dan non-numerik tidak diproses.
- Nomor baru tidak dipakai ulang jika PUT gagal.
- Konflik diperiksa sebelum apply: jika ada NIPD non-target di rentang baru, operasi dibatalkan.

## Perintah

Build:

```bash
go build -o renumber-preview ./cmd/renumber-preview
go build -o renumber-apply ./cmd/renumber-apply
```

Dry-run (wajib dulu):

```bash
./renumber-preview \
  --base-url https://dapodik.sekolah.sch.id \
  --sekolah-id UUID_SEKOLAH --npsn NPSN \
  --threshold 17978 --operator '>' --start 17979 \
  --report renumber-preview.csv
```

Apply (menulis):

```bash
./renumber-apply \
  --base-url https://dapodik.sekolah.sch.id \
  --sekolah-id UUID_SEKOLAH --npsn NPSN \
  --threshold 17978 --operator '>' --start 17979 --delay 250ms \
  --apply --yes \
  --rollback renumber-rollback.csv \
  --report renumber-report.csv
```

Cookie/token lewat env `DAPODIK_COOKIE`, `DAPODIK_TOKEN`, atau flag `--cookie`/`--token`.

## File output

- `renumber-rollback-*.csv` — mapping `old_nipd → new_nipd` sebelum apply; simpan untuk rollback manual.
- `renumber-report-*.csv` — status per siswa (`ok`/`failed`) setelah apply.
- Keduanya berisi PII; permission `0600`, jangan commit.

## Peringatan

- Selalu dry-run dan periksa operator, konflik, rentang sebelum apply.
- Backup database aplikasi sebelum operasi besar.
- Apply menimpa NIPD lama; pastikan operator/threshold/start benar.
- Jangan menjalankan dua apply bersamaan. Worker web bisa menulis Dapodik pada saat yang sama; koordinasikan.
- Operasi CLI tidak muncul di Riwayat Job web.
- Jika cookie kedaluwarsa, ambil cookie baru; jangan mengulang apply tanpa memeriksa report parsial.
- Untuk rollback, gunakan CSV rollback (old_nipd) sebagai sumber nilai lama; lakukan dry-run/verifikasi sebelum mengirim PUT balik.
