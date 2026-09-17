package handlers

import (
	"encoding/json"
	"strconv"
	"strings"

	"wa-assistant/backend/database"
	"wa-assistant/backend/models"
	"wa-assistant/backend/services"
)

// ── Blast Multiple Number ────────────────────────────────────────────────────
//
// Beda dengan rotasi biasa (sticky hash), di sini penerima menempel ke nomor yang
// PERNAH chat dengannya: riwayat chat dua arah diutamakan, lalu blast sebelumnya.
// Penerima seperti itu dikunci (Locked) — failover tidak boleh memindahkannya.
// Penerima baru dibagi rata ke nomor pool dan boleh di-failover seperti biasa.

const maxMultiBlastNumbers = 10

const assignModeHistory = "history"

// agentBlastSetting = setelan khusus satu nomor (menimpa setelan global broadcast):
// jeda/istirahat dan, bila diisi, template pesan sendiri (spin & variabel tetap berlaku).
type agentBlastSetting struct {
	MinDelay     int    `json:"min_delay"`
	MaxDelay     int    `json:"max_delay"`
	RestEvery    int    `json:"rest_every"`
	RestDuration int    `json:"rest_duration"`
	Message      string `json:"message,omitempty"`
}

// parseStoredAgentSettings membaca AgentSettingsJSON; nil bila kosong/rusak.
func parseStoredAgentSettings(raw string) map[string]agentBlastSetting {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var m map[string]agentBlastSetting
	if json.Unmarshal([]byte(raw), &m) != nil {
		return nil
	}
	return m
}

// agentBlastMessage = template pesan untuk satu nomor: pesan khusus bila ada, kalau tidak pesan global.
func agentBlastMessage(b models.Broadcast, agentID uint) string {
	if s, ok := parseStoredAgentSettings(b.AgentSettingsJSON)[strconv.FormatUint(uint64(agentID), 10)]; ok && strings.TrimSpace(s.Message) != "" {
		return s.Message
	}
	return b.Message
}

// allAgentsHaveMessage: tiap nomor pool punya pesan (khusus atau global) — pesan global boleh
// kosong hanya kalau semua nomor punya pesan sendiri.
func allAgentsHaveMessage(pool []uint, settingsJSON, global string) bool {
	if strings.TrimSpace(global) != "" {
		return true
	}
	m := parseStoredAgentSettings(settingsJSON)
	for _, a := range pool {
		if strings.TrimSpace(m[strconv.FormatUint(uint64(a), 10)].Message) == "" {
			return false
		}
	}
	return true
}

// parseAgentSettings memvalidasi JSON setelan per nomor dari form; hanya nomor di pool yang disimpan.
func parseAgentSettings(raw string, pool []uint) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	var in map[string]agentBlastSetting
	if err := json.Unmarshal([]byte(raw), &in); err != nil {
		return "", err
	}
	poolSet := map[uint]bool{}
	for _, a := range pool {
		poolSet[a] = true
	}
	out := map[string]agentBlastSetting{}
	for k, s := range in {
		id, err := strconv.ParseUint(k, 10, 64)
		if err != nil || !poolSet[uint(id)] {
			continue
		}
		s.MinDelay, s.MaxDelay = normalizeBroadcastDelay(s.MinDelay, s.MaxDelay)
		s.RestEvery, s.RestDuration = normalizeBroadcastRest(s.RestEvery, s.RestDuration)
		s.Message = strings.TrimSpace(s.Message)
		out[k] = s
	}
	if len(out) == 0 {
		return "", nil
	}
	b, err := json.Marshal(out)
	return string(b), err
}

// agentBlastSettings mengembalikan jeda/istirahat untuk satu nomor: setelan khusus bila ada,
// kalau tidak pakai nilai global broadcast.
func agentBlastSettings(b models.Broadcast, agentID uint, minD, maxD int) (int, int, int, int) {
	restEvery, restDuration := normalizeBroadcastRest(b.RestEvery, b.RestDuration)
	s, ok := parseStoredAgentSettings(b.AgentSettingsJSON)[strconv.FormatUint(uint64(agentID), 10)]
	if !ok {
		return minD, maxD, restEvery, restDuration
	}
	minD, maxD = normalizeBroadcastDelay(s.MinDelay, s.MaxDelay)
	restEvery, restDuration = normalizeBroadcastRest(s.RestEvery, s.RestDuration)
	return minD, maxD, restEvery, restDuration
}

