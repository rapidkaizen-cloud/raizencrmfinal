# Perubahan raizencrm — 21 Agustus 2026

Ringkas. Semua yang berubah di server untuk CRM, dan apa artinya buat kamu.

## Yang berubah paling penting

| | Sebelum | Sesudah |
|---|---|---|
| Yang melayani csraizen.digital | nginx + systemd `raizencrm` | container di Coolify |
| Port 80/443 | nginx | Traefik (`coolify-proxy`) |
| Cara deploy | `systemctl restart raizencrm` | `/opt/raizencrm-deploy/deploy-crm.sh` |
| Sertifikat TLS | certbot (manual) | Traefik (otomatis) |
| Cadangan | tidak ada | harian 03:00 WIB, teruji pulih |

`systemd raizencrm` dan `nginx` sekarang **inactive + disabled**, sengaja
dibiarkan terpasang sebagai jalan pulang darurat.

## 1. Cutover ke container

- Downtime **69 detik**
- Data dipindah: `wa-assistant.db` (5,9 MB) + `wa-session-agent-2.db` + media
- Sesi WhatsApp **selamat** — perangkat `62895627101264:2@s.whatsapp.net`
  diverifikasi identik sebelum dan sesudah
- Berkas asli di `/opt/raizencrm` **tidak dipindah, hanya disalin**

Image dibangun ulang dari sumber terkini karena snapshot lama tertinggal:
15 berkas berbeda, 111 baris, 6 berkas `.go` baru, dan route bertambah
**171 → 193** (learning, meta, ai-status, media-assets).

## 2. Frontend sengaja TIDAK dibangun ulang

Urutan waktu di produksi:

```
11:11  frontend/dist dibangun        ← yang dilihat user
12:04  LearningPanel, MetaCapiPanel, MediaAssetsPanel dibuat
12:06  binary backend dibangun ulang
```

Backend sudah punya 22 endpoint barunya, tampilannya belum. `dist` disalin apa
adanya supaya perpindahan server tidak berubah jadi rilis fitur yang tidak
diminta siapa pun.

**Perlu keputusanmu.** Kalau ketiga panel itu siap terbit:
`cd /opt/raizencrm/frontend && npm run build` lalu jalankan `deploy-crm.sh`.

## 3. Sertifikat pindah ke Traefik

Setelah cutover tidak ada yang memperpanjang sertifikat: certbot memakai
`authenticator=nginx` (nginx sudah mati), sementara Traefik menyajikan
sertifikat statis sehingga ACME tidak pernah jalan.

Sekarang Traefik yang memilikinya:
`06C6A5F3…` (statis) → `063D5657…` (ACME), berlaku s/d 19 Nov 2026, otomatis
diperpanjang. Renewal certbot dinonaktifkan; berkas lamanya tetap disimpan.

## 4. Cadangan harian

`/opt/raizencrm-deploy/cadangkan-crm.sh` → `/var/backups/raizencrm`, 03:00 WIB,
simpan 14 hari.

Memakai `sqlite .backup`, bukan `cp` — aman pada basis data yang sedang dipakai,
layanan tidak perlu berhenti. Tiap salinan diverifikasi (`integrity_check` +
perangkat WhatsApp harus ada) dan sudah **diuji pulih**: hasilnya identik dengan
yang berjalan (perangkat sama, 33 kontak, 66 tabel).

## 5. Perkakas pindah ke lokasi bersama

Dari `/home/afiq/…` (tidak bisa kamu baca) ke **`/opt/raizencrm-deploy/`**.
`/home/afiq` sengaja tetap tertutup karena memuat token dan password.

```
/opt/raizencrm-deploy/
  deploy-crm.sh              deploy
  cek-sesi-wa.sh             status WhatsApp lewat API
  cadangkan-crm.sh           cadangan (dipanggil timer)
  cutover-crm-batalkan.sh    kembali ke nginx + systemd, darurat
  BACA-DULU-DEPLOY-BERUBAH.md
```

User `deploy` ditambahkan ke grup `docker` — bukan peningkatan hak, karena
sudah punya `sudo ALL:ALL`.

## Yang perlu kamu tahu saat deploy

- Downtime 20–60 detik per deploy
- **404 sesaat itu normal**: Traefik hanya merutekan ke container `healthy`,
  dan healthcheck jalan tiap 15 detik. Skrip menunggu sampai 200.
- Sesi WhatsApp diperiksa otomatis tiap deploy. Sudah 3 kali deploy, selamat
  ketiganya.

## Koreksi teknis yang penting

Sesi WhatsApp yang hidup ada di **`wa-assistant.db`**, bukan
`data/wa-session-agent-2.db`:

```go
main.go:53   InitWA(config.Env("DB_PATH", "./wa-assistant.db"))
wa.go:216    agent 1 memakai legacyDBPath (= DB_PATH)
```

`wa-session-agent-2.db` tidak punya baris `whatsmeow_device` sama sekali — itu
sebabnya agen 2 selalu `disconnected`/`qr`. Berkas yang tidak boleh hilang
adalah `wa-assistant.db`.

## Masih menggantung

- Tiga panel frontend menunggu keputusanmu (bagian 2)
- Repo GitHub tertinggal ~2 minggu dari kode yang berjalan; container dibangun
  dari `/opt/raizencrm`, bukan dari repo. Selama belum disusulkan,
  `/opt/raizencrm` satu-satunya salinan kode terbaru dan tidak punya riwayat.
