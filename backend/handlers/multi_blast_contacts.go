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
	masterID, tid, ok := resolveBlastMaster(c)
	if !ok {
		return
	}
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
			TenantID: tid, MasterID: masterID, Number: r.Number, Name: r.Name, VarsJSON: encodeVars(r.Vars),
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
	masterID, tid, ok := resolveBlastMaster(c)
	if !ok {
		return
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	if page < 1 {
		page = 1
	}
	limit, _ := strconv.Atoi(c.Query("limit"))
	if limit < 1 || limit > multiBlastMaxPageLimit {
		limit = multiBlastPageLimit
	}
	// Kontak tanpa pemilik (data sebelum fitur master ada, atau bekas master yang diturunkan)
	// diadopsi master yang pertama membuka menu ini, supaya tidak hilang dari semua tabel.
	database.DB.Model(&models.MultiBlastContact{}).Where("tenant_id = ? AND master_id = 0", tid).
		Update("master_id", masterID)
	q := database.DB.Model(&models.MultiBlastContact{}).Where("tenant_id = ? AND master_id = ?", tid, masterID)
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
		Where("tenant_id = ? AND master_id = ?", tid, masterID).Group("agent_id").Scan(&aggs)
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
	masterID, tid, ok := resolveBlastMaster(c)
	if !ok {
		return
	}
	var req struct {
		IDs     []uint `json:"ids"`
		AgentID uint   `json:"agent_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || len(req.IDs) == 0 {
		c.JSON(400, gin.H{"error": "Pilih kontak dulu"})
		return
	}
	if req.AgentID != 0 && !agentBelongsToMaster(req.AgentID, masterID, tid) {
		c.JSON(400, gin.H{"error": "Nomor itu bukan anggota master ini"})
		return
	}
	res := database.DB.Model(&models.MultiBlastContact{}).
		Where("tenant_id = ? AND master_id = ? AND id IN ?", tid, masterID, req.IDs).Update("agent_id", req.AgentID)
	c.JSON(200, gin.H{"updated": res.RowsAffected})
}

// DistributeMultiBlastContacts membagi rata kontak yang belum di-assign ke nomor-nomor terpilih,
// memperhitungkan beban yang sudah ada agar totalnya seimbang.
func DistributeMultiBlastContacts(c *gin.Context) {
	masterID, tid, ok := resolveBlastMaster(c)
	if !ok {
		return
	}
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
		if a == 0 || !agentBelongsToMaster(a, masterID, tid) {
			c.JSON(400, gin.H{"error": "Ada nomor yang bukan anggota master ini"})
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
	database.DB.Select("id", "number").Where("tenant_id = ? AND master_id = ? AND agent_id = 0", tid, masterID).Order("id asc").Find(&rows)
	assigned := map[uint]int{}
	for _, r := range rows {
		dest := pickFailoverAgent(r.Number, pool, load)
		database.DB.Model(&models.MultiBlastContact{}).Where("id = ?", r.ID).Update("agent_id", dest)
		load[dest]++
		assigned[dest]++
	}
	c.JSON(200, gin.H{"assigned": len(rows), "per_agent": assigned})
}

// UpdateMultiBlastContact mengubah satu kontak: nomor, nama, variabel, dan penanggung jawab.
// blast_count / last_blast_at tidak bisa diubah dari sini (riwayat).
func UpdateMultiBlastContact(c *gin.Context) {
	masterID, tid, ok := resolveBlastMaster(c)
	if !ok {
		return
	}
	var req struct {
		ID      uint              `json:"id"`
		Number  string            `json:"number"`
		Name    string            `json:"name"`
		Vars    map[string]string `json:"vars"`
		AgentID uint              `json:"agent_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.ID == 0 {
		c.JSON(400, gin.H{"error": "Data kontak tidak valid"})
		return
	}
	number := services.NormalizePhone(req.Number)
	if number == "" {
		c.JSON(400, gin.H{"error": "Nomor tidak valid"})
		return
	}
	if req.AgentID != 0 && !agentBelongsToMaster(req.AgentID, masterID, tid) {
		c.JSON(400, gin.H{"error": "Nomor itu bukan anggota master ini"})
		return
	}
	var dup int64
	database.DB.Model(&models.MultiBlastContact{}).
		Where("tenant_id = ? AND number = ? AND id <> ?", tid, number, req.ID).Count(&dup)
	if dup > 0 {
		c.JSON(400, gin.H{"error": "Nomor +" + number + " sudah ada di data kontak"})
		return
	}
	res := database.DB.Model(&models.MultiBlastContact{}).
		Where("tenant_id = ? AND master_id = ? AND id = ?", tid, masterID, req.ID).
		Updates(map[string]any{
			"number": number, "name": strings.TrimSpace(req.Name), "vars_json": encodeVars(req.Vars), "agent_id": req.AgentID,
		})
	if res.Error != nil {
		c.JSON(500, gin.H{"error": "Kontak belum bisa disimpan"})
		return
	}
	if res.RowsAffected == 0 {
		c.JSON(404, gin.H{"error": "Kontak tidak ditemukan"})
		return
	}
	c.JSON(200, gin.H{"updated": res.RowsAffected})
}

