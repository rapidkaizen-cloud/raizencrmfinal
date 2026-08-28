package handlers

import (
	"slices"

	"github.com/gin-gonic/gin"
)

// RequireFeature memblokir user yang tidak punya fitur tsb. Super admin selalu lolos.
// WAJIB dipasang SETELAH AuthMiddleware: middleware ini tidak membaca JWT maupun DB,
// ia hanya membaca flag "is_super_admin" dan daftar "features" yang ditaruh AuthMiddleware
// ke context. Kalau dipasang duluan (atau tanpa AuthMiddleware sama sekali), context masih
// kosong sehingga SEMUA request — termasuk milik super admin — akan ditolak 403.
//
// Dipakai untuk mengunci menu berbayar/berisiko (mis. fitur "ai" dan section "akun")
// agar role manager/cs tidak bisa mengubah persona, knowledge, atau konfigurasi akun
// selama super admin belum meng-grant fiturnya.
func RequireFeature(feature string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !hasFeature(c, feature) {
			c.AbortWithStatusJSON(403, gin.H{"error": "Fitur ini tidak tersedia untuk akun kamu"})
			return
		}
		c.Next()
	}
}

// currentUserID = id user pemilik request (0 kalau belum lewat AuthMiddleware).
func currentUserID(c *gin.Context) uint {
	if v, ok := c.Get("user_id"); ok {
		if id, ok := v.(uint); ok {
			return id
		}
	}
	return 0
}

// isSuperAdmin membaca flag yang ditaruh AuthMiddleware.
// Jangan pakai string Role untuk ini — super admin lama di DB masih ber-Role "admin".
func isSuperAdmin(c *gin.Context) bool {
	isSuper, _ := c.Get("is_super_admin")
	return isSuper == true
}

// hasFeature mengecek satu fitur pada user yang sedang login.
// Super admin selalu true walau kolom Features-nya kosong.
func hasFeature(c *gin.Context, feature string) bool {
	if isSuperAdmin(c) {
		return true
	}
	v, ok := c.Get("features")
	if !ok {
		return false
	}
	list, ok := v.([]string)
	if !ok {
		return false
	}
	return slices.Contains(list, feature)
}
