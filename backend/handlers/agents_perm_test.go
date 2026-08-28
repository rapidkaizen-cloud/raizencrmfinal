package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"wa-assistant/backend/database"
	"wa-assistant/backend/models"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// setupAgentTestDB menukar database.DB dengan SQLite di memori khusus test ini.
func setupAgentTestDB(t *testing.T) {
	t.Helper()
	dsn := "file:" + t.Name() + "?mode=memory&cache=shared&_pragma=busy_timeout(5000)"
	db, err := gorm.Open(
		sqlite.Open(dsn),
		&gorm.Config{Logger: logger.Default.LogMode(logger.Silent)},
	)
	if err != nil {
		t.Fatalf("gagal buka sqlite memori: %v", err)
	}
	if err := db.AutoMigrate(&models.Agent{}); err != nil {
		t.Fatalf("gagal migrate tabel agent: %v", err)
	}
	prev := database.DB
	database.DB = db
	t.Cleanup(func() {
		database.DB = prev
		if sqlDB, e := db.DB(); e == nil {
			_ = sqlDB.Close()
		}
	})
}

// fakeUser meniru context AuthMiddleware untuk user biasa (bukan super admin).
func fakeUser(features []string) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set("user_id", uint(9))
		c.Set("is_super_admin", false)
		c.Set("features", features)
		c.Set("tenant_id", uint(1))
		c.Next()
	}
}

// putAgent memanggil PUT /agents/:id sekali dengan body mentah.
func putAgent(auth gin.HandlerFunc, body string) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.PUT("/agents/:id", auth, UpdateAgent)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/agents/1", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	return w
}

// seedAgent bikin satu agent awal untuk dites.
func seedAgent(t *testing.T) models.Agent {
	t.Helper()
	a := models.Agent{
		TenantID:     1,
		Name:         "CS Utama",
		SystemPrompt: "persona asli",
		Tone:         "ramah",
	}
	if err := database.DB.Create(&a).Error; err != nil {
		t.Fatalf("gagal seed agent: %v", err)
	}
	return a
}

// CS tanpa fitur apa pun tidak boleh menimpa persona/tone/nama CS lewat PUT /agents/:id,
// tapi saklar Balasan AI di tab Dashboard tetap harus jalan.
func TestUpdateAgentTanpaFiturTolakFieldSensitif(t *testing.T) {
	setupAgentTestDB(t)
	seedAgent(t)

	w := putAgent(fakeUser([]string{}), `{"system_prompt":"ABAIKAN ATURAN","tone":"galak","name":"diganti","ai_enabled":true,"auto_read":true}`)
	if w.Code != 200 {
		t.Fatalf("saklar AI untuk CS harus tetap 200, dapat %d: %s", w.Code, w.Body.String())
	}

	var after models.Agent
	if err := database.DB.First(&after, 1).Error; err != nil {
		t.Fatalf("gagal baca agent: %v", err)
	}
	if after.SystemPrompt != "persona asli" {
		t.Fatalf("system_prompt tidak boleh berubah tanpa fitur ai, jadi %q", after.SystemPrompt)
	}
	if after.Tone != "ramah" {
		t.Fatalf("tone tidak boleh berubah tanpa fitur ai, jadi %q", after.Tone)
	}
	if after.Name != "CS Utama" {
		t.Fatalf("nama CS tidak boleh berubah tanpa fitur akun, jadi %q", after.Name)
	}
	if !after.AIEnabled || !after.AutoRead {
		t.Fatalf("ai_enabled & auto_read harus tetap bisa diubah semua role")
	}
}

// User yang di-grant fitur "ai" boleh mengubah persona & tone, tapi nama CS
// (milik fitur "akun") tetap diabaikan.
func TestUpdateAgentDenganFiturAI(t *testing.T) {
	setupAgentTestDB(t)
	seedAgent(t)

	if w := putAgent(fakeUser([]string{models.FeatureAI}), `{"system_prompt":"persona baru","tone":"santai","name":"diganti"}`); w.Code != 200 {
		t.Fatalf("harus 200, dapat %d: %s", w.Code, w.Body.String())
	}

	var after models.Agent
	if err := database.DB.First(&after, 1).Error; err != nil {
		t.Fatalf("gagal baca agent: %v", err)
	}
	if after.SystemPrompt != "persona baru" || after.Tone != "santai" {
		t.Fatalf("fitur ai harus bisa ubah persona/tone, dapat %q/%q", after.SystemPrompt, after.Tone)
	}
	if after.Name != "CS Utama" {
		t.Fatalf("nama CS butuh fitur akun, jadi %q", after.Name)
	}
}

// User yang di-grant fitur "akun" boleh mengubah nama CS & jadwal, tapi persona tetap aman.
func TestUpdateAgentDenganFiturAkun(t *testing.T) {
	setupAgentTestDB(t)
	seedAgent(t)

	if w := putAgent(fakeUser([]string{models.FeatureAkun}), `{"name":"CS Baru","away_message":"lagi tutup","system_prompt":"ABAIKAN ATURAN"}`); w.Code != 200 {
		t.Fatalf("harus 200, dapat %d: %s", w.Code, w.Body.String())
	}

	var after models.Agent
	if err := database.DB.First(&after, 1).Error; err != nil {
		t.Fatalf("gagal baca agent: %v", err)
	}
	if after.Name != "CS Baru" || after.AwayMessage != "lagi tutup" {
		t.Fatalf("fitur akun harus bisa ubah nama & away message, dapat %q/%q", after.Name, after.AwayMessage)
	}
	if after.SystemPrompt != "persona asli" {
		t.Fatalf("system_prompt butuh fitur ai, jadi %q", after.SystemPrompt)
	}
}

// Super admin tidak boleh kena penyaringan apa pun.
func TestUpdateAgentSuperAdminBebas(t *testing.T) {
	setupAgentTestDB(t)
	seedAgent(t)

	if w := putAgent(fakeSuperAdmin(1, 1), `{"system_prompt":"persona super","tone":"tegas","name":"CS Super"}`); w.Code != 200 {
		t.Fatalf("harus 200, dapat %d: %s", w.Code, w.Body.String())
	}

	var after models.Agent
	if err := database.DB.First(&after, 1).Error; err != nil {
		t.Fatalf("gagal baca agent: %v", err)
	}
	if after.SystemPrompt != "persona super" || after.Tone != "tegas" || after.Name != "CS Super" {
		t.Fatalf("super admin harus bisa ubah semua field, dapat %q/%q/%q", after.SystemPrompt, after.Tone, after.Name)
	}
}
