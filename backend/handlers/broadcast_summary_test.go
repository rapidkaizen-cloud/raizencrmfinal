package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"wa-assistant/backend/database"
	"wa-assistant/backend/models"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Riwayat & ringkasan: anggota pool melihat kampanye milik master-nya; filter sender & status
// membatasi keduanya; penerima lama tanpa agent_id dihitung sebagai nomor pemilik.
func TestBroadcastSummaryAndListVisibility(t *testing.T) {
	dsn := "file:" + t.Name() + "?mode=memory&cache=shared&_pragma=busy_timeout(5000)"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.Agent{}, &models.Broadcast{}, &models.BroadcastRecipient{}); err != nil {
		t.Fatal(err)
	}
	prev := database.DB
	database.DB = db
	t.Cleanup(func() { database.DB = prev })

	db.Create(&models.Agent{TenantID: 1, Name: "Master", IsBlastMaster: true}) // 1
	db.Create(&models.Agent{TenantID: 1, Name: "Anggota A", BlastMasterID: 1}) // 2
	db.Create(&models.Agent{TenantID: 1, Name: "Anggota B", BlastMasterID: 1}) // 3
	// Multi blast milik master, dikirim nomor 2 & 3.
	db.Create(&models.Broadcast{TenantID: 1, AgentID: 1, AgentIDs: "[2,3]", AssignMode: "history", Status: "done", Total: 3, Sent: 2, Failed: 1})
	db.Create(&[]models.BroadcastRecipient{
		{BroadcastID: 1, Number: "628111", AgentID: 2, Status: "sent"},
		{BroadcastID: 1, Number: "628222", AgentID: 2, Status: "failed"},
		{BroadcastID: 1, Number: "628333", AgentID: 3, Status: "sent"},
	})
	// Blast lama nomor 2 sendiri, penerima tanpa agent_id.
	db.Create(&models.Broadcast{TenantID: 1, AgentID: 2, Status: "failed", Total: 1, Failed: 1})
	db.Create(&models.BroadcastRecipient{BroadcastID: 2, Number: "628444", Status: "failed"})

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/agents/:id/broadcasts", fakeUser(nil), ListBroadcasts)
	r.GET("/agents/:id/broadcast/summary", fakeUser(nil), BroadcastSummary)
	get := func(path string) map[string]any {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != 200 {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body)
		}
		var out map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &out)
		return out
	}

	if n := get("/agents/2/broadcasts")["total"].(float64); n != 2 {
		t.Fatalf("anggota harus melihat 2 blast (miliknya + milik master), dapat %v", n)
	}
	if n := get("/agents/3/broadcasts?status=failed")["total"].(float64); n != 0 {
		t.Fatalf("filter status: nomor 3 tidak punya blast failed, dapat %v", n)
	}
	if n := get("/agents/1/broadcasts?sender=3")["total"].(float64); n != 1 {
		t.Fatalf("filter sender=3 harus 1 blast, dapat %v", n)
	}
	if n := get("/agents/2/broadcasts?has=sent")["total"].(float64); n != 1 {
		t.Fatalf("has=sent harus 1 blast, dapat %v", n)
	}
	if n := get("/agents/1/broadcasts?has=failed&sender=3")["total"].(float64); n != 0 {
		t.Fatalf("has=failed sender=3: nomor 3 tidak punya gagal, dapat %v", n)
	}

	sum := get("/agents/2/broadcast/summary")["data"].(map[string]any)
	if sum["broadcasts"].(float64) != 2 || sum["sent"].(float64) != 2 || sum["failed"].(float64) != 2 {
		t.Fatalf("ringkasan nomor 2 salah: %v", sum)
	}
	per := sum["per_agent"].([]any)
	if len(per) != 2 {
		t.Fatalf("per_agent harus 2 nomor, dapat %v", per)
	}
	for _, row := range per {
		m := row.(map[string]any)
		switch m["agent_id"].(float64) {
		case 2: // 1 terkirim + 1 gagal di blast 1, 1 gagal (agent_id 0) di blast 2
			if m["sent"].(float64) != 1 || m["failed"].(float64) != 2 || m["broadcasts"].(float64) != 2 || m["name"] != "Anggota A" {
				t.Fatalf("baris nomor 2 salah: %v", m)
			}
		case 3:
			if m["sent"].(float64) != 1 || m["failed"].(float64) != 0 {
				t.Fatalf("baris nomor 3 salah: %v", m)
			}
		default:
			t.Fatalf("nomor tak dikenal di per_agent: %v", m)
		}
	}
	sum = get("/agents/1/broadcast/summary?sender=3")["data"].(map[string]any)
	if sum["sent"].(float64) != 1 || len(sum["per_agent"].([]any)) != 1 {
		t.Fatalf("ringkasan sender=3 salah: %v", sum)
	}
}
