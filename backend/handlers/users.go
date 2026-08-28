package handlers

import (
	"errors"
	"log"
	"strconv"
	"strings"
	"unicode"

	"wa-assistant/backend/database"
	"wa-assistant/backend/models"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// CRUD akun user untuk halaman "Tim & Akses".
// SEMUA route di file ini dipasang di group RequireSuperAdmin() (lihat main.go), jadi
// pemanggilnya sudah pasti super admin. Yang tetap dijaga di sini adalah hal yang tidak
// bisa dijamin middleware:
//   - akun super admin TIDAK boleh diubah/direset/dihapus lewat API (hanya lewat .env),
//   - role "superadmin" TIDAK boleh diberikan ke siapa pun lewat halaman ini,
//   - super admin tidak boleh menghapus akunnya sendiri (nanti tidak ada yang bisa masuk),
//   - query user selalu difilter tenant supaya tenant lain tidak bocor.
//
// Balasan user SELALU lewat userResponse() dari auth.go supaya hash password,
// token verifikasi email, dan token reset password tidak pernah ikut terkirim.

// minUserPasswordLength = panjang minimum password akun baru / hasil reset,
// samakan dengan aturan ChangePassword di auth.go.
const minUserPasswordLength = 8

// normalizeUsername merapikan username: buang spasi pinggir + huruf kecil semua,
// supaya "Budi " dan "budi" tidak jadi dua akun berbeda.
func normalizeUsername(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// hasSpace true kalau di dalam teks masih ada spasi/tab (setelah di-trim).
// Username berspasi bikin repot saat login, jadi ditolak.
func hasSpace(s string) bool {
	return strings.ContainsFunc(s, unicode.IsSpace)
}

// assignableRole mengecek role yang boleh di-set lewat API: hanya manager & cs.
// "superadmin" sengaja ditolak walau lolos models.IsValidRole — akun super admin
// hanya boleh lahir dari .env, bukan dari halaman manajemen akun.
func assignableRole(role string) bool {
	return models.IsValidRole(role) && role != models.RoleSuperAdmin
}

// managedUsersQuery = daftar user yang boleh disentuh pemanggil: sebatas tenant-nya sendiri.
// Baris super admin ikut kelihatan (dia bisa saja tanpa tenant) supaya tetap tampil di tabel,
// tapi semua handler di bawah menolak mengubahnya.
func managedUsersQuery(c *gin.Context) *gorm.DB {
	return database.DB.Model(&models.User{}).
		Where("tenant_id = ? OR is_super_admin = ?", currentTenantID(c), true)
}

// loadManagedUser mengambil user target dari parameter URL :uid, sudah difilter tenant.
// Error sudah dikirim ke klien di dalam fungsi ini; caller cukup berhenti kalau ok == false.
func loadManagedUser(c *gin.Context) (models.User, bool) {
	id, err := strconv.ParseUint(strings.TrimSpace(c.Param("uid")), 10, 64)
	if err != nil || id == 0 {
		c.JSON(400, gin.H{"error": "ID user tidak valid"})
		return models.User{}, false
	}
	var user models.User
	err = managedUsersQuery(c).First(&user, uint(id)).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(404, gin.H{"error": "User tidak ditemukan"})
		return models.User{}, false
	}
	if err != nil {
		// Error DB lain jangan ditelan jadi 404 — nanti bug-nya susah dilacak.
		log.Printf("users: gagal ambil user %d: %v", id, err)
		c.JSON(500, gin.H{"error": "Gagal mengambil data user"})
		return models.User{}, false
	}
	return user, true
}

// usernameTaken mengecek username sudah dipakai akun lain (cek global, BUKAN per tenant,
// karena kolom username unik untuk seluruh instalasi). exceptID dilewati agar update
// tidak bentrok dengan dirinya sendiri.
func usernameTaken(username string, exceptID uint) (bool, error) {
	var other models.User
	err := database.DB.Where("username = ? AND id <> ?", username, exceptID).First(&other).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// isDuplicateUsernameErr menebak error unique-index dari driver (MySQL & SQLite beda pesan).
// Dipakai untuk kasus balapan: dua request bikin username sama pada detik yang sama,
// jadi lolos pengecekan usernameTaken tapi ditolak DB.
func isDuplicateUsernameErr(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "duplicate") || strings.Contains(msg, "unique")
}

// ListUsers = GET /api/users. Urutan: super admin paling atas, sisanya id menaik
// supaya baris di tabel tidak loncat-loncat tiap kali data diubah.
func ListUsers(c *gin.Context) {
	var users []models.User
	if err := managedUsersQuery(c).Order("is_super_admin DESC, id ASC").Find(&users).Error; err != nil {
		log.Printf("users: gagal ambil daftar user: %v", err)
		c.JSON(500, gin.H{"error": "Gagal mengambil daftar user"})
		return
	}
	out := make([]gin.H, 0, len(users))
	for _, u := range users {
		out = append(out, userResponse(u))
	}
	c.JSON(200, gin.H{"data": out})
}

// CreateUser = POST /api/users. Akun dibuat manual oleh super admin, jadi langsung
// aktif & email dianggap terverifikasi (tidak ada link aktivasi yang dikirim).
func CreateUser(c *gin.Context) {
	var req struct {
		Name     string   `json:"name"`
		Username string   `json:"username"`
		Email    string   `json:"email"`
		Phone    string   `json:"phone"`
		Password string   `json:"password"`
		Role     string   `json:"role"`
		Features []string `json:"features"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "Format data tidak valid"})
		return
	}

	username := normalizeUsername(req.Username)
	if username == "" {
		c.JSON(400, gin.H{"error": "Username wajib diisi"})
		return
	}
	if hasSpace(username) {
		c.JSON(400, gin.H{"error": "Username tidak boleh mengandung spasi"})
		return
	}
	if len(req.Password) < minUserPasswordLength {
		c.JSON(400, gin.H{"error": "Password minimal 8 karakter"})
		return
	}
	role := strings.ToLower(strings.TrimSpace(req.Role))
	if role == models.RoleSuperAdmin {
		c.JSON(400, gin.H{"error": "Akun super admin tidak bisa dibuat lewat halaman ini"})
		return
	}
	if !assignableRole(role) {
		c.JSON(400, gin.H{"error": "Role harus manager atau cs"})
		return
	}

	taken, err := usernameTaken(username, 0)
	if err != nil {
		log.Printf("users: gagal cek username %q: %v", username, err)
		c.JSON(500, gin.H{"error": "Gagal memeriksa username"})
		return
	}
	if taken {
		c.JSON(409, gin.H{"error": "Username sudah dipakai"})
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		log.Printf("users: gagal hash password: %v", err)
		c.JSON(500, gin.H{"error": "Gagal menyimpan password"})
		return
	}

	// Nama kosong dibiarkan jatuh ke username supaya baris tabel tidak tampil melompong.
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = username
	}
	user := models.User{
		Username:      username,
		Password:      string(hash),
		Role:          role,
		Name:          name,
		Email:         strings.TrimSpace(req.Email),
		EmailVerified: true, // dibuat manual admin, tidak perlu verifikasi email
		Phone:         strings.TrimSpace(req.Phone),
		IsSuperAdmin:  false,
		Active:        true,
	}
	// User baru ikut tenant si pembuat. Kalau super admin belum punya tenant (id 0),
	// biarkan NULL — nanti terisi saat tenant-nya jelas.
	if tid := currentTenantID(c); tid > 0 {
		user.TenantID = &tid
	}
	user.SetFeatures(req.Features) // fitur di luar daftar valid dibuang diam-diam

	if err := database.DB.Create(&user).Error; err != nil {
		if isDuplicateUsernameErr(err) {
			c.JSON(409, gin.H{"error": "Username sudah dipakai"})
			return
		}
		log.Printf("users: gagal bikin user %q: %v", username, err)
		c.JSON(500, gin.H{"error": "Gagal menyimpan user"})
		return
	}
	c.JSON(201, gin.H{"data": userResponse(user)})
}

// UpdateUser = PUT /api/users/:uid. TIDAK menerima password (pakai ResetUserPassword)
// dan TIDAK menerima username (username dikunci setelah akun dibuat).
// Semua field pointer supaya update parsial — mis. cuma menggeser switch "aktif" dari
// tabel — tidak ikut menghapus nama, role, atau fitur yang sudah tersimpan.
func UpdateUser(c *gin.Context) {
	user, ok := loadManagedUser(c)
	if !ok {
		return
	}
	if user.IsSuperAdmin {
		c.JSON(400, gin.H{"error": "Akun super admin tidak bisa diubah dari sini"})
		return
	}

	var req struct {
		Name     *string   `json:"name"`
		Email    *string   `json:"email"`
		Phone    *string   `json:"phone"`
		Role     *string   `json:"role"`
		Active   *bool     `json:"active"`
		Features *[]string `json:"features"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "Format data tidak valid"})
		return
	}

	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" {
			c.JSON(400, gin.H{"error": "Nama tidak boleh kosong"})
			return
		}
		user.Name = name
	}
	if req.Role != nil {
		role := strings.ToLower(strings.TrimSpace(*req.Role))
		if role == models.RoleSuperAdmin {
			c.JSON(400, gin.H{"error": "Akun super admin tidak bisa dibuat lewat halaman ini"})
			return
		}
		if !assignableRole(role) {
			c.JSON(400, gin.H{"error": "Role harus manager atau cs"})
			return
		}
		user.Role = role
	}
	if req.Email != nil {
		// Email boleh kosong; kalau diisi disimpan apa adanya karena akun dibuat
		// manual oleh admin (EmailVerified sudah true, tidak ada kiriman verifikasi).
		user.Email = strings.TrimSpace(*req.Email)
	}
	if req.Phone != nil {
		user.Phone = strings.TrimSpace(*req.Phone)
	}
	if req.Active != nil {
		user.Active = *req.Active
	}
	if req.Features != nil {
		user.SetFeatures(*req.Features)
	}

	if err := database.DB.Save(&user).Error; err != nil {
		log.Printf("users: gagal simpan user %d: %v", user.ID, err)
		c.JSON(500, gin.H{"error": "Gagal menyimpan user"})
		return
	}
	c.JSON(200, gin.H{"data": userResponse(user)})
}

