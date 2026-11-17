# Panduan Pengguna (Operator)

Panduan ini untuk operator yang memakai SMANTI Tools untuk melihat dan mengisi NIPD serta mencari data siswa. Akun dan akses scope dibuat oleh admin sekolah; tidak ada pendaftaran mandiri.

## 1. Login

1. Buka alamat tool dari admin, mis. `https://nipd.sekolah.sch.id`.
2. Masukkan `username` dan `password`.
3. Klik **Masuk**.

Catatan:

- Username tidak membedakan huruf besar/kecil.
- Setelah 5 percobaan gagal dalam 15 menit, login dikunci sementara untuk kombinasi IP + username tersebut.
- Sesi berlaku 12 jam. Setelah itu login ulang.
- Jika lupa password, minta admin mereset lewat halaman **Administrasi → Pengguna**.
- Jangan memakai akun bersama. Semua job tercatat atas nama akun yang dipakai.

## 2. Mengenal halaman

- **Dashboard** — daftar modul dan job terakhir.
- **Pengisian NIPD** — preview dan pengisian NIPD.
- **Cek Siswa** — pencarian NISN.
- **Riwayat Job** — semua job milik Anda (admin melihat semua job).
- **Ganti Password** — ubah password sendiri.

## 3. Scope Dapodik

Admin dapat membuat beberapa scope Dapodik (mis. **Dapodik A**, **Dapodik B**, atau nama sekolah). Setiap scope punya URL, sekolah, cookie, dan token sendiri. Anda hanya melihat scope yang di-assign admin ke akun Anda.

- **Anda tidak perlu menempel cookie.** Kredensial dikelola admin per scope.
- Di modul NIPD dan Cek Siswa, pilih **Scope Dapodik** yang ingin dipakai.
- Pastikan scope yang dipilih sesuai sekolah/data yang ingin dilihat. Scope berbeda bisa berisi data berbeda.
- Jika dropdown kosong atau scope yang dituju tidak muncul, hubungi admin untuk assign scope.
- Jika muncul **“Scope … belum memiliki cookie”** atau **“Session telah habis”**, hubungi admin untuk memperbarui cookie scope tersebut.
- Jika admin mengubah URL/sekolah/NPSN scope setelah preview, apply akan menolak dengan pesan **“Pengaturan scope berubah sejak preview”**. Muat preview ulang.

## 4. Alur Pengisian NIPD

Halaman ini memiliki dua mode:

- **Isi NIPD kosong** — default; hanya mengisi siswa yang belum punya NIPD. NIPD lama tidak disentuh.
- **Renumber NIPD lama** — mengganti NIPD yang sudah ada sesuai **operator pembanding** (`>`, `>=`, `<`, `<=`) terhadap batas. Gunakan hanya atas instruksi admin.

### Langkah 1 — Pilih mode, scope, dan opsi

- **Mode** — pilih **Isi NIPD kosong** atau **Renumber NIPD lama**.
- **Scope Dapodik** — pilih scope yang diizinkan.
- **Limit per halaman** — default 20000. Biarkan default kecuali admin menyarankan lain.
- **Jeda request** — default 0.3 detik. Jangan terlalu kecil saat mengisi banyak siswa.
- **Opsi lanjutan → NIPD awal** — biarkan kosong agar tool memakai NIPD tertinggi + 1. Isi hanya jika admin meminta nomor awal tertentu.

Klik **Muat data & preview**. Proses ini **read-only**: tidak ada data yang diubah.

### Langkah 2 — Periksa preview

Preview menampilkan:

- Total siswa, jumlah NIPD kosong (mode isi) atau jumlah siswa yang akan diubah (mode renumber), NIPD tertinggi, dan NIPD awal.
- Daftar rombel berurutan dari kelas bawah ke atas: `X`, `XI`, `XII`, lalu `(Tanpa Rombel)`.
- Di dalam tiap tingkat, rombel diurutkan abjad/natural, mis. `X A`, `X B`, ... `X O`, `XI A1`, dst.
- Jumlah siswa dan jumlah NIPD kosong/siswa yang akan diubah per rombel.

Yang perlu dilakukan:

