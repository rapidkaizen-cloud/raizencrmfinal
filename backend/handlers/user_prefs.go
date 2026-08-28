package handlers

import (
	"log"

	"wa-assistant/backend/database"
	"wa-assistant/backend/models"

	"github.com/gin-gonic/gin"
)

// SaveBlastDelay menyimpan default jeda blast milik user yang sedang login.
// Setelan ini per AKUN, bukan per nomor CS: satu CS bisa memegang beberapa nomor
// dan ritme kirimnya mengikuti orangnya. Karena itu endpoint ini terbuka untuk
// semua role — tiap orang mengatur ritmenya sendiri, tidak bisa mengubah milik
// orang lain (id user diambil dari token, bukan dari body).
func SaveBlastDelay(c *gin.Context) {
	var req struct {
		MinDelay     *int `json:"min_delay"`
		MaxDelay     *int `json:"max_delay"`
		RestEvery    *int `json:"rest_every"`
		RestDuration *int `json:"rest_duration"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "Format data tidak valid"})
		return
	}

	var user models.User
	if database.DB.First(&user, c.GetUint("user_id")).Error != nil {
		c.JSON(404, gin.H{"error": "User tidak ditemukan"})
		return
	}

	// Field yang tidak dikirim tetap memakai nilai lama (update parsial).
	minDelay, maxDelay, restEvery, restDuration := user.BlastDelay()
	if req.MinDelay != nil {
		minDelay = *req.MinDelay
	}
	if req.MaxDelay != nil {
		maxDelay = *req.MaxDelay
	}
	if req.RestEvery != nil {
		restEvery = *req.RestEvery
	}
	if req.RestDuration != nil {
		restDuration = *req.RestDuration
	}

	if msg := models.ValidateBlastDelay(minDelay, maxDelay, restEvery, restDuration); msg != "" {
		c.JSON(400, gin.H{"error": msg})
		return
	}

	user.BlastMinDelay, user.BlastMaxDelay = minDelay, maxDelay
	user.BlastRestEvery, user.BlastRestDuration = restEvery, restDuration
	// Select eksplisit: jangan sampai Save() ikut menulis ulang kolom sensitif
	// (password, token reset, flag aktif) hanya karena user disimpan utuh.
	if err := database.DB.Model(&user).
		Select("blast_min_delay", "blast_max_delay", "blast_rest_every", "blast_rest_duration").
		Updates(map[string]any{
			"blast_min_delay":     minDelay,
			"blast_max_delay":     maxDelay,
			"blast_rest_every":    restEvery,
			"blast_rest_duration": restDuration,
		}).Error; err != nil {
		log.Printf("Simpan jeda blast user %d gagal: %v", user.ID, err)
		c.JSON(500, gin.H{"error": "Gagal menyimpan pengaturan jeda"})
		return
	}

	c.JSON(200, gin.H{"data": gin.H{
		"min_delay":     minDelay,
		"max_delay":     maxDelay,
		"rest_every":    restEvery,
		"rest_duration": restDuration,
	}})
}
