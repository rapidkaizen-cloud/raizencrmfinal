package handlers

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"wa-assistant/backend/database"
	"wa-assistant/backend/models"
	"wa-assistant/backend/services"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ── Data kontak Blast Multiple Number ────────────────────────────────────────
//
// Satu tabel per tenant. Impor .xlsx di-upsert by nomor (yang sudah ada dilewati),
// tiap kontak punya nomor penanggung jawab (AgentID) yang dipakai seterusnya.

const (
	multiBlastPageLimit    = 10  // default baris per halaman
	multiBlastMaxPageLimit = 100 // batas atas pilihan pengguna
)

type multiBlastColumn struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}

func loadMultiBlastColumns(tid uint) []multiBlastColumn {
	var s models.MultiBlastSchema
	if database.DB.Where("tenant_id = ?", tid).First(&s).Error != nil {
		return []multiBlastColumn{}
	}
	var cols []multiBlastColumn
	if json.Unmarshal([]byte(s.ColumnsJSON), &cols) != nil || cols == nil {
		return []multiBlastColumn{}
	}
	return cols
}

// mergeMultiBlastColumns menyimpan gabungan kolom lama + kolom baru (urutan lama dipertahankan).
func mergeMultiBlastColumns(tid uint, incoming []multiBlastColumn) []multiBlastColumn {
	cols := loadMultiBlastColumns(tid)
	seen := map[string]bool{}
	for _, c := range cols {
		seen[c.Key] = true
	}
	for _, c := range incoming {
		if c.Key == "" || seen[c.Key] {
			continue
		}
		seen[c.Key] = true
		cols = append(cols, c)
	}
	raw, _ := json.Marshal(cols)
	var s models.MultiBlastSchema
	if database.DB.Where("tenant_id = ?", tid).First(&s).Error != nil {
		database.DB.Create(&models.MultiBlastSchema{TenantID: tid, ColumnsJSON: string(raw)})
	} else {
		database.DB.Model(&s).Update("columns_json", string(raw))
	}
	return cols
}