1. Centang rombel yang ingin diproses. Rombel dengan 0 NIPD kosong/siswa eligible otomatis tidak bisa dicentang.
2. Klik **Hitung ulang** setelah mengubah pilihan. Nomor NIPD dihitung ulang hanya untuk rombel terpilih.
3. Gunakan kotak **Cari nama atau NISN** untuk memeriksa siswa tertentu.
4. Perhatikan badge **`(Tanpa Rombel)`**: siswa ini belum punya rombel, tetapi tetap bisa diisi NIPD dan diproses terakhir.
5. Pastikan rentang NIPD yang ditampilkan masuk akal.

Jika tertulis **Semua siswa sudah memiliki NIPD**, tidak ada yang perlu dilakukan. Tombol apply dinonaktifkan.

### Langkah 3 — Terapkan

1. Klik **Terapkan NIPD**.
2. Modal menampilkan jumlah siswa, jumlah rombel, dan rentang NIPD.
3. Centang **Saya sudah memeriksa preview**.
4. Klik **Terapkan sekarang**.

Saat job berjalan:

- Progres dan log tampil realtime.
- Halaman boleh ditinggal; job tetap berjalan di server.
- Tombol **Batalkan** menghentikan job sebelum PUT berikutnya. Satu PUT yang sedang berjalan bisa tetap selesai.
- Jika server restart, job ditandai gagal; ulangi dari preview.

### Setelah selesai

- Ringkasan menunjukkan berhasil, gagal, dilewati, dan NIPD terakhir.
- Unduh **CSV** untuk Excel atau **JSON** untuk arsip.
- Buka **Riwayat Job** untuk melihat ulang log dan item.
- Jika ada item gagal, baca pesan per baris. Nomor NIPD yang gagal **tidak dipakai ulang** untuk siswa lain.

### Pengaman penting

- Apply akan menolak jika data Dapodik berubah sejak preview. Muat preview ulang.
- Apply juga menolak jika pengaturan scope berubah sejak preview.
- Nomor NIPD tidak pernah dipakai ulang setelah gagal, untuk menghindari duplikasi.
- NIPD tidak boleh negatif.
- Jika data belum punya NIPD numerik sama sekali, isi **NIPD awal** di opsi lanjutan.

### Mode Renumber NIPD lama

Gunakan mode ini hanya jika admin meminta penggantian NIPD yang sudah ada.

1. Pilih mode **Renumber NIPD lama**.
2. Pilih **Operator NIPD** (`>`, `>=`, `<`, `<=`), isi **Batas NIPD**, lalu isi **NIPD awal baru**.
   - Hanya NIPD numerik yang memenuhi operator yang diproses. Contoh: `>` tidak mengikutkan NIPD yang tepat sama dengan batas; `>=` mengikutkannya.
   - NIPD awal baru tidak harus di atas batas. Konflik NIPD tetap dicek otomatis.
   - NIPD baru dialokasikan berurutan; urutan grup X A → XII Z, lalu `(Tanpa Rombel)`.
   - Mengubah operator/batas/start setelah preview memerlukan **Muat data & preview** ulang.
3. Klik **Muat data & preview**. Preview tetap read-only.
4. Periksa:
   - jumlah siswa yang akan diubah dan rentang NIPD baru;
   - jumlah siswa yang dipertahankan/dilewati;
   - peringatan **konflik**. Konflik berarti ada NIPD milik siswa non-target yang berada di rentang baru. Apply akan diblokir; ubah batas/start atau pilihan rombel lalu muat preview ulang.
5. Centang rombel, klik **Hitung ulang**, lalu **Terapkan NIPD**. Konfirmasi tetap wajib.
6. Setelah selesai, unduh **CSV** dan **Unduh rollback**. Rollback CSV berisi `registrasi_id`, NIPD lama, dan NIPD baru untuk siswa yang berhasil diubah.
7. Simpan rollback CSV di tempat aman. Tool **tidak** melakukan rollback otomatis.

Peringatan:

