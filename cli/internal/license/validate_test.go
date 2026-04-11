package license

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// signExpiredToken creates a signed JWT with an expiry in the past.
func signExpiredToken(t *testing.T, hoursAgo int) string {
	t.Helper()
	past := time.Now().Add(-time.Duration(hoursAgo) * time.Hour)
	claims := &LicenseClaims{
		LicenseKey:  "ABCD-1234-EF56-7890",
		MachineID:   "test-machine",
		Tier:        "pro",
		ExpiresAt:   past.Unix(),
		ActivatedAt: past.Add(-24 * time.Hour).Unix(),
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "bravros",
			Subject:   "license",
			IssuedAt:  jwt.NewNumericDate(past.Add(-24 * time.Hour)),
			ExpiresAt: jwt.NewNumericDate(past),
		},
	}
	return signTestToken(t, claims)
}

// signFreshToken creates a valid, non-expired signed JWT.
func signFreshToken(t *testing.T, tier string) string {
	t.Helper()
	now := time.Now()
	claims := &LicenseClaims{
		LicenseKey:  "ABCD-1234-EF56-7890",
		MachineID:   "test-machine",
		Tier:        tier,
		ExpiresAt:   now.Add(24 * time.Hour).Unix(),
		ActivatedAt: now.Add(-time.Hour).Unix(),
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "bravros",
			Subject:   "license",
			IssuedAt:  jwt.NewNumericDate(now.Add(-time.Hour)),
			ExpiresAt: jwt.NewNumericDate(now.Add(24 * time.Hour)),
		},
	}
	return signTestToken(t, claims)
}

func TestCheck_NotActivated(t *testing.T) {
	withTempHome(t, func() {
		// No token in cache.
		claims, err := CheckWithClient(NewClient("http://localhost"))
		if !errors.Is(err, ErrNotActivated) {
			t.Errorf("CheckWithClient() error = %v, want ErrNotActivated", err)
		}
		if claims != nil {
			t.Errorf("CheckWithClient() claims = %v, want nil", claims)
		}
	})
}

func TestCheck_TrialValid(t *testing.T) {
	withTempHome(t, func() {
		token := signFreshToken(t, "trial")
		if err := SaveToken(token); err != nil {
			t.Fatalf("SaveToken() error = %v", err)
		}

		claims, err := CheckWithClient(NewClient("http://localhost"))
		if err != nil {
			t.Fatalf("CheckWithClient() error = %v", err)
		}
		if claims == nil {
			t.Fatal("CheckWithClient() returned nil claims")
		}
		if claims.Tier != "trial" {
			t.Errorf("claims.Tier = %q, want %q", claims.Tier, "trial")
		}
	})
}

func TestCheck_ProValid(t *testing.T) {
	withTempHome(t, func() {
		token := signFreshToken(t, "pro")
		if err := SaveToken(token); err != nil {
			t.Fatalf("SaveToken() error = %v", err)
		}

		claims, err := CheckWithClient(NewClient("http://localhost"))
		if err != nil {
			t.Fatalf("CheckWithClient() error = %v", err)
		}
		if claims == nil {
			t.Fatal("CheckWithClient() returned nil claims")
		}
		if claims.Tier != "pro" {
			t.Errorf("claims.Tier = %q, want %q", claims.Tier, "pro")
		}
	})
}

func TestCheck_ExpiredWithinGrace_OfflineOk(t *testing.T) {
	withTempHome(t, func() {
		// Expired 1 hour ago — within 30-day grace.
		token := signExpiredToken(t, 1)
		if err := SaveToken(token); err != nil {
			t.Fatalf("SaveToken() error = %v", err)
		}

		// Use a server that returns a network error (closes immediately).
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			hj, ok := w.(http.Hijacker)
			if !ok {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			conn, _, _ := hj.Hijack()
			conn.Close()
		}))
		defer srv.Close()

		client := NewClient(srv.URL)
		claims, err := CheckWithClient(client)
		if err != nil {
			t.Fatalf("CheckWithClient() offline grace error = %v, want nil", err)
		}
		if claims == nil {
			t.Fatal("CheckWithClient() returned nil claims for offline grace")
		}
		if claims.Tier != "pro" {
			t.Errorf("claims.Tier = %q, want %q", claims.Tier, "pro")
		}
	})
}

func TestCheck_ExpiredWithinGrace_RefreshSuccess(t *testing.T) {
	withTempHome(t, func() {
		// Expired 1 hour ago — within 30-day grace.
		expiredToken := signExpiredToken(t, 1)
		if err := SaveToken(expiredToken); err != nil {
			t.Fatalf("SaveToken() error = %v", err)
		}

		// The refreshed token is fresh.
		freshToken := signFreshToken(t, "pro")

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/verify" {
				t.Errorf("unexpected path: %s", r.URL.Path)
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{"token": freshToken})
		}))
		defer srv.Close()

		client := NewClient(srv.URL)
		claims, err := CheckWithClient(client)
		if err != nil {
			t.Fatalf("CheckWithClient() refresh error = %v", err)
		}
		if claims == nil {
			t.Fatal("CheckWithClient() returned nil claims after refresh")
		}
		// Verify the new token was saved.
		saved, loadErr := LoadToken()
		if loadErr != nil {
			t.Fatalf("LoadToken() after refresh error = %v", loadErr)
		}
		if saved != freshToken {
			t.Errorf("saved token mismatch after refresh")
		}
	})
}

func TestCheck_ExpiredPastGrace(t *testing.T) {
	withTempHome(t, func() {
		// Expired 31 days ago — past grace period.
		token := signExpiredToken(t, 31*24)
		if err := SaveToken(token); err != nil {
			t.Fatalf("SaveToken() error = %v", err)
		}

		claims, err := CheckWithClient(NewClient("http://localhost"))
		if !errors.Is(err, ErrExpired) {
			t.Errorf("CheckWithClient() error = %v, want ErrExpired", err)
		}
		if claims != nil {
			t.Errorf("CheckWithClient() claims = %v, want nil", claims)
		}
	})
}

func TestValidateLicenseKey(t *testing.T) {
	tests := []struct {
		key  string
		want bool
	}{
		{"ABCD-1234-EF56-7890", true},
		{"abcd-1234-ef56-7890", true},
		{"ABCD-1234-EF56-789G", false}, // G is not hex
		{"ABCD-1234-EF56", false},      // too short
		{"ABCD-1234-EF56-7890-1234", false}, // too long
		{"", false},
		{"ABCD1234EF567890", false}, // no dashes
		{"XXXX-YYYY-ZZZZ-WWWW", false}, // non-hex letters
	}

	for _, tt := range tests {
		got := ValidateLicenseKey(tt.key)
		if got != tt.want {
			t.Errorf("ValidateLicenseKey(%q) = %v, want %v", tt.key, got, tt.want)
		}
	}
}
