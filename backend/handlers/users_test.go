package handlers

import (
	"encoding/json"
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

const testUserTenant = uint(1)

// setupUserTestDB menukar database.DB dengan SQLite di memori khusus test ini,
// lalu mengembalikannya lagi saat test selesai.
func setupUserTestDB(t *testing.T) {
	t.Helper()
	dsn := "file:" + t.Name() + "?mode=memory&cache=shared&_pragma=busy_timeout(5000)"
	db, err := gorm.Open(
		sqlite.Open(dsn),
		&gorm.Config{Logger: logger.Default.LogMode(logger.Silent)},
	)
	if err != nil {
		t.Fatalf("gagal buka sqlite memori: %v", err)
	}
	if err := db.AutoMigrate(&models.User{}); err != nil {
		t.Fatalf("gagal migrate tabel user: %v", err)
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

// fakeSuperAdmin meniru context yang ditaruh AuthMiddleware untuk super admin,
// lengkap dengan tenant_id (dipakai filter tenant di managedUsersQuery).
func fakeSuperAdmin(userID, tenantID uint) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set("user_id", userID)
		c.Set("is_super_admin", true)
		c.Set("features", []string{})
		c.Set("tenant_id", tenantID)
		c.Next()
	}
}

// usersRouter memasang kelima route /users di belakang middleware auth palsu.
func usersRouter(auth gin.HandlerFunc) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/users", auth, ListUsers)
	r.POST("/users", auth, CreateUser)
	r.PUT("/users/:uid", auth, UpdateUser)
	r.POST("/users/:uid/password", auth, ResetUserPassword)
	r.DELETE("/users/:uid", auth, DeleteUser)
	return r
}

// callUsers memanggil satu route /users sekali dan mengembalikan responsnya.
func callUsers(auth gin.HandlerFunc, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	usersRouter(auth).ServeHTTP(w, req)
	return w
}

// seedUser menaruh satu user langsung ke DB test (tanpa lewat handler).
func seedUser(t *testing.T, u models.User) models.User {
	t.Helper()
	if u.Password == "" {
		u.Password = "hash-palsu"
	}
	if u.TenantID == nil && !u.IsSuperAdmin {
		tid := testUserTenant
		u.TenantID = &tid
	}
	if err := database.DB.Create(&u).Error; err != nil {
		t.Fatalf("gagal seed user %q: %v", u.Username, err)
	}
	return u
}

// mustLoadUser mengambil ulang user dari DB untuk memastikan perubahan benar-benar tersimpan
// (atau justru TIDAK tersimpan, saat menguji aturan penolakan).
func mustLoadUser(t *testing.T, id uint) models.User {
	t.Helper()
	var u models.User
	if err := database.DB.First(&u, id).Error; err != nil {
		t.Fatalf("user %d tidak ada di DB: %v", id, err)
	}
	return u
}

// --- CreateUser ---------------------------------------------------------------

// Role "superadmin" tidak boleh dibuat lewat API, walau pemanggilnya super admin.
func TestCreateUserTolakRoleSuperAdmin(t *testing.T) {
	setupUserTestDB(t)
	auth := fakeSuperAdmin(1, testUserTenant)

	body := `{"name":"Budi","username":"budi","password":"rahasia123","role":"superadmin"}`
	if w := callUsers(auth, http.MethodPost, "/users", body); w.Code != 400 {
		t.Fatalf("role superadmin harus ditolak 400, dapat %d (%s)", w.Code, w.Body.String())
	}
	var count int64
	database.DB.Model(&models.User{}).Count(&count)
	if count != 0 {
		t.Fatalf("user tidak boleh terbuat saat role ditolak, ada %d baris", count)
	}
}

// Role di luar manager/cs (mis. "admin" atau "owner") juga ditolak.
func TestCreateUserTolakRoleTidakDikenal(t *testing.T) {
	setupUserTestDB(t)
	auth := fakeSuperAdmin(1, testUserTenant)

	for _, role := range []string{"", "admin", "owner", "ngasal"} {
		body := `{"name":"Budi","username":"budi","password":"rahasia123","role":"` + role + `"}`
		if w := callUsers(auth, http.MethodPost, "/users", body); w.Code != 400 {
			t.Fatalf("role %q harus ditolak 400, dapat %d", role, w.Code)
		}
	}
}

