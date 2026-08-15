package fetch

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"fmt"
	"strings"

	"golang.org/x/crypto/blake2b"
)

// pubKey is the key actually consulted by verifyMinisign. It defaults to the
// real pinned MinisignPubKey; tests override it (with a t.Cleanup restore) to
// exercise the verifier against a throwaway keypair without weakening the
// exported constant.
var pubKey = MinisignPubKey

const (
	minisignAlgoEd = "Ed" // pure Ed25519 signature over the raw file content
	minisignAlgoED = "ED" // Ed25519 signature over blake2b-512(file content)

	minisignSigBlobLen = 74 // 2-byte algo + 8-byte key id + 64-byte signature
	minisignPubKeyLen  = 42 // 2-byte algo + 8-byte key id + 32-byte Ed25519 pubkey
)

// verifyMinisign verifies a legacy-format minisign ".minisig" signature
// (sigText) over content, against the base64-encoded public key string
// pubKeyB64 (algo || key id || Ed25519 public key, no comment header).
//
// It checks, in order: the key id embedded in the signature matches the
// pinned public key, the per-algorithm signature over content verifies, and
// the global signature over (signature || trusted comment) verifies — so a
// tampered trusted-comment line is caught too.
func verifyMinisign(content []byte, sigText []byte, pubKeyB64 string) error {
	pubRaw, err := base64.StdEncoding.DecodeString(pubKeyB64)
	if err != nil {
		return fmt.Errorf("decode public key: %w", err)
	}
	if len(pubRaw) != minisignPubKeyLen {
		return fmt.Errorf("public key: unexpected length %d (want %d)", len(pubRaw), minisignPubKeyLen)
	}
	pubKeyID := pubRaw[2:10]
	pubKeyBytes := pubRaw[10:42]

	lines := strings.Split(strings.ReplaceAll(string(sigText), "\r\n", "\n"), "\n")
	if len(lines) < 4 {
		return fmt.Errorf("malformed minisig: expected at least 4 lines, got %d", len(lines))
	}
	sigLine := strings.TrimSpace(lines[1])
	trustedCommentLine := lines[2]
	globalSigLine := strings.TrimSpace(lines[3])

	const trustedPrefix = "trusted comment: "
	if !strings.HasPrefix(trustedCommentLine, trustedPrefix) {
		return fmt.Errorf("malformed minisig: missing trusted comment line")
	}
	trustedComment := strings.TrimPrefix(trustedCommentLine, trustedPrefix)

	sigBlob, err := base64.StdEncoding.DecodeString(sigLine)
	if err != nil {
		return fmt.Errorf("decode signature: %w", err)
	}
	if len(sigBlob) != minisignSigBlobLen {
		return fmt.Errorf("signature: unexpected length %d (want %d)", len(sigBlob), minisignSigBlobLen)
	}
	algo := string(sigBlob[0:2])
	keyID := sigBlob[2:10]
	sig := sigBlob[10:74]

	if !bytes.Equal(keyID, pubKeyID) {
		return fmt.Errorf("signature key id does not match pinned public key")
	}

	var message []byte
	switch algo {
	case minisignAlgoEd:
		message = content
	case minisignAlgoED:
		sum := blake2b.Sum512(content)
		message = sum[:]
	default:
		return fmt.Errorf("unsupported signature algorithm %q", algo)
	}

	if !ed25519.Verify(pubKeyBytes, message, sig) {
		return fmt.Errorf("signature verification failed")
	}

	globalSig, err := base64.StdEncoding.DecodeString(globalSigLine)
	if err != nil {
		return fmt.Errorf("decode global signature: %w", err)
	}
	globalMessage := append(append([]byte{}, sig...), []byte(trustedComment)...)
	if !ed25519.Verify(pubKeyBytes, globalMessage, globalSig) {
		return fmt.Errorf("trusted comment signature verification failed")
	}

	return nil
}
