package handlers

import (
	"testing"

	"wa-assistant/backend/models"
)

func TestAssignByHistoryLocksOwnersAndBalancesRest(t *testing.T) {
	pool := []uint{1, 2, 3}
	recipients := []broadcastGuardRecipient{
		{Number: "628111"}, {Number: "628222"}, {Number: "628333"},
		{Number: "628444"}, {Number: "628555"}, {Number: "628666"},
	}
	owners := map[string]uint{
		"628111": 2, "628222": 2, // pernah chat dengan nomor 2
		"628333": 9, // pemilik di luar pool -> dianggap baru
	}
	// Penerima khusus nomor 3 menang atas riwayat; nomor di luar pool diabaikan.
	explicit := append([]broadcastGuardRecipient{{Number: "628111", AgentID: 3}, {Number: "628777", AgentID: 42}}, recipients[1:]...)
	ex := assignByHistory(explicit, pool, owners)
	if ex[0].AgentID != 3 || !ex[0].Locked {
		t.Errorf("penerima khusus: agent=%d locked=%v, mau 3 terkunci", ex[0].AgentID, ex[0].Locked)
	}
	if ex[1].Locked || ex[1].AgentID == 42 {
		t.Errorf("agent di luar pool: agent=%d locked=%v", ex[1].AgentID, ex[1].Locked)
	}
	out := assignByHistory(recipients, pool, owners)
	if len(out) != 6 {
		t.Fatalf("jumlah penerima = %d, mau 6", len(out))
	}
	for _, r := range out[:2] {
		if r.AgentID != 2 || !r.Locked {
			t.Errorf("%s: agent=%d locked=%v, mau agent 2 terkunci", r.Number, r.AgentID, r.Locked)
		}
	}
	load := map[uint]int{}
	for _, r := range out {
		load[r.AgentID]++
		if r.AgentID == 9 {
			t.Errorf("%s dikirim ke nomor di luar pool", r.Number)
		}
		if r.Locked && r.AgentID != 2 {
			t.Errorf("%s terkunci tanpa riwayat", r.Number)
		}
	}
	// 2 terkunci di nomor 2, 4 sisanya dibagi rata -> tiap nomor 2.
	for _, a := range pool {
		if load[a] != 2 {
			t.Errorf("beban nomor %d = %d, mau 2 (%v)", a, load[a], load)
		}
	}
}

func TestRenderBroadcastMessageAppliesVars(t *testing.T) {
	r := models.BroadcastRecipient{Name: "Budi", VarsJSON: `{"no_resi":"JNE123","tanggal":"16/09/2026"}`}
	got := renderBroadcastMessage("Halo {nama}, resi {no_resi} tgl {tanggal}. {kosong}", r)
	want := "Halo Budi, resi JNE123 tgl 16/09/2026. {kosong}"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestAgentBlastSettingsOverride(t *testing.T) {
	b := models.Broadcast{RestEvery: 25, RestDuration: 90, AgentSettingsJSON: `{"7":{"min_delay":20,"max_delay":40,"rest_every":0}}`}
	minD, maxD, every, dur := agentBlastSettings(b, 7, 10, 30)
	if minD != 20 || maxD != 40 || every != 0 || dur != 0 {
		t.Fatalf("override nomor 7 = %d %d %d %d", minD, maxD, every, dur)
	}
	minD, maxD, every, dur = agentBlastSettings(b, 8, 10, 30)
	if minD != 10 || maxD != 30 || every != 25 || dur != 90 {
		t.Fatalf("global nomor 8 = %d %d %d %d", minD, maxD, every, dur)
	}
	if s, err := parseAgentSettings(`{"7":{"min_delay":1,"max_delay":0},"99":{"min_delay":5}}`, []uint{7}); err != nil || s != `{"7":{"min_delay":8,"max_delay":28,"rest_every":0,"rest_duration":0}}` {
		t.Fatalf("parseAgentSettings = %q, %v", s, err)
	}
	b.AgentSettingsJSON = `{"7":{"min_delay":8,"max_delay":28,"message":"Khusus {nama}"}}`
	if got := agentBlastMessage(b, 7); got != "Khusus {nama}" {
		t.Fatalf("pesan khusus nomor 7 = %q", got)
	}
	b.Message = "Global"
	if got := agentBlastMessage(b, 8); got != "Global" {
		t.Fatalf("pesan global nomor 8 = %q", got)
	}
	if allAgentsHaveMessage([]uint{7, 8}, b.AgentSettingsJSON, "") {
		t.Fatal("nomor 8 tanpa pesan harus ditolak")
	}
	if !allAgentsHaveMessage([]uint{7}, b.AgentSettingsJSON, "") {
		t.Fatal("semua nomor punya pesan khusus harus lolos")
	}
}
