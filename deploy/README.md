# Deploy Assets

Folder ini berisi contoh konfigurasi produksi. Panduan lengkap: [docs/DEPLOYMENT.md](../docs/DEPLOYMENT.md).

| File | Fungsi |
|---|---|
| `env.example` | Template environment systemd. Salin ke `/etc/dapodik-tool/env`. |
| `dapodik-tool.service` | Unit systemd, user non-root, restart otomatis. |
| `Caddyfile` | Reverse proxy HTTPS + SSE unbuffered (disarankan). |
| `nginx.conf.example` | Alternatif Nginx + Let's Encrypt + SSE. |

## Langkah singkat

```bash
# build
CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o dapodik-tool ./cmd/dapodik-tool

# user & direktori
sudo useradd --system --home /opt/dapodik-tool --shell /usr/sbin/nologin dapodik
sudo mkdir -p /opt/dapodik-tool/data /opt/dapodik-tool/backup /etc/dapodik-tool
sudo cp dapodik-tool /opt/dapodik-tool/
sudo chown -R dapodik:dapodik /opt/dapodik-tool

# env
sudo cp deploy/env.example /etc/dapodik-tool/env
sudo chmod 600 /etc/dapodik-tool/env
sudo editor /etc/dapodik-tool/env   # isi SESSION_SECRET, domain, default

# admin
sudo -u dapodik /opt/dapodik-tool/dapodik-tool create-admin

# service
sudo cp deploy/dapodik-tool.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now dapodik-tool

# proxy (contoh Caddy)
sudo cp deploy/Caddyfile /etc/caddy/Caddyfile
sudo editor /etc/caddy/Caddyfile
sudo systemctl reload caddy
```

## Penting

- Jangan membuka port aplikasi (`APP_ADDR`, default `127.0.0.1:8000`) ke internet.
- `SESSION_COOKIE_SECURE=1` dan `TRUST_PROXY=1` di belakang HTTPS/proxy.
- Isi `CREDENTIALS_KEY` (32+ karakter) jika ingin kunci enkripsi kredensial terpisah dari `SESSION_SECRET`; simpan terpisah dari backup.
- Setelah login admin, atur kredensial di **Administrasi → Dapodik** dan jalankan **Tes koneksi**.
- SSE membutuhkan `flush_interval -1` (Caddy) atau `proxy_buffering off` (Nginx).
- File env dan backup berisi rahasia/PII; jangan commit.
- Backup dan prune tidak otomatis; jadwalkan dari cron/systemd.