func DeleteMultiBlastContacts(c *gin.Context) {
	masterID, tid, ok := resolveBlastMaster(c)
	if !ok {
		return
	}
	var req struct {
		IDs []uint `json:"ids"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || len(req.IDs) == 0 {
		c.JSON(400, gin.H{"error": "Pilih kontak dulu"})
		return
	}
	res := database.DB.Where("tenant_id = ? AND master_id = ? AND id IN ?", tid, masterID, req.IDs).Delete(&models.MultiBlastContact{})
	c.JSON(200, gin.H{"deleted": res.RowsAffected})
}

// resolveBlastMaster: agent pada URL harus master agent. Data kontak & aksi di menu ini
// selalu dalam lingkup satu master.
func resolveBlastMaster(c *gin.Context) (masterID, tid uint, ok bool) {
	id, ok := resolveAgent(c)
	if !ok {
		return 0, 0, false
	}
	var a models.Agent
	if database.DB.Select("id", "is_blast_master").First(&a, id).Error != nil || !a.IsBlastMaster {
		c.JSON(403, gin.H{"error": "Nomor ini bukan master agent Blast Multiple Number"})
		return 0, 0, false
	}
	return id, currentTenantID(c), true
}

// agentBelongsToMaster = agent milik tenant ini DAN anggota master tersebut.
func agentBelongsToMaster(agentID, masterID, tid uint) bool {
	var n int64
	database.DB.Model(&models.Agent{}).
		Where("id = ? AND tenant_id = ? AND blast_master_id = ?", agentID, tid, masterID).Count(&n)
	return n > 0
}

// SaveMultiBlastStructure menyimpan struktur master -> anggota untuk seluruh tenant (super admin).
// Body: {"items":[{"agent_id":1,"is_master":true},{"agent_id":2,"master_id":1},...]}.
// Agent yang tidak disebut tidak diubah.
func SaveMultiBlastStructure(c *gin.Context) {
	if !isSuperAdmin(c) {
		c.JSON(403, gin.H{"error": "Hanya super admin yang bisa mengatur master agent"})
		return
	}
	tid := currentTenantID(c)
	var req struct {
		Items []struct {
			AgentID  uint `json:"agent_id"`
			IsMaster bool `json:"is_master"`
			MasterID uint `json:"master_id"`
		} `json:"items"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || len(req.Items) == 0 {
		c.JSON(400, gin.H{"error": "Struktur master tidak valid"})
		return
	}
	var agents []models.Agent
	database.DB.Select("id", "is_blast_master", "blast_master_id").Where("tenant_id = ?", tid).Find(&agents)
	current := map[uint]models.Agent{}
	masters := map[uint]bool{} // keadaan akhir: siapa saja yang master
	for _, a := range agents {
		current[a.ID] = a
		masters[a.ID] = a.IsBlastMaster
	}
	for _, it := range req.Items {
		if _, ok := current[it.AgentID]; !ok {
			c.JSON(400, gin.H{"error": "Ada nomor yang tidak dikenal"})
			return
		}
		masters[it.AgentID] = it.IsMaster
	}
	for _, it := range req.Items {
		if !it.IsMaster && it.MasterID != 0 && (!masters[it.MasterID] || it.MasterID == it.AgentID) {
			c.JSON(400, gin.H{"error": "Anggota hanya bisa dipasang ke nomor yang berstatus master"})
			return
		}
	}
	err := database.DB.Transaction(func(tx *gorm.DB) error {
		for _, it := range req.Items {
			newMaster := it.MasterID
			if it.IsMaster {
				newMaster = 0 // master tidak bisa jadi anggota master lain
			}
			if err := tx.Model(&models.Agent{}).Where("id = ? AND tenant_id = ?", it.AgentID, tid).
				Updates(map[string]any{"is_blast_master": it.IsMaster, "blast_master_id": newMaster}).Error; err != nil {
				return err
			}
			old := current[it.AgentID]
			contacts := tx.Model(&models.MultiBlastContact{}).Where("tenant_id = ? AND agent_id = ?", tid, it.AgentID)
			switch {
			case old.BlastMasterID == newMaster:
			case newMaster != 0: // pindah master: kontak yang dipegangnya ikut pindah
				if err := contacts.Update("master_id", newMaster).Error; err != nil {
					return err
				}
			default: // keluar dari master: kontaknya kembali ke kolam master lama
				if err := contacts.Update("agent_id", 0).Error; err != nil {
					return err
				}
			}
			// Master yang diturunkan: kontaknya jadi tanpa pemilik (master_id 0) dan diadopsi
			// master pertama yang membuka menu ini (lihat ListMultiBlastContacts).
			if old.IsBlastMaster && !it.IsMaster {
				if err := tx.Model(&models.MultiBlastContact{}).Where("tenant_id = ? AND master_id = ?", tid, it.AgentID).
					Updates(map[string]any{"master_id": 0, "agent_id": 0}).Error; err != nil {
					return err
				}
			}
		}
		// Anggota dari master yang sudah tidak berstatus master dilepas.
		notMasters := make([]uint, 0)
		for id, isMaster := range masters {
			if !isMaster {
				notMasters = append(notMasters, id)
			}
		}
		if len(notMasters) > 0 {
			return tx.Model(&models.Agent{}).Where("tenant_id = ? AND blast_master_id IN ?", tid, notMasters).
				Update("blast_master_id", 0).Error
		}
		return nil
	})
	if err != nil {
		c.JSON(500, gin.H{"error": "Struktur master belum bisa disimpan"})
		return
	}
	var out []models.Agent
	database.DB.Where("tenant_id = ?", tid).Order("id asc").Find(&out)
	c.JSON(200, gin.H{"data": out})
}

// multiBlastRecipients memuat penerima dari tabel kontak untuk CreateBroadcast. ids kosong = semua.
// Kontak yang sudah di-assign ke nomor di luar pool dilewati (tidak boleh dikirim nomor lain).
func multiBlastRecipients(tid, masterID uint, ids []uint, pool []uint) (recipients []broadcastGuardRecipient, skippedOtherAgent int) {
	poolSet := map[uint]bool{}
	for _, a := range pool {
		poolSet[a] = true
	}
	q := database.DB.Where("tenant_id = ? AND master_id = ?", tid, masterID)
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
