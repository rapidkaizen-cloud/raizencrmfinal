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

// Edit kontak: nomor dinormalisasi, nomor bentrok ditolak, penanggung jawab harus anggota master.
func TestUpdateMultiBlastContact(t *testing.T) {
	dsn := "file:" + t.Name() + "?mode=memory&cache=shared&_pragma=busy_timeout(5000)"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.Agent{}, &models.MultiBlastContact{}); err != nil {
		t.Fatal(err)
	}
	prev := database.DB
	database.DB = db
	t.Cleanup(func() { database.DB = prev })

	db.Create(&models.Agent{TenantID: 1, Name: "Master", IsBlastMaster: true})                  // id 1
	db.Create(&models.Agent{TenantID: 1, Name: "Anggota", BlastMasterID: 1})                    // id 2
	db.Create(&models.Agent{TenantID: 1, Name: "Bukan anggota"})                                // id 3
	db.Create(&models.MultiBlastContact{TenantID: 1, MasterID: 1, Number: "628111", Name: "A"}) // id 1
	db.Create(&models.MultiBlastContact{TenantID: 1, MasterID: 1, Number: "628222", Name: "B"}) // id 2

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/agents/:id/multi-blast/contacts/update", fakeUser(nil), UpdateMultiBlastContact)
	post := func(body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/agents/1/multi-blast/contacts/update", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		return w
	}

	if w := post(`{"id":1,"number":"628222","name":"A"}`); w.Code != 400 {
		t.Fatalf("nomor bentrok harus 400, dapat %d: %s", w.Code, w.Body)
	}
	if w := post(`{"id":1,"number":"628111","agent_id":3}`); w.Code != 400 {
		t.Fatalf("agent bukan anggota harus 400, dapat %d: %s", w.Code, w.Body)
	}
	if w := post(`{"id":1,"number":"0812-3456-789","name":" Budi ","vars":{"no_resi":"JNE1"},"agent_id":2}`); w.Code != 200 {
		t.Fatalf("update valid harus 200, dapat %d: %s", w.Code, w.Body)
	}
	var c models.MultiBlastContact
	db.First(&c, 1)
	if c.Number != "628123456789" || c.Name != "Budi" || c.AgentID != 2 || !strings.Contains(c.VarsJSON, "JNE1") {
		t.Fatalf("hasil update salah: %+v", c)
	}
	// Nomor sendiri tidak dihitung bentrok.
	if w := post(`{"id":1,"number":"628123456789","name":"Budi"}`); w.Code != 200 {
		t.Fatalf("update nomor sendiri harus 200, dapat %d: %s", w.Code, w.Body)
	}
}