// ImportMultiBlastContacts = upsert kontak dari file impor: nomor yang sudah ada dilewati.
func ImportMultiBlastContacts(c *gin.Context) {
	if _, ok := resolveAgent(c); !ok {
		return
	}
	tid := currentTenantID(c)
	var req struct {
		Columns []multiBlastColumn        `json:"columns"`
		Rows    []broadcastGuardRecipient `json:"rows"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || len(req.Rows) == 0 {
		c.JSON(400, gin.H{"error": "Data impor kosong"})
		return
	}
	rows := normalizeGuardRecipients(req.Rows)
	if len(rows) == 0 {
		c.JSON(400, gin.H{"error": "Tidak ada nomor valid di file"})
		return
	}
	existing := map[string]bool{}
	for start := 0; start < len(rows); start += 500 {
		chunk := rows[start:min(start+500, len(rows))]
		numbers := make([]string, 0, len(chunk))
		for _, r := range chunk {
			numbers = append(numbers, r.Number)
		}
		var found []string
		database.DB.Model(&models.MultiBlastContact{}).
			Where("tenant_id = ? AND number IN ?", tid, numbers).Pluck("number", &found)
		for _, n := range found {
			existing[n] = true
		}
	}
	inserts := make([]models.MultiBlastContact, 0, len(rows))
	for _, r := range rows {
		if existing[r.Number] {
			continue
		}
		inserts = append(inserts, models.MultiBlastContact{
			TenantID: tid, Number: r.Number, Name: r.Name, VarsJSON: encodeVars(r.Vars),
		})
	}
	// ON CONFLICT DO NOTHING pada unique (tenant_id, number): dua impor bersamaan tidak bisa
	// menghasilkan duplikat maupun menggagalkan seluruh batch — baris bentrok dilewati diam-diam.
	var inserted int64
	if len(inserts) > 0 {
		res := database.DB.Clauses(clause.OnConflict{DoNothing: true}).CreateInBatches(&inserts, 200)
		if res.Error != nil {
			c.JSON(500, gin.H{"error": "Kontak belum bisa disimpan"})
			return
		}
		inserted = res.RowsAffected
	}
	cols := mergeMultiBlastColumns(tid, req.Columns)
	c.JSON(200, gin.H{"inserted": inserted, "skipped": int64(len(rows)) - inserted, "columns": cols})
}

// applyContactOwnership: nomor yang diketik manual (tanpa agent_id) tetapi sudah ada di data kontak
// tetap tunduk pada penanggung jawabnya — dikunci ke nomor itu dan memakai variabel kontaknya.
// Kalau penanggung jawabnya tidak ikut Blast ini, nomor itu dilewati. Penerima khusus (agent_id
// diisi dari form) adalah override manual dan dibiarkan apa adanya.
func applyContactOwnership(tid uint, recs []broadcastGuardRecipient, pool []uint) (out []broadcastGuardRecipient, skippedOtherAgent int) {
	poolSet := map[uint]bool{}
	for _, a := range pool {
		poolSet[a] = true
	}
	numbers := make([]string, 0, len(recs))
	for i := range recs {
		recs[i].Number = services.NormalizePhone(recs[i].Number)
		if recs[i].AgentID == 0 && recs[i].Number != "" {
			numbers = append(numbers, recs[i].Number)
		}
	}
	owned := map[string]models.MultiBlastContact{}
	for start := 0; start < len(numbers); start += 500 {
		var found []models.MultiBlastContact
		database.DB.Where("tenant_id = ? AND number IN ? AND agent_id <> 0", tid, numbers[start:min(start+500, len(numbers))]).Find(&found)
		for _, f := range found {
			owned[f.Number] = f
		}
	}
	out = make([]broadcastGuardRecipient, 0, len(recs))
	for _, r := range recs {
		c, ok := owned[r.Number]
		if r.AgentID != 0 || !ok {
			out = append(out, r)
			continue
		}
		if !poolSet[c.AgentID] {
			skippedOtherAgent++
			continue
		}
		r.AgentID = c.AgentID
		if r.Name == "" {
			r.Name = c.Name
		}
		if len(r.Vars) == 0 && c.VarsJSON != "" {
			_ = json.Unmarshal([]byte(c.VarsJSON), &r.Vars)
		}
		out = append(out, r)
	}
	return out, skippedOtherAgent
}

// ListMultiBlastContacts = tabel kontak berpaginasi. Filter: q (nomor/nama), agent_id ("0" = belum di-assign).
func ListMultiBlastContacts(c *gin.Context) {
	if _, ok := resolveAgent(c); !ok {
		return
	}
	tid := currentTenantID(c)
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	if page < 1 {
		page = 1
	}
	limit, _ := strconv.Atoi(c.Query("limit"))
	if limit < 1 || limit > multiBlastMaxPageLimit {
		limit = multiBlastPageLimit
	}
	q := database.DB.Model(&models.MultiBlastContact{}).Where("tenant_id = ?", tid)
	if s := strings.TrimSpace(c.Query("q")); s != "" {
		like := "%" + s + "%"
		q = q.Where("number LIKE ? OR name LIKE ?", like, like)
	}
	if raw := strings.TrimSpace(c.Query("agent_id")); raw != "" {
		aid, _ := strconv.Atoi(raw)
		q = q.Where("agent_id = ?", aid)
	}
	// ids_only=1: semua id yang cocok dengan filter (tanpa paginasi) — dipakai "centang semua"
	// lintas halaman dan untuk membatasi aksi massal ke kontak milik tabel yang sedang dibuka.
	if c.Query("ids_only") == "1" {
		ids := []uint{}
		q.Order("id desc").Pluck("id", &ids)
		c.JSON(200, gin.H{"ids": ids})
		return
	}
	var total int64
	q.Count(&total)
	var rows []models.MultiBlastContact
	q.Order("id desc").Offset((page - 1) * limit).Limit(limit).Find(&rows)

	// Ringkasan beban per nomor + jumlah belum di-assign (untuk header tabel & tombol bagi rata).
	type agg struct {
		AgentID uint
		N       int64
	}
	var aggs []agg
	database.DB.Model(&models.MultiBlastContact{}).Select("agent_id, COUNT(*) AS n").
		Where("tenant_id = ?", tid).Group("agent_id").Scan(&aggs)
	perAgent := map[string]int64{}
	var unassigned, all int64
	for _, a := range aggs {
		all += a.N
		if a.AgentID == 0 {
			unassigned = a.N
			continue
		}
		perAgent[strconv.FormatUint(uint64(a.AgentID), 10)] = a.N
	}
	c.JSON(200, gin.H{
		"data": rows, "total": total, "page": page, "limit": limit,
		"columns": loadMultiBlastColumns(tid), "per_agent": perAgent, "unassigned": unassigned, "all": all,
	})
}

// AssignMultiBlastContacts menetapkan nomor penanggung jawab untuk kontak terpilih (agent_id 0 = lepas).
func AssignMultiBlastContacts(c *gin.Context) {
	if _, ok := resolveAgent(c); !ok {
		return
	}
	tid := currentTenantID(c)
	var req struct {
		IDs     []uint `json:"ids"`
		AgentID uint   `json:"agent_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || len(req.IDs) == 0 {
		c.JSON(400, gin.H{"error": "Pilih kontak dulu"})
		return
	}
	if req.AgentID != 0 && !agentBelongsToTenant(req.AgentID, tid) {
		c.JSON(400, gin.H{"error": "Nomor tidak dikenal"})
		return
	}
	res := database.DB.Model(&models.MultiBlastContact{}).
		Where("tenant_id = ? AND id IN ?", tid, req.IDs).Update("agent_id", req.AgentID)
	c.JSON(200, gin.H{"updated": res.RowsAffected})
}

