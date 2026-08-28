package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"wa-assistant/backend/database"
	"wa-assistant/backend/models"

	"github.com/gin-gonic/gin"
)

// putBlastDelay memanggil PUT /blast-delay sebagai user tertentu.
func putBlastDelay(userID uint, body string) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.PUT("/blast-delay", func(c *gin.Context) {
		c.Set("user_id", userID)
		c.Set("is_super_admin", false)
		c.Set("features", []string{})
		c.Next()
	}, SaveBlastDelay)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/blast-delay", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	return w
}

// seedBlastUser membuat satu user CS untuk dipakai test jeda blast.
func seedBlastUser(t *testing.T) models.User {
	t.Helper()
	u := models.User{
		Username: "cs1", Name: "CS Satu", Role: models.RoleCS, Active: true,
		TenantID: func() *uint { v := testUserTenant; return &v }(),
	}
	if err := database.DB.Create(&u).Error; err != nil {
		t.Fatalf("gagal bikin user: %v", err)
	}
	return u
}

// Nilai wajar tersimpan dan dibalas kembali ke klien.
func TestSaveBlastDelayTersimpan(t *testing.T) {
	setupUserTestDB(t)
	u := seedBlastUser(t)

	w := putBlastDelay(u.ID, `{"min_delay":15,"max_delay":45,"rest_every":40,"rest_duration":120}`)
	if w.Code != 200 {
		t.Fatalf("harus 200, dapat %d — %s", w.Code, w.Body.String())
	}

	var resp struct {
		Data struct {
			MinDelay     int `json:"min_delay"`
			MaxDelay     int `json:"max_delay"`
			RestEvery    int `json:"rest_every"`
			RestDuration int `json:"rest_duration"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("balasan bukan JSON valid: %v", err)
	}
	if resp.Data.MinDelay != 15 || resp.Data.MaxDelay != 45 || resp.Data.RestEvery != 40 || resp.Data.RestDuration != 120 {
		t.Fatalf("balasan tidak sesuai kiriman: %+v", resp.Data)
	}

	var after models.User
	if err := database.DB.First(&after, u.ID).Error; err != nil {
		t.Fatalf("gagal baca user: %v", err)
	}
	if after.BlastMinDelay != 15 || after.BlastMaxDelay != 45 || after.BlastRestEvery != 40 || after.BlastRestDuration != 120 {
		t.Fatalf("kolom di DB tidak ikut tersimpan: %+v", after)
	}
}

// rest_every = 0 berarti "istirahat berkala dimatikan" dan harus BOLEH tersimpan.
// Ini gampang rusak kalau ada kode yang memperlakukan 0 sebagai "kosong".
func TestSaveBlastDelayRestEveryNolBoleh(t *testing.T) {
	setupUserTestDB(t)
	u := seedBlastUser(t)

	if w := putBlastDelay(u.ID, `{"min_delay":10,"max_delay":20,"rest_every":0,"rest_duration":90}`); w.Code != 200 {
		t.Fatalf("rest_every=0 harus diterima, dapat %d — %s", w.Code, w.Body.String())
	}
	var after models.User
	database.DB.First(&after, u.ID)
	if after.BlastRestEvery != 0 {
		t.Fatalf("rest_every=0 harus tersimpan apa adanya, dapat %d", after.BlastRestEvery)
	}
}

// Field yang tidak dikirim tidak boleh mereset nilai yang sudah tersimpan.
func TestSaveBlastDelayUpdateParsial(t *testing.T) {
	setupUserTestDB(t)
	u := seedBlastUser(t)

	if w := putBlastDelay(u.ID, `{"min_delay":12,"max_delay":40,"rest_every":30,"rest_duration":100}`); w.Code != 200 {
		t.Fatalf("simpan awal gagal: %d", w.Code)
	}
	// Hanya mengubah max_delay.
	if w := putBlastDelay(u.ID, `{"max_delay":50}`); w.Code != 200 {
		t.Fatalf("update parsial gagal: %d — %s", w.Code, w.Body.String())
	}

	var after models.User
	database.DB.First(&after, u.ID)
	if after.BlastMaxDelay != 50 {
		t.Fatalf("max_delay harus jadi 50, dapat %d", after.BlastMaxDelay)
	}
	if after.BlastMinDelay != 12 || after.BlastRestEvery != 30 || after.BlastRestDuration != 100 {
		t.Fatalf("field lain tidak boleh berubah: %+v", after)
	}
}

// Nilai tidak masuk akal ditolak 400 dan TIDAK mengubah apa pun di DB.
func TestSaveBlastDelayTolakNilaiNgawur(t *testing.T) {
	kasus := []struct {
		nama string
		body string
	}{
		{"min di bawah 1", `{"min_delay":0,"max_delay":30,"rest_every":25,"rest_duration":90}`},
		{"max lebih kecil dari min", `{"min_delay":30,"max_delay":10,"rest_every":25,"rest_duration":90}`},
		{"max kelewat besar", `{"min_delay":10,"max_delay":99999,"rest_every":25,"rest_duration":90}`},
		{"rest_every negatif", `{"min_delay":10,"max_delay":30,"rest_every":-5,"rest_duration":90}`},
		{"istirahat aktif tapi durasinya 0", `{"min_delay":10,"max_delay":30,"rest_every":25,"rest_duration":0}`},
	}
	for _, k := range kasus {
		t.Run(k.nama, func(t *testing.T) {
			setupUserTestDB(t)
			u := seedBlastUser(t)
			// Baca ulang: kolom jeda terisi oleh `gorm:"default:..."` saat INSERT,
			// jadi patokannya harus nilai nyata di DB, bukan nol.
			var sebelum models.User
			database.DB.First(&sebelum, u.ID)

			w := putBlastDelay(u.ID, k.body)
			if w.Code != 400 {
				t.Fatalf("harus ditolak 400, dapat %d — %s", w.Code, w.Body.String())
			}
			var after models.User
			database.DB.First(&after, u.ID)
			if after.BlastMinDelay != sebelum.BlastMinDelay || after.BlastMaxDelay != sebelum.BlastMaxDelay ||
				after.BlastRestEvery != sebelum.BlastRestEvery || after.BlastRestDuration != sebelum.BlastRestDuration {
				t.Fatalf("kiriman ngawur tidak boleh menyentuh DB: sebelum=%d/%d/%d/%d sesudah=%d/%d/%d/%d",
					sebelum.BlastMinDelay, sebelum.BlastMaxDelay, sebelum.BlastRestEvery, sebelum.BlastRestDuration,
					after.BlastMinDelay, after.BlastMaxDelay, after.BlastRestEvery, after.BlastRestDuration)
			}
		})
	}
}

// User lama (kolom masih 0 karena dibuat sebelum kolom ini ada) harus jatuh
// ke nilai bawaan, bukan mengirim 0 detik ke form blast.
func TestBlastDelayUserLamaPakaiNilaiBawaan(t *testing.T) {
	var lama models.User // semua kolom jeda = 0
	minDelay, maxDelay, restEvery, restDuration := lama.BlastDelay()
	if minDelay != models.DefaultBlastMinDelay {
		t.Fatalf("min harus jatuh ke bawaan %d, dapat %d", models.DefaultBlastMinDelay, minDelay)
	}
	if maxDelay < minDelay {
		t.Fatalf("max (%d) tidak boleh lebih kecil dari min (%d)", maxDelay, minDelay)
	}
	if restDuration != models.DefaultBlastRestDuration {
		t.Fatalf("lama istirahat harus jatuh ke bawaan %d, dapat %d", models.DefaultBlastRestDuration, restDuration)
	}
	// restEvery=0 sah (istirahat mati), jadi TIDAK boleh dipulihkan jadi 25.
	if restEvery != 0 {
		t.Fatalf("rest_every=0 harus dibiarkan 0, dapat %d", restEvery)
	}
}