// ResetUserPassword = POST /api/users/:uid/password.
// Super admin memaksa password baru tanpa perlu password lama (user-nya lupa/keluar tim).
func ResetUserPassword(c *gin.Context) {
	user, ok := loadManagedUser(c)
	if !ok {
		return
	}
	if user.IsSuperAdmin {
		c.JSON(400, gin.H{"error": "Password super admin diganti lewat .env, bukan dari sini"})
		return
	}

	var req struct {
		NewPassword string `json:"new_password"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "Format data tidak valid"})
		return
	}
	if len(req.NewPassword) < minUserPasswordLength {
		c.JSON(400, gin.H{"error": "Password minimal 8 karakter"})
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		log.Printf("users: gagal hash password user %d: %v", user.ID, err)
		c.JSON(500, gin.H{"error": "Gagal menyimpan password"})
		return
	}
	user.Password = string(hash)
	// Link "lupa password" yang mungkin masih beredar ikut dimatikan.
	user.PasswordResetToken = ""
	user.PasswordResetExpiry = nil
	if err := database.DB.Save(&user).Error; err != nil {
		log.Printf("users: gagal simpan password user %d: %v", user.ID, err)
		c.JSON(500, gin.H{"error": "Gagal menyimpan password"})
		return
	}
	c.JSON(200, gin.H{"message": "Password user berhasil direset"})
}

// DeleteUser = DELETE /api/users/:uid.
func DeleteUser(c *gin.Context) {
	user, ok := loadManagedUser(c)
	if !ok {
		return
	}
	// Dicek duluan supaya super admin yang salah klik barisnya sendiri dapat pesan
	// yang lebih jelas daripada "akun super admin tidak bisa dihapus".
	if user.ID == currentUserID(c) {
		c.JSON(400, gin.H{"error": "Tidak bisa menghapus akun sendiri"})
		return
	}
	if user.IsSuperAdmin {
		c.JSON(400, gin.H{"error": "Akun super admin tidak bisa dihapus"})
		return
	}
	if err := database.DB.Delete(&models.User{}, user.ID).Error; err != nil {
		log.Printf("users: gagal hapus user %d: %v", user.ID, err)
		c.JSON(500, gin.H{"error": "Gagal menghapus user"})
		return
	}
	c.JSON(200, gin.H{"message": "User dihapus"})
}