// historyOwners memetakan nomor penerima -> agent di pool yang pernah berinteraksi dengannya.
// Prioritas: chat dua arah (ChatHistory, yang terbaru menang) lalu blast sebelumnya yang terkirim.
func historyOwners(numbers []string, pool []uint) map[string]uint {
	owners := map[string]uint{}
	if len(numbers) == 0 || len(pool) == 0 {
		return owners
	}
	type row struct {
		AgentID uint
		Number  string
		Last    int64
	}
	// latestOwner: per nomor, agent dengan interaksi (id) paling baru.
	latestOwner := func(rows []row, into map[string]uint) {
		last := map[string]int64{}
		for _, r := range rows {
			if r.Last > last[r.Number] {
				last[r.Number] = r.Last
				into[r.Number] = r.AgentID
			}
		}
	}
	chatOwner, blastOwner := map[string]uint{}, map[string]uint{}
	// ponytail: chunk 500 agar aman dari batas variabel SQLite lama.
	for start := 0; start < len(numbers); start += 500 {
		chunk := numbers[start:min(start+500, len(numbers))]
		var chats, blasts []row
		database.DB.Model(&models.ChatHistory{}).
			Select("agent_id, sender AS number, MAX(id) AS last").
			Where("agent_id IN ? AND sender IN ?", pool, chunk).
			Group("agent_id, sender").Scan(&chats)
		database.DB.Model(&models.BroadcastRecipient{}).
			Select("agent_id, number, MAX(id) AS last").
			Where("agent_id IN ? AND number IN ? AND status = ?", pool, chunk, "sent").
			Group("agent_id, number").Scan(&blasts)
		latestOwner(chats, chatOwner)
		latestOwner(blasts, blastOwner)
	}
	for n, a := range blastOwner {
		owners[n] = a
	}
	for n, a := range chatOwner { // chat dua arah menang atas blast
		owners[n] = a
	}
	return owners
}

// assignByHistory membagi penerima: penerima khusus (AgentID dari form) dan yang punya riwayat
// dikunci ke nomornya, sisanya ke nomor dengan beban paling ringan (seri diputus sticky).
// Murni, tanpa DB — mudah diuji.
func assignByHistory(recipients []broadcastGuardRecipient, pool []uint, owners map[string]uint) []models.BroadcastRecipient {
	load := map[uint]int{}
	for _, a := range pool {
		load[a] = 0
	}
	out := make([]models.BroadcastRecipient, 0, len(recipients))
	for _, r := range recipients {
		rec := models.BroadcastRecipient{Number: r.Number, Name: r.Name, Status: "pending", VarsJSON: encodeVars(r.Vars)}
		owner, ok := owners[r.Number]
		if _, explicit := load[r.AgentID]; explicit && r.AgentID != 0 {
			owner, ok = r.AgentID, true // penerima khusus nomor ini menang atas riwayat
		}
		if ok {
			if _, inPool := load[owner]; inPool {
				rec.AgentID, rec.Locked = owner, true
				load[owner]++
				out = append(out, rec)
				continue
			}
		}
		rec.AgentID = pickFailoverAgent(r.Number, pool, load)
		load[rec.AgentID]++
		out = append(out, rec)
	}
	return out
}

func encodeVars(vars map[string]string) string {
	if len(vars) == 0 {
		return ""
	}
	b, err := json.Marshal(vars)
	if err != nil {
		return ""
	}
	return string(b)
}

// applyRecipientVars mengganti {kunci} dengan nilai dari VarsJSON penerima. Kunci yang tidak
// ada dibiarkan apa adanya. Dipanggil setelah spin & {nama}.
func applyRecipientVars(msg, varsJSON string) string {
	if strings.TrimSpace(varsJSON) == "" || !strings.Contains(msg, "{") {
		return msg
	}
	var vars map[string]string
	if json.Unmarshal([]byte(varsJSON), &vars) != nil {
		return msg
	}
	for k, v := range vars {
		if k == "" {
			continue
		}
		msg = strings.ReplaceAll(msg, "{"+k+"}", v)
	}
	return msg
}

// renderBroadcastMessage = teks final untuk satu penerima: spin -> {nama} -> variabel impor.
func renderBroadcastMessage(template string, r models.BroadcastRecipient) string {
	return applyRecipientVars(personalize(spinText(template), r.Name), r.VarsJSON)
}

// lockedPendingOffline = masih ada penerima terkunci yang nomornya offline tapi belum
// dikarantina keras (mis. sedang reconnect). Worker rotasi menunggu, bukan menyerah.
func lockedPendingOffline(broadcastID uint, campaign *broadcastCampaign) bool {
	var agentIDs []uint
	database.DB.Model(&models.BroadcastRecipient{}).
		Where("broadcast_id = ? AND status = ? AND locked = ?", broadcastID, "pending", true).
		Distinct().Pluck("agent_id", &agentIDs)
	for _, a := range agentIDs {
		if !services.WA(a).IsConnected() && !campaign.isHardQuarantined(a) {
			return true
		}
	}
	return false
}