- Renumber menimpa NIPD lama. Pastikan operator, batas, NIPD awal, dan pilihan rombel sudah benar.
- Jika data Dapodik berubah sejak preview, apply ditolak tanpa menulis.
- Jika satu PUT gagal, nomor berikutnya tidak dipakai ulang; periksa report sebelum mengulang.
- Jangan menjalankan apply lain (termasuk dari CLI) saat job renumber berjalan.

## 5. Cek Siswa

1. Buka modul **Cek Siswa**.
2. Pilih **Scope Dapodik**.
3. Masukkan satu atau banyak NISN, dipisah koma, spasi, titik koma, atau baris baru. Maksimal 100 NISN per pencarian.
4. Klik **Cari siswa**.

Hasil menampilkan nama, NISN, rombel, tingkat, dan NIPD. Siswa tanpa rombel diberi badge **`(Tanpa Rombel)`**. NIPD kosong diberi badge **Kosong**. NISN yang tidak ditemukan ditampilkan terpisah.

Pencarian memakai cache memori 5 menit per scope agar pencarian berikutnya cepat. Cache tidak ditulis ke database.

## 6. Status dan pesan yang sering muncul

| Pesan / status | Arti | Tindakan |
|---|---|---|
| Tidak ada scope di dropdown | Admin belum meng-assign scope | Hubungi admin |
| `Scope ... belum memiliki cookie` | Admin belum mengisi cookie scope | Hubungi admin |
| `Session telah habis` | Cookie scope kedaluwarsa | Hubungi admin untuk memperbarui cookie scope |
| `HTTP 401/403` dari Dapodik | Sesi Dapodik ditolak | Hubungi admin |
| `Pengaturan scope berubah sejak preview` | URL/sekolah/NPSN scope berubah | Muat preview ulang |
| `Tidak ada NIPD numerik` | Sekolah belum punya NIPD angka | Isi NIPD awal di opsi lanjutan |
| `Data berubah sejak preview` | Ada perubahan data setelah preview | Muat preview ulang |
| `Rombel tidak ada di preview` | Pilihan rombel sudah berubah | Muat preview ulang |
| `Preview belum selesai` | Apply dikirim sebelum preview selesai | Tunggu preview selesai |
| `CSRF token tidak valid` | Halaman/sesi kedaluwarsa | Muat ulang halaman, login ulang bila perlu |
| `Terlalu banyak percobaan` | Rate limit login | Tunggu 15 menit |
| Job `queued` lama | Antrean menunggu job lain | Tunggu; jika macet, hubungi admin |
| Job `failed` | Job berhenti | Baca log; jika sesi habis, hubungi admin |
| Halaman modul 404 | Modul dinonaktifkan admin | Hubungi admin atau tunggu diaktifkan |
| `Scope Dapodik tidak diizinkan` | Scope tidak di-assign ke akun | Pilih scope lain atau hubungi admin |

## 7. Kebiasaan aman

- Selalu preview sebelum apply.
- Jangan mengisi NIPD saat jam sibuk Dapodik atau saat operator lain sedang mengubah data.
- Jangan memakai akun operator lain.
- Unduh laporan jika diperlukan, lalu simpan di tempat yang dilindungi karena berisi nama/NISN.
- Laporkan ke admin jika melihat data siswa yang tidak semestinya, job milik orang lain, atau scope yang tidak seharusnya bisa diakses.
- Anda tidak perlu dan tidak seharusnya menerima cookie Dapodik dari siapa pun; cookie dikelola admin per scope.

## 8. Batasan yang perlu diketahui

- Setiap scope = satu konfigurasi Dapodik (biasanya satu sekolah).
- Akses scope ditentukan admin; operator tidak bisa menambah/mengubah scope.
- Tool tidak mengubah rombel atau data lain; hanya NIPD pada RegistrasiPesertaDidik.
- Tool tidak mengirim email/WhatsApp.
- Tool tidak menyediakan 2FA.
- Toggle modul oleh admin langsung berlaku; halaman modul bisa tiba-tiba 404.
- Tool membutuhkan koneksi ke server Dapodik dan (untuk font) Google Fonts. Jika offline, font memakai fallback sistem.
- Browser harus mendukung EventSource/SSE dan elemen `<dialog>` (Chrome/Edge/Firefox/Safari modern).