// DistributeMultiBlastContacts membagi rata kontak yang belum di-assign ke nomor-nomor terpilih,
// memperhitungkan beban yang sudah ada agar totalnya seimbang.
func DistributeMultiBlastContacts(c *gin.Context) {
	if _, ok := resolveAgent(c); !ok {
		return
	}
	tid := currentTenantID(c)
	var req struct {
		AgentIDs []uint `json:"agent_ids"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || len(req.AgentIDs) == 0 {
		c.JSON(400, gin.H{"error": "Pilih nomor tujuan dulu"})
		return
	}
	pool := make([]uint, 0, len(req.AgentIDs))
	load := map[uint]int{}
	for _, a := range req.AgentIDs {
		if a == 0 || !agentBelongsToTenant(a, tid) {
			c.JSON(400, gin.H{"error": "Ada nomor yang tidak dikenal"})
			return
		}
		if _, dup := load[a]; dup {
			continue
		}
		var n int64
		database.DB.Model(&models.MultiBlastContact{}).Where("tenant_id = ? AND agent_id = ?", tid, a).Count(&n)
		load[a] = int(n)
		pool = append(pool, a)
	}
	var rows []models.MultiBlastContact
	database.DB.Select("id", "number").Where("tenant_id = ? AND agent_id = 0", tid).Order("id asc").Find(&rows)
	assigned := map[uint]int{}
	for _, r := range rows {
		dest := pickFailoverAgent(r.Number, pool, load)
		database.DB.Model(&models.MultiBlastContact{}).Where("id = ?", r.ID).Update("agent_id", dest)
		load[dest]++
		assigned[dest]++
	}
	c.JSON(200, gin.H{"assigned": len(rows), "per_agent": assigned})
}

func DeleteMultiBlastContacts(c *gin.Context) {
	if _, ok := resolveAgent(c); !ok {
		return
	}
	tid := currentTenantID(c)
	var req struct {
		IDs []uint `json:"ids"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || len(req.IDs) == 0 {
		c.JSON(400, gin.H{"error": "Pilih kontak dulu"})
		return
	}
	res := database.DB.Where("tenant_id = ? AND id IN ?", tid, req.IDs).Delete(&models.MultiBlastContact{})
	c.JSON(200, gin.H{"deleted": res.RowsAffected})
}

func agentBelongsToTenant(agentID, tid uint) bool {
	var n int64
	database.DB.Model(&models.Agent{}).Where("id = ? AND tenant_id = ?", agentID, tid).Count(&n)
	return n > 0
}

// multiBlastRecipients memuat penerima dari tabel kontak untuk CreateBroadcast. ids kosong = semua.
// Kontak yang sudah di-assign ke nomor di luar pool dilewati (tidak boleh dikirim nomor lain).
func multiBlastRecipients(tid uint, ids []uint, pool []uint) (recipients []broadcastGuardRecipient, skippedOtherAgent int) {
	poolSet := map[uint]bool{}
	for _, a := range pool {
		poolSet[a] = true
	}
	q := database.DB.Where("tenant_id = ?", tid)
	if len(ids) > 0 {
		q = q.Where("id IN ?", ids)
	}
	var rows []models.MultiBlastContact
	q.Order("id asc").Find(&rows)
	for _, r := range rows {
		if r.AgentID != 0 && !poolSet[r.AgentID] {
			skippedOtherAgent++
			continue
		}
		var vars map[string]string
		if r.VarsJSON != "" {
			_ = json.Unmarshal([]byte(r.VarsJSON), &vars)
		}
		recipients = append(recipients, broadcastGuardRecipient{Number: r.Number, Name: r.Name, Vars: vars, AgentID: r.AgentID})
	}
	return recipients, skippedOtherAgent
}

// recordMultiBlastSent: setelah satu pesan terkirim di Blast Multiple Number, kontak dipegang
// nomor pengirim seterusnya dan hitungan blast-nya naik.
func recordMultiBlastSent(tid uint, number string, agentID uint) {
	now := time.Now()
	database.DB.Model(&models.MultiBlastContact{}).
		Where("tenant_id = ? AND number = ?", tid, services.NormalizePhone(number)).
		Updates(map[string]any{
			"agent_id": agentID, "blast_count": gorm.Expr("blast_count + 1"), "last_blast_at": &now,
		})
}
