# PENTING: cara deploy raizencrm sudah berubah

**Sejak 21 Agustus 2026, `sudo systemctl restart raizencrm` TIDAK LAGI
BERPENGARUH APA-APA.**

Perintah itu masih "berhasil" dan tidak memberi pesan salah apa pun, tapi
kode barumu tidak akan naik. Ini jenis kegagalan yang paling membingungkan —
karena itu tolong baca bagian ini sampai habis.

## Apa yang berubah

| | Dulu | Sekarang |
|---|---|---|
| Yang melayani csraizen.digital | nginx + systemd `raizencrm` | container di Coolify |
| Port 80/443 dipegang | nginx | Traefik (`coolify-proxy`) |
| Cara deploy | `systemctl restart raizencrm` | `deploy-crm.sh` |
| Sertifikat TLS | certbot | Traefik, otomatis |

`systemd raizencrm` dan `nginx` sekarang **inactive dan disabled**. Keduanya
sengaja dibiarkan terpasang sebagai jalan pulang darurat, bukan karena masih
dipakai.

## Yang TIDAK berubah

- Kode tetap di `/opt/raizencrm`. Itu masih sumber kebenaran.
- Cara menyunting, `go build`, `npm run build` — semua sama.
- Basis data, sesi WhatsApp, media: isinya sama persis, sudah dipindahkan.

Yang berbeda hanya **cara menjalankannya**: sumber dibangun jadi image, lalu
container dibuat ulang.

## Alur deploy baru

```bash
# 1. sunting kode di /opt/raizencrm seperti biasa

# 2. kalau menyentuh frontend, bangun dulu seperti biasa
cd /opt/raizencrm/frontend && npm run build

# 3. deploy
/opt/raizencrm-deploy/deploy-crm.sh
```

Itu saja. Skripnya:

1. memeriksa `frontend/dist` tidak ketinggalan dari sumbernya (memperingatkan
   kalau ada `.tsx` yang lebih baru — ini pernah terjadi, lihat catatan bawah)
2. menyalin `/opt/raizencrm` jadi konteks build
3. memastikan sumber Go di konteks build identik dengan `/opt/raizencrm`
4. membangun kedua image
5. memastikan `index.html` di image sama dengan `dist` milikmu
6. membuat ulang container
7. **menunggu dan memastikan sesi WhatsApp tersambung lagi**
8. menunggu Traefik merutekan, lalu memastikan `csraizen.digital` menjawab 200

Downtime per deploy: **20–60 detik**. Sudah dua kali diuji, sesi WhatsApp
selamat keduanya.

### Kenapa sempat 404 saat deploy

Traefik hanya merutekan ke container yang sudah `healthy`, dan healthcheck
berjalan tiap 15 detik. Jadi beberapa puluh detik pertama setelah container
dibuat ulang, `csraizen.digital` menjawab 404. Itu normal dan sementara —
skripnya menunggu sampai 200 sebelum menyatakan selesai.

## Perintah yang berguna

```bash
# status sesi WhatsApp (lewat API, bukan menebak dari log)
BE=$(docker ps --format '{{.Names}}' | grep raizencrm-backend)
bash /opt/raizencrm-deploy/cek-sesi-wa.sh $BE

# log backend
docker logs $BE --tail 50 -f

# status container
docker ps --format '{{.Names}}\t{{.Status}}' | grep raizencrm
```

Butuh keanggotaan grup `docker`. Kalau `docker ps` ditolak, minta ditambahkan
lalu login ulang.

## Kalau harus kembali ke cara lama (darurat)

```bash
/opt/raizencrm-deploy/cutover-crm-batalkan.sh
```

Menghentikan container dan Traefik, lalu menghidupkan nginx dan systemd
`raizencrm` seperti sedia kala. Data asli di `/opt/raizencrm` tidak pernah
dipindah — hanya disalin — jadi selalu masih di tempatnya.

Catatan: data yang masuk SETELAH cutover ada di volume container, bukan di
`/opt/raizencrm`. Kalau membatalkan, data itu perlu disalin balik dulu.

## Dua hal yang perlu kamu putuskan

**1. Tiga panel frontend belum terbit.**

Pada 21 Agustus 12:04 kamu menambahkan `LearningPanel.tsx`, `MetaCapiPanel.tsx`,
dan `MediaAssetsPanel.tsx`, lalu membangun ulang binary pada 12:06 — tapi
`frontend/dist` terakhir dibangun 11:11, sebelum ketiganya ada.

Jadi **backend sudah punya 22 endpoint barunya** (learning, meta, ai-status,
media-assets) sementara tampilannya belum. Saat cutover, `dist` sengaja
disalin apa adanya supaya perpindahan server tidak berubah jadi rilis fitur
yang tidak diminta siapa pun.

Kalau ketiga panel itu memang siap terbit:

```bash
cd /opt/raizencrm/frontend && npm run build
/opt/raizencrm-deploy/deploy-crm.sh
```

**2. Repo GitHub tertinggal jauh.**

Repo publik raizencrm tertinggal ~2 minggu dari kode yang berjalan (32 berkas
belum di-commit). Container dibangun dari `/opt/raizencrm`, bukan dari repo,
sesuai permintaan pemilik. Selama repo belum disusulkan, `/opt/raizencrm`
adalah satu-satunya salinan kode terbaru — dan itu tidak punya riwayat.

## Satu koreksi teknis yang perlu kamu tahu

Sesi WhatsApp yang hidup **ada di `wa-assistant.db`, bukan di
`data/wa-session-agent-2.db`**:

```go
main.go:53      InitWA(config.Env("DB_PATH", "./wa-assistant.db"))
wa.go:216       agent 1 memakai legacyDBPath (= DB_PATH)
```

`wa-session-agent-2.db` tidak punya baris `whatsmeow_device` sama sekali —
itu sebabnya agen 2 selalu `disconnected`/`qr`. Jadi berkas yang benar-benar
tidak boleh hilang adalah **`wa-assistant.db`**.
