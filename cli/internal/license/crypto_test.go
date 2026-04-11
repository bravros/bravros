package license

import (
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/pem"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// loadPrivateKey reads the dev private key from disk (test-only helper).
func loadPrivateKey(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()
	data, err := os.ReadFile("keys/private.pem")
	if err != nil {
		t.Fatalf("failed to read private key: %v", err)
	}
	block, _ := pem.Decode(data)
	if block == nil {
		t.Fatal("failed to decode private key PEM")
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		t.Fatalf("failed to parse private key: %v", err)
	}
	ecKey, ok := key.(*ecdsa.PrivateKey)
	if !ok {
		t.Fatal("private key is not ECDSA")
	}
	return ecKey
}

// signTestToken creates a valid ES256-signed license token for testing.
func signTestToken(t *testing.T, claims *LicenseClaims) string {
	t.Helper()
	privKey := loadPrivateKey(t)
	token := jwt.NewWithClaims(jwt.SigningMethodES256, claims)
	signed, err := token.SignedString(privKey)
	if err != nil {
		t.Fatalf("failed to sign test token: %v", err)
	}
	return signed
}

func validClaims() *LicenseClaims {
	now := time.Now()
	return &LicenseClaims{
		LicenseKey:  "TEST-1234-5678",
		MachineID:   "deadbeef",
		Tier:        "pro",
		ExpiresAt:   now.Add(24 * time.Hour).Unix(),
		ActivatedAt: now.Unix(),
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "bravros",
			Subject:   "license",
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(24 * time.Hour)),
		},
	}
}

func TestVerifyToken_ValidToken(t *testing.T) {
	signed := signTestToken(t, validClaims())

	claims, err := VerifyToken(signed)
	if err != nil {
		t.Fatalf("VerifyToken() error = %v", err)
	}
	if claims.LicenseKey != "TEST-1234-5678" {
		t.Errorf("LicenseKey = %q, want %q", claims.LicenseKey, "TEST-1234-5678")
	}
	if claims.Tier != "pro" {
		t.Errorf("Tier = %q, want %q", claims.Tier, "pro")
	}
	if claims.MachineID != "deadbeef" {
		t.Errorf("MachineID = %q, want %q", claims.MachineID, "deadbeef")
	}
}

func TestVerifyToken_TamperedToken(t *testing.T) {
	signed := signTestToken(t, validClaims())

	// Tamper: replace the entire signature segment with a bogus one.
	// Flipping only the last base64url character is unreliable because
	// base64url padding can make the trailing bits irrelevant, causing
	// some substitutions to decode to an identical byte sequence.
	// Instead we corrupt the middle of the signature to guarantee the
	// decoded bytes differ and the ECDSA verification fails.
	parts := strings.SplitN(signed, ".", 3)
	if len(parts) != 3 {
		t.Fatal("signed token does not have 3 JWT segments")
	}
	sig := []byte(parts[2])
	// Flip several bytes in the middle of the signature segment.
	mid := len(sig) / 2
	for i := mid; i < mid+4 && i < len(sig); i++ {
		if sig[i] == 'A' {
			sig[i] = 'B'
		} else {
			sig[i] = 'A'
		}
	}
	tampered := parts[0] + "." + parts[1] + "." + string(sig)

	_, err := VerifyToken(tampered)
	if err == nil {
		t.Fatal("VerifyToken() expected error for tampered token, got nil")
	}
}

func TestVerifyToken_WrongAlgorithm(t *testing.T) {
	// Sign with HS256 (symmetric), which must be rejected
	claims := validClaims()
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte("some-hmac-secret"))
	if err != nil {
		t.Fatalf("failed to sign HS256 token: %v", err)
	}

	_, err = VerifyToken(signed)
	if err == nil {
		t.Fatal("VerifyToken() expected error for wrong algorithm, got nil")
	}
}

func TestVerifyToken_ExpiredToken(t *testing.T) {
	past := time.Now().Add(-2 * time.Hour)
	claims := &LicenseClaims{
		LicenseKey:  "EXPIRED-KEY",
		MachineID:   "deadbeef",
		Tier:        "trial",
		ExpiresAt:   past.Unix(),
		ActivatedAt: past.Add(-time.Hour).Unix(),
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "bravros",
			Subject:   "license",
			IssuedAt:  jwt.NewNumericDate(past.Add(-time.Hour)),
			ExpiresAt: jwt.NewNumericDate(past),
		},
	}
	signed := signTestToken(t, claims)

	_, err := VerifyToken(signed)
	if err == nil {
		t.Fatal("VerifyToken() expected error for expired token, got nil")
	}
}
