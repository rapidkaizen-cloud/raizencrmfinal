package services

import "testing"

// TestValidatePhoneForWALength mengunci aturan panjang nomor Indonesia: yang dihitung
// adalah digit setelah kode negara (9–12), sehingga nomor nasional 13 digit ("08xx…")
// tetap lolos meski setelah normalisasi jadi 14 digit ("628xx…").
func TestValidatePhoneForWALength(t *testing.T) {
	cases := []struct {
		raw  string
		want bool
	}{
		{"08123456789", true},   // nasional 11 digit -> 62 + 10
		{"081234567890", true},  // nasional 12 digit -> 62 + 11
		{"0812345678901", true}, // nasional 13 digit -> 62 + 12 (kasus yang dulu ditolak)
		{"+62 812-3456-78901", true},
		{"0812345678", true},      // nasional 10 digit -> 62 + 9
		{"081234567", false},      // terlalu pendek
		{"08123456789012", false}, // terlalu panjang
		{"", false},
		{"6591234567", true},        // negara lain, 10 digit
		{"6512345678901234", false}, // negara lain, 16 digit
	}
	for _, c := range cases {
		norm := NormalizePhone(c.raw)
		got, reason := ValidatePhoneForWA(norm)
		if got != c.want {
			t.Errorf("ValidatePhoneForWA(%q -> %q) = %v (%s), mau %v", c.raw, norm, got, reason, c.want)
		}
	}
}
