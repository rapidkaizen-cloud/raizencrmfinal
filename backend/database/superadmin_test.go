package database

import (
	"testing"

	"wa-assistant/backend/models"

	sqlite "github.com/glebarez/sqlite"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// Ganti username + password sekaligus di .env harus menang atas isi DB.
func TestSyncSuperAdminEnvMenang(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.User{}); err != nil {
		t.Fatal(err)
	}
	old := DB
	DB = db
	t.Cleanup(func() { DB = old })

	lama, _ := bcrypt.GenerateFromPassword([]byte("password-lama-123"), bcrypt.MinCost)
	db.Create(&models.User{Username: "superadmin", Email: "s@x.local", Password: string(lama), IsSuperAdmin: true})

	t.Setenv("SUPERADMIN_USERNAME", "bos")
	t.Setenv("SUPERADMIN_PASSWORD", "password-baru-456")
	syncSuperAdminPassword()

	var u models.User
	db.Where("is_super_admin = ?", true).First(&u)
	if u.Username != "bos" {
		t.Fatalf("username tidak ikut env: %q", u.Username)
	}
	if bcrypt.CompareHashAndPassword([]byte(u.Password), []byte("password-baru-456")) != nil {
		t.Fatal("password tidak ikut env")
	}

	// Password env terlalu pendek → password DB tidak disentuh.
	t.Setenv("SUPERADMIN_PASSWORD", "abc")
	syncSuperAdminPassword()
	db.Where("is_super_admin = ?", true).First(&u)
	if bcrypt.CompareHashAndPassword([]byte(u.Password), []byte("password-baru-456")) != nil {
		t.Fatal("password pendek tidak boleh menimpa")
	}
}