// Password kurang dari 8 karakter ditolak.
func TestCreateUserTolakPasswordPendek(t *testing.T) {
	setupUserTestDB(t)
	auth := fakeSuperAdmin(1, testUserTenant)

	body := `{"name":"Budi","username":"budi","password":"1234567","role":"cs"}`
	w := callUsers(auth, http.MethodPost, "/users", body)
	if w.Code != 400 {
		t.Fatalf("password 7 karakter harus ditolak 400, dapat %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "minimal 8 karakter") {
		t.Fatalf("pesan error harus menyebut minimal 8 karakter, dapat %s", w.Body.String())
	}
}

// Username kosong / berspasi ditolak sebelum menyentuh DB.
func TestCreateUserTolakUsernameTidakValid(t *testing.T) {
	setupUserTestDB(t)
	auth := fakeSuperAdmin(1, testUserTenant)

	for _, username := range []string{"", "   ", "budi cs"} {
		body := `{"name":"Budi","username":"` + username + `","password":"rahasia123","role":"cs"}`
		if w := callUsers(auth, http.MethodPost, "/users", body); w.Code != 400 {
			t.Fatalf("username %q harus ditolak 400, dapat %d", username, w.Code)
		}
	}
}

// Username yang sudah dipakai dibalas 409, bukan 500 dari unique index.
func TestCreateUserUsernameSudahDipakai(t *testing.T) {
	setupUserTestDB(t)
	auth := fakeSuperAdmin(1, testUserTenant)
	seedUser(t, models.User{Username: "budi", Name: "Budi", Role: models.RoleCS, Active: true})

	// Huruf besar/spasi tetap dianggap username yang sama.
	body := `{"name":"Budi Lagi","username":" BUDI ","password":"rahasia123","role":"cs"}`
	w := callUsers(auth, http.MethodPost, "/users", body)
	if w.Code != 409 {
		t.Fatalf("username duplikat harus 409, dapat %d (%s)", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "Username sudah dipakai") {
		t.Fatalf("pesan error tidak sesuai kontrak: %s", w.Body.String())
	}
}

// Jalur sukses: password ter-hash, tenant ikut pembuat, akun langsung aktif & terverifikasi,
// fitur ngawur dibuang, dan respons tidak membocorkan hash password.
func TestCreateUserSukses(t *testing.T) {
	setupUserTestDB(t)
	auth := fakeSuperAdmin(1, testUserTenant)

	body := `{"name":"Sinta","username":" Sinta ","email":" sinta@contoh.id ","password":"rahasia123","role":"manager","features":["ai","ngawur","AI"]}`
	w := callUsers(auth, http.MethodPost, "/users", body)
	if w.Code != 201 {
		t.Fatalf("create user harus 201, dapat %d (%s)", w.Code, w.Body.String())
	}

	saved := mustLoadUser(t, 1)
	if saved.Username != "sinta" {
		t.Fatalf("username harus dinormalisasi jadi \"sinta\", dapat %q", saved.Username)
	}
	if saved.Email != "sinta@contoh.id" {
		t.Fatalf("email harus disimpan ter-trim, dapat %q", saved.Email)
	}
	if saved.Password == "rahasia123" || !strings.HasPrefix(saved.Password, "$2") {
		t.Fatalf("password wajib di-hash bcrypt, dapat %q", saved.Password)
	}
	if saved.IsSuperAdmin || !saved.Active || !saved.EmailVerified {
		t.Fatalf("user baru harus aktif + email terverifikasi + bukan super admin, dapat %+v", saved)
	}
	if saved.TenantID == nil || *saved.TenantID != testUserTenant {
		t.Fatalf("tenant user baru harus ikut pembuat (%d), dapat %v", testUserTenant, saved.TenantID)
	}
	if saved.Features != models.FeatureAI {
		t.Fatalf("fitur tak dikenal harus dibuang, Features = %q", saved.Features)
	}
	if strings.Contains(w.Body.String(), saved.Password) || strings.Contains(w.Body.String(), "password") {
		t.Fatalf("respons tidak boleh membocorkan password: %s", w.Body.String())
	}
}

// --- UpdateUser ---------------------------------------------------------------

// Super admin tidak boleh diedit lewat endpoint ini.
func TestUpdateUserTolakSuperAdmin(t *testing.T) {
	setupUserTestDB(t)
	auth := fakeSuperAdmin(1, testUserTenant)
	super := seedUser(t, models.User{Username: "super", Name: "Super", Role: "admin", IsSuperAdmin: true})

	body := `{"name":"Dibajak","role":"cs","active":false}`
	w := callUsers(auth, http.MethodPut, "/users/1", body)
	if w.Code != 400 {
		t.Fatalf("edit super admin harus ditolak 400, dapat %d (%s)", w.Code, w.Body.String())
	}
	after := mustLoadUser(t, super.ID)
	if after.Name != "Super" || after.Role != "admin" {
		t.Fatalf("data super admin tidak boleh berubah, dapat %+v", after)
	}
}

// Menaikkan user biasa jadi superadmin lewat update juga ditolak.
func TestUpdateUserTolakNaikJadiSuperAdmin(t *testing.T) {
	setupUserTestDB(t)
	auth := fakeSuperAdmin(1, testUserTenant)
	target := seedUser(t, models.User{Username: "budi", Name: "Budi", Role: models.RoleCS, Active: true})

	w := callUsers(auth, http.MethodPut, "/users/1", `{"role":"superadmin"}`)
	if w.Code != 400 {
		t.Fatalf("promosi ke superadmin harus ditolak 400, dapat %d (%s)", w.Code, w.Body.String())
	}
	if after := mustLoadUser(t, target.ID); after.Role != models.RoleCS || after.IsSuperAdmin {
		t.Fatalf("role user tidak boleh berubah, dapat %+v", after)
	}
}

// User milik tenant lain tidak kelihatan sama sekali (404, bukan 400/200).
func TestUpdateUserTenantLainTidakDitemukan(t *testing.T) {
	setupUserTestDB(t)
	auth := fakeSuperAdmin(1, testUserTenant)
	lain := uint(99)
	seedUser(t, models.User{Username: "tetangga", Name: "Tetangga", Role: models.RoleCS, Active: true, TenantID: &lain})

	if w := callUsers(auth, http.MethodPut, "/users/1", `{"name":"Diganti"}`); w.Code != 404 {
		t.Fatalf("user tenant lain harus 404, dapat %d (%s)", w.Code, w.Body.String())
	}
}

// Update parsial (cuma toggle aktif) tidak boleh menghapus nama, role, atau fitur.
func TestUpdateUserParsialTidakMenghapusData(t *testing.T) {
	setupUserTestDB(t)
	auth := fakeSuperAdmin(1, testUserTenant)
	target := seedUser(t, models.User{
		Username: "sinta", Name: "Sinta", Role: models.RoleManager,
		Active: true, Features: models.FeatureAI,
	})

	if w := callUsers(auth, http.MethodPut, "/users/1", `{"active":false}`); w.Code != 200 {
		t.Fatalf("update aktif harus 200, dapat %d (%s)", w.Code, w.Body.String())
	}
	after := mustLoadUser(t, target.ID)
	if after.Active {
		t.Fatal("user harus jadi nonaktif")
	}
	if after.Name != "Sinta" || after.Role != models.RoleManager || after.Features != models.FeatureAI {
		t.Fatalf("field lain tidak boleh ikut terhapus, dapat %+v", after)
	}
}

// --- ResetUserPassword --------------------------------------------------------

// Password super admin hanya lewat .env, bukan lewat endpoint ini.
func TestResetPasswordTolakSuperAdmin(t *testing.T) {
	setupUserTestDB(t)
	auth := fakeSuperAdmin(1, testUserTenant)
	super := seedUser(t, models.User{Username: "super", Name: "Super", Role: "admin", IsSuperAdmin: true, Password: "hash-lama"})

	w := callUsers(auth, http.MethodPost, "/users/1/password", `{"new_password":"rahasia123"}`)
	if w.Code != 400 {
		t.Fatalf("reset password super admin harus ditolak 400, dapat %d (%s)", w.Code, w.Body.String())
	}
	if after := mustLoadUser(t, super.ID); after.Password != "hash-lama" {
		t.Fatal("password super admin tidak boleh berubah")
	}
}

// Password baru kurang dari 8 karakter ditolak dan tidak ikut tersimpan.
func TestResetPasswordTolakKurangDari8(t *testing.T) {
	setupUserTestDB(t)
	auth := fakeSuperAdmin(1, testUserTenant)
	target := seedUser(t, models.User{Username: "budi", Name: "Budi", Role: models.RoleCS, Active: true, Password: "hash-lama"})

	w := callUsers(auth, http.MethodPost, "/users/1/password", `{"new_password":"1234567"}`)
	if w.Code != 400 {
		t.Fatalf("password 7 karakter harus ditolak 400, dapat %d", w.Code)
	}
	if after := mustLoadUser(t, target.ID); after.Password != "hash-lama" {
		t.Fatal("password lama tidak boleh tertimpa saat validasi gagal")
	}
}

// Jalur sukses reset: tersimpan dalam bentuk hash, bukan teks polos.
func TestResetPasswordSukses(t *testing.T) {
	setupUserTestDB(t)
	auth := fakeSuperAdmin(1, testUserTenant)
	target := seedUser(t, models.User{Username: "budi", Name: "Budi", Role: models.RoleCS, Active: true, Password: "hash-lama"})

	w := callUsers(auth, http.MethodPost, "/users/1/password", `{"new_password":"rahasia123"}`)
	if w.Code != 200 {
		t.Fatalf("reset password harus 200, dapat %d (%s)", w.Code, w.Body.String())
	}
	after := mustLoadUser(t, target.ID)
	if after.Password == "hash-lama" || after.Password == "rahasia123" || !strings.HasPrefix(after.Password, "$2") {
		t.Fatalf("password baru wajib di-hash bcrypt, dapat %q", after.Password)
	}
}

// --- DeleteUser ---------------------------------------------------------------

// Super admin tidak boleh menghapus akunnya sendiri (nanti tidak ada yang bisa masuk lagi).
func TestDeleteUserTolakDiriSendiri(t *testing.T) {
	setupUserTestDB(t)
	sendiri := seedUser(t, models.User{Username: "budi", Name: "Budi", Role: models.RoleManager, Active: true})
	auth := fakeSuperAdmin(sendiri.ID, testUserTenant)

	w := callUsers(auth, http.MethodDelete, "/users/1", "")
	if w.Code != 400 {
		t.Fatalf("hapus diri sendiri harus ditolak 400, dapat %d (%s)", w.Code, w.Body.String())
	}
	mustLoadUser(t, sendiri.ID) // masih ada; kalau terhapus, helper ini yang gagal
}

// Akun super admin tidak boleh dihapus dari halaman ini.
func TestDeleteUserTolakSuperAdmin(t *testing.T) {
	setupUserTestDB(t)
	auth := fakeSuperAdmin(9, testUserTenant)
	super := seedUser(t, models.User{Username: "super", Name: "Super", Role: "admin", IsSuperAdmin: true})

	w := callUsers(auth, http.MethodDelete, "/users/1", "")
	if w.Code != 400 {
		t.Fatalf("hapus super admin harus ditolak 400, dapat %d (%s)", w.Code, w.Body.String())
	}
	mustLoadUser(t, super.ID)
}

// --- ListUsers ----------------------------------------------------------------

// Daftar dibungkus {"data": [...]}, super admin di baris pertama, dan tanpa hash password.
func TestListUsersUrutanDanTidakBocorkanPassword(t *testing.T) {
	setupUserTestDB(t)
	auth := fakeSuperAdmin(2, testUserTenant)
	seedUser(t, models.User{Username: "budi", Name: "Budi", Role: models.RoleCS, Active: true, Password: "hash-budi"})
	seedUser(t, models.User{Username: "super", Name: "Super", Role: "admin", IsSuperAdmin: true, Password: "hash-super"})
	lain := uint(99)
	seedUser(t, models.User{Username: "tetangga", Name: "Tetangga", Role: models.RoleCS, Active: true, TenantID: &lain})

	w := callUsers(auth, http.MethodGet, "/users", "")
	if w.Code != 200 {
		t.Fatalf("list user harus 200, dapat %d (%s)", w.Code, w.Body.String())
	}
	var resp struct {
		Data []struct {
			Username     string `json:"username"`
			IsSuperAdmin bool   `json:"is_super_admin"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("respons bukan JSON {\"data\": [...]}: %v (%s)", err, w.Body.String())
	}
	if len(resp.Data) != 2 {
		t.Fatalf("user tenant lain tidak boleh ikut, dapat %d baris (%s)", len(resp.Data), w.Body.String())
	}
	if !resp.Data[0].IsSuperAdmin || resp.Data[0].Username != "super" {
		t.Fatalf("super admin harus di baris pertama, dapat %+v", resp.Data)
	}
	for _, bocor := range []string{"hash-budi", "hash-super", "password"} {
		if strings.Contains(w.Body.String(), bocor) {
			t.Fatalf("respons membocorkan %q: %s", bocor, w.Body.String())
		}
	}
}
