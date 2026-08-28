package services

import (
	"os"
	"strings"
	"testing"

	"wa-assistant/backend/database"
	"wa-assistant/backend/models"
)

// Verifikasi manual (bukan bagian suite): jalankan dengan
//
//	RUN_LIVE_AI=1 go test ./backend/services -run TestLiveNoEmptyReply -v
//
// Memanggil API AI sungguhan memakai SALINAN database.
func TestLiveNoEmptyReply(t *testing.T) {
	if os.Getenv("RUN_LIVE_AI") == "" {
		t.Skip("set RUN_LIVE_AI=1 untuk menjalankan verifikasi live")
	}
	database.Init()

	var agent models.Agent
	if err := database.DB.First(&agent, 1).Error; err != nil {
		t.Fatalf("agent 1 tidak ditemukan: %v", err)
	}

	// Percakapan persis yang memulangkan balasan kosong di log produksi 17 Agu 15:18.
	history := []models.ChatHistory{
		{Message: "sampek berdarah loh", Reply: "Wah, BAB berdarah itu paling bikin khawatir ya kak. Sebelum saya rekomendasikan, kakak ada penyakit bawaan nggak?"},
		{Message: "gk ada", Reply: "Baik kak, aman kalau begitu."},
	}
	cases := []string{
		"lah kan kamu nanya ada penyakit bawaan atau gk",
		"yaudah apa sih",
		"lah, knp sih",
	}
	const rounds = 4
	empty := 0
	for _, msg := range cases {
		for i := 0; i < rounds; i++ {
			res, err := ChatWithKnowledge(agent.ID, agent.SystemPrompt, agent.Tone, msg, history)
			if err != nil {
				t.Fatalf("%q: error: %v", msg, err)
			}
			reply := strings.TrimSpace(res.Reply)
			if reply == "" && !res.Escalate {
				t.Errorf("%q: balasan kosong", msg)
				empty++
				continue
			}
			if strings.Contains(reply, "boleh diulang pertanyaannya") {
				t.Errorf("%q: kena teks fallback: %q", msg, reply)
				empty++
				continue
			}
			t.Logf("%q -> [%d char] %s", msg, len(reply), truncateForLog(reply, 90))
		}
	}
	t.Logf("TOTAL: %d panggilan, %d bermasalah", len(cases)*rounds, empty)
}
