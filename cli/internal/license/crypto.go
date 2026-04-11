package license

import (
	_ "embed"
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

//go:embed keys/public.pem
var publicKeyPEM []byte

// LicenseClaims represents the payload of a bravros license JWT.
type LicenseClaims struct {
	LicenseKey  string `json:"license_key"`
	MachineID   string `json:"machine_id"`
	Tier        string `json:"tier"` // "trial" | "free" | "pro"
	ExpiresAt   int64  `json:"expires_at"`
	ActivatedAt int64  `json:"activated_at"`

	jwt.RegisteredClaims
}

// GetExpirationTime implements jwt.Claims.
func (c LicenseClaims) GetExpirationTime() (*jwt.NumericDate, error) {
	if c.ExpiresAt == 0 {
		return nil, nil
	}
	return jwt.NewNumericDate(time.Unix(c.ExpiresAt, 0)), nil
}

// GetIssuedAt implements jwt.Claims.
func (c LicenseClaims) GetIssuedAt() (*jwt.NumericDate, error) {
	return c.RegisteredClaims.GetIssuedAt()
}

// GetNotBefore implements jwt.Claims.
func (c LicenseClaims) GetNotBefore() (*jwt.NumericDate, error) {
	return c.RegisteredClaims.GetNotBefore()
}

// GetIssuer implements jwt.Claims.
func (c LicenseClaims) GetIssuer() (string, error) {
	return c.RegisteredClaims.GetIssuer()
}

// GetSubject implements jwt.Claims.
func (c LicenseClaims) GetSubject() (string, error) {
	return c.RegisteredClaims.GetSubject()
}

// GetAudience implements jwt.Claims.
func (c LicenseClaims) GetAudience() (jwt.ClaimStrings, error) {
	return c.RegisteredClaims.GetAudience()
}

// VerifyToken parses and verifies a license JWT using the embedded ES256 public key.
// Returns the parsed LicenseClaims on success, or an error if verification fails.
func VerifyToken(tokenString string) (*LicenseClaims, error) {
	pubKey, err := parseECPublicKey(publicKeyPEM)
	if err != nil {
		return nil, fmt.Errorf("license: failed to load public key: %w", err)
	}

	claims := &LicenseClaims{}
	token, err := jwt.ParseWithClaims(tokenString, claims, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodECDSA); !ok {
			return nil, fmt.Errorf("license: unexpected signing method: %v", t.Header["alg"])
		}
		return pubKey, nil
	}, jwt.WithValidMethods([]string{"ES256"}))
	if err != nil {
		return nil, fmt.Errorf("license: token verification failed: %w", err)
	}
	if !token.Valid {
		return nil, fmt.Errorf("license: token is not valid")
	}
	return claims, nil
}

// parseECPublicKey decodes a PEM-encoded EC public key.
func parseECPublicKey(pemData []byte) (*ecdsa.PublicKey, error) {
	block, _ := pem.Decode(pemData)
	if block == nil {
		return nil, fmt.Errorf("license: failed to decode PEM block")
	}
	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("license: failed to parse public key: %w", err)
	}
	ecPub, ok := pub.(*ecdsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("license: public key is not ECDSA")
	}
	return ecPub, nil
}
