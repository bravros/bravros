package license

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"time"
)

const gracePeriod = 30 * 24 * time.Hour

// licenseKeyPattern matches keys of the form XXXX-XXXX-XXXX-XXXX (hex groups).
var licenseKeyPattern = regexp.MustCompile(`^[A-Fa-f0-9]{4}-[A-Fa-f0-9]{4}-[A-Fa-f0-9]{4}-[A-Fa-f0-9]{4}$`)

// ValidateLicenseKey returns true if key matches the expected format.
func ValidateLicenseKey(key string) bool {
	return licenseKeyPattern.MatchString(key)
}

// Check performs an offline-first license validation, orchestrating the cache,
// local JWT verification, and optional online refresh.
//
// Flow:
//  1. LoadToken — if ErrNotActivated, return nil, ErrNotActivated.
//  2. VerifyToken — if valid and not expired, return claims.
//  3. If expired but within 30-day grace window, attempt API refresh.
//     - Success: save refreshed token and return new claims.
//     - Network failure: return cached claims (offline-ok).
//  4. If expired past grace period, return nil, ErrExpired.
func Check() (*LicenseClaims, error) {
	return CheckWithClient(DefaultClient)
}

// CheckWithClient is the testable variant of Check that accepts a custom client.
func CheckWithClient(client ClientIface) (*LicenseClaims, error) {
	token, err := LoadToken()
	if err != nil {
		if errors.Is(err, ErrNotActivated) {
			return nil, ErrNotActivated
		}
		return nil, err
	}

	claims, err := VerifyToken(token)
	if err == nil {
		// Token is valid and not expired.
		return claims, nil
	}

	// VerifyToken failed — parse raw claims to determine grace window.
	expiredClaims, parseErr := parseExpiredClaims(token)
	if parseErr != nil {
		// Can't parse the token — treat as not activated.
		return nil, ErrNotActivated
	}

	now := time.Now().Unix()
	graceDeadline := expiredClaims.ExpiresAt + int64(gracePeriod.Seconds())

	if now < graceDeadline {
		// Within grace window — try online refresh.
		newToken, refreshErr := client.Verify(token, MachineID())
		if refreshErr != nil {
			if isNetworkError(refreshErr) {
				// Offline grace: return cached claims.
				return expiredClaims, nil
			}
			// API returned a hard error (e.g. machine_limit) — treat as expired.
			return nil, ErrExpired
		}

		// Refresh succeeded — save and return new claims.
		_ = SaveToken(newToken) // non-fatal if save fails

		newClaims, verifyErr := VerifyToken(newToken)
		if verifyErr != nil {
			return nil, ErrExpired
		}
		return newClaims, nil
	}

	// Past grace period.
	return nil, ErrExpired
}

// parseExpiredClaims parses the payload of a JWT without validating expiry,
// returning the embedded LicenseClaims even if the token has expired.
func parseExpiredClaims(tokenString string) (*LicenseClaims, error) {
	parts := strings.Split(tokenString, ".")
	if len(parts) != 3 {
		return nil, ErrNotActivated
	}

	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, ErrNotActivated
	}

	claims := &LicenseClaims{}
	if err := json.Unmarshal(raw, claims); err != nil {
		return nil, ErrNotActivated
	}
	return claims, nil
}

// isNetworkError returns true if err is a transient network/connection error
// rather than a structured API error.
func isNetworkError(err error) bool {
	if err == nil {
		return false
	}
	var apiErr *APIError
	return !errors.As(err, &apiErr)
}
