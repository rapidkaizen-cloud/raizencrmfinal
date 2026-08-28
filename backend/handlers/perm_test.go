package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"wa-assistant/backend/models"

	"github.com/gin-gonic/gin"
)

// fakeAuth meniru context yang ditaruh AuthMiddleware, tanpa perlu JWT & DB asli.
func fakeAuth(userID uint, super bool, features []string) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set("user_id", userID)
		c.Set("is_super_admin", super)
		c.Set("features", features)
		c.Next()
	}
}

// runFeatureRoute memasang RequireFeature di belakang middleware auth palsu,
// lalu memanggil route-nya sekali.
func runFeatureRoute(auth gin.HandlerFunc, feature string) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	chain := []gin.HandlerFunc{}
	if auth != nil {
		chain = append(chain, auth)
	}
	chain = append(chain, RequireFeature(feature), func(c *gin.Context) {
		c.JSON(200, gin.H{"ok": true})
	})
	r.GET("/coba", chain...)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/coba", nil))
	return w
}

// Super admin lolos semua fitur walau kolom Features-nya kosong.
func TestRequireFeatureSuperAdminSelaluLolos(t *testing.T) {
	super := fakeAuth(1, true, nil)
	for _, f := range []string{models.FeatureAI, models.FeatureAkun} {
		if code := runFeatureRoute(super, f).Code; code != 200 {
			t.Fatalf("super admin harus lolos RequireFeature(%q), dapat %d", f, code)
		}
	}
}

// User biasa hanya lolos fitur yang di-grant; fitur lain ditolak 403.
func TestRequireFeatureUserBiasa(t *testing.T) {
	// Manager yang cuma dapat grant "ai".
	manager := fakeAuth(7, false, []string{models.FeatureAI})

	if code := runFeatureRoute(manager, models.FeatureAI).Code; code != 200 {
		t.Fatalf("user dengan fitur %q harus lolos, dapat %d", models.FeatureAI, code)
	}
	if code := runFeatureRoute(manager, models.FeatureAkun).Code; code != 403 {
		t.Fatalf("user tanpa fitur %q harus ditolak 403, dapat %d", models.FeatureAkun, code)
	}
}

// Tanpa AuthMiddleware context kosong, jadi semua request ditolak.
// Ini yang bikin RequireFeature WAJIB dipasang SETELAH AuthMiddleware.
func TestRequireFeatureTanpaAuthDitolak(t *testing.T) {
	if code := runFeatureRoute(nil, models.FeatureAI).Code; code != 403 {
		t.Fatalf("tanpa AuthMiddleware harus ditolak 403, dapat %d", code)
	}
}

// currentUserID & isSuperAdmin membaca context, dan aman saat context masih kosong.
func TestHelperContextKosong(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	if id := currentUserID(c); id != 0 {
		t.Fatalf("currentUserID pada context kosong harus 0, dapat %d", id)
	}
	if isSuperAdmin(c) {
		t.Fatal("isSuperAdmin pada context kosong harus false")
	}

	c.Set("user_id", uint(9))
	c.Set("is_super_admin", true)
	if id := currentUserID(c); id != 9 {
		t.Fatalf("currentUserID = %d, ingin 9", id)
	}
	if !isSuperAdmin(c) {
		t.Fatal("isSuperAdmin harus true setelah flag diset")
	}
}
