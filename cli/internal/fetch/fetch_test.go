package fetch

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/blake2b"
)

// ── test key + minisig wire-format helpers ──────────────────────────────────

type testKey struct {
	pub   ed25519.PublicKey
	priv  ed25519.PrivateKey
	keyID [8]byte
}

func generateTestKey(t *testing.T) testKey {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	var keyID [8]byte
	if _, err := rand.Read(keyID[:]); err != nil {
		t.Fatalf("generate key id: %v", err)
	}
	return testKey{pub: pub, priv: priv, keyID: keyID}
}

// pubKeyB64 renders the pinned-key-format base64 string (2-byte algo || 8-byte
// key id || 32-byte Ed25519 public key) for k, or for an overridden key id
// when useKeyID is non-nil (used by the wrong-key-id test).
func pubKeyB64(k testKey) string {
	raw := make([]byte, 0, 42)
	raw = append(raw, 'E', 'd')
	raw = append(raw, k.keyID[:]...)
	raw = append(raw, k.pub...)
	return base64.StdEncoding.EncodeToString(raw)
}

// signMinisig produces the contents of a legacy-format ".minisig" file over
// content, signed by k using the given signature algorithm ("Ed" or "ED").
// signingKeyID lets a test forge a mismatched key id in the signature blob.
func signMinisig(t *testing.T, k testKey, signingKeyID [8]byte, algo string, content []byte, trustedComment string) []byte {
	t.Helper()

	var message []byte
	switch algo {
	case "Ed":
		message = content
	case "ED":
		sum := blake2b.Sum512(content)
		message = sum[:]
	default:
		t.Fatalf("unknown test algo %q", algo)
	}
	sig := ed25519.Sign(k.priv, message)

	sigBlob := make([]byte, 0, 74)
	sigBlob = append(sigBlob, algo[0], algo[1])
	sigBlob = append(sigBlob, signingKeyID[:]...)
	sigBlob = append(sigBlob, sig...)

	globalMessage := append(append([]byte{}, sig...), []byte(trustedComment)...)
	globalSig := ed25519.Sign(k.priv, globalMessage)

	var buf bytes.Buffer
	buf.WriteString("untrusted comment: minisign signature (test fixture)\n")
	buf.WriteString(base64.StdEncoding.EncodeToString(sigBlob))
	buf.WriteString("\n")
	buf.WriteString("trusted comment: " + trustedComment + "\n")
	buf.WriteString(base64.StdEncoding.EncodeToString(globalSig))
	buf.WriteString("\n")
	return buf.Bytes()
}

// ── tarball helpers ──────────────────────────────────────────────────────────

type tarEntry struct {
	name     string
	body     []byte
	typeflag byte
}

func buildTarGz(t *testing.T, entries []tarEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		typeflag := e.typeflag
		if typeflag == 0 {
			typeflag = tar.TypeReg
		}
		hdr := &tar.Header{
			Name:     e.name,
			Mode:     0o644,
			Size:     int64(len(e.body)),
			Typeflag: typeflag,
		}
		if typeflag == tar.TypeSymlink {
			hdr.Linkname = string(e.body)
			hdr.Size = 0
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("write tar header: %v", err)
		}
		if typeflag == tar.TypeReg {
			if _, err := tw.Write(e.body); err != nil {
				t.Fatalf("write tar body: %v", err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("close tar writer: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("close gzip writer: %v", err)
	}
	return buf.Bytes()
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// ── test server ──────────────────────────────────────────────────────────────

type routeResponse struct {
	status int // 0 => 200
	body   []byte
}

func newTestServer(t *testing.T, routes map[string]routeResponse) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	for path, resp := range routes {
		resp := resp
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			if resp.status != 0 {
				w.WriteHeader(resp.status)
			}
			_, _ = w.Write(resp.body)
		})
	}
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts
}

const testTag = "v1.0.0"

func routePath(asset string) string {
	return fmt.Sprintf("/releases/download/%s/%s", testTag, asset)
}

// ── fixture assembly ─────────────────────────────────────────────────────────

type fixture struct {
	payload   []byte
	checksums []byte
	sig       []byte
}

// buildFixture assembles a valid, self-consistent release: a payload tarball,
// its checksums.txt, and a minisig signature over checksums.txt signed by k
// using algo. Tests mutate the returned fixture fields to inject a specific
// failure.
func buildFixture(t *testing.T, k testKey, algo string, entries []tarEntry) fixture {
	t.Helper()
	payload := buildTarGz(t, entries)
	checksums := []byte(fmt.Sprintf("%s  %s\n", sha256Hex(payload), PayloadAsset))
	sig := signMinisig(t, k, k.keyID, algo, checksums, "timestamp:0\tfile:checksums.txt")
	return fixture{payload: payload, checksums: checksums, sig: sig}
}

func (f fixture) routes() map[string]routeResponse {
	return map[string]routeResponse{
		routePath("checksums.txt"):         {body: f.checksums},
		routePath("checksums.txt.minisig"): {body: f.sig},
		routePath(PayloadAsset):            {body: f.payload},
	}
}

// withPubKey overrides the package-level pubKey var for the duration of the
// test, restoring the real pinned constant on cleanup.
func withPubKey(t *testing.T, k testKey) {
	t.Helper()
	orig := pubKey
	pubKey = pubKeyB64(k)
	t.Cleanup(func() { pubKey = orig })
}

func defaultEntries() []tarEntry {
	return []tarEntry{
		{name: "skills/example/SKILL.md", body: []byte("# example skill\n")},
		{name: "templates/CLAUDE.md", body: []byte("# template\n")},
	}
}

// assertDestUntouched verifies destDir still contains exactly the marker file
// it was seeded with, and that no stray staging/backup siblings were left in
// its parent directory.
func assertDestUntouched(t *testing.T, parent, destDir, markerContent string) {
	t.Helper()

	entries, err := os.ReadDir(destDir)
	if err != nil {
		t.Fatalf("read destDir: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "marker.txt" {
		t.Fatalf("destDir contents changed: %+v", entries)
	}
	got, err := os.ReadFile(filepath.Join(destDir, "marker.txt"))
	if err != nil {
		t.Fatalf("read marker: %v", err)
	}
	if string(got) != markerContent {
		t.Fatalf("marker content changed: got %q want %q", got, markerContent)
	}

	parentEntries, err := os.ReadDir(parent)
	if err != nil {
		t.Fatalf("read parent dir: %v", err)
	}
	if len(parentEntries) != 1 || parentEntries[0].Name() != "payload" {
		names := make([]string, len(parentEntries))
		for i, e := range parentEntries {
			names[i] = e.Name()
		}
		t.Fatalf("stray siblings left next to destDir: %v", names)
	}
}

// seedDestDir creates destDir with a single marker file, returning the marker
// content for later comparison.
func seedDestDir(t *testing.T, destDir string) string {
	t.Helper()
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		t.Fatalf("seed destDir: %v", err)
	}
	content := "original-payload-marker"
	if err := os.WriteFile(filepath.Join(destDir, "marker.txt"), []byte(content), 0o644); err != nil {
		t.Fatalf("write marker: %v", err)
	}
	return content
}

func runFailingFetch(t *testing.T, ts *httptest.Server) (parent, destDir string, err error) {
	t.Helper()
	parent = t.TempDir()
	destDir = filepath.Join(parent, "payload")
	marker := seedDestDir(t, destDir)

	c := &Client{BaseURL: ts.URL, HTTP: http.DefaultClient}
	_, err = c.FetchPayload(context.Background(), testTag, destDir)
	if err == nil {
		t.Fatalf("expected FetchPayload to fail, got nil error")
	}
	assertDestUntouched(t, parent, destDir, marker)
	return parent, destDir, err
}

// ── tests ─────────────────────────────────────────────────────────────────

func TestFetchPayload_HappyPath(t *testing.T) {
	k := generateTestKey(t)
	withPubKey(t, k)
	entries := defaultEntries()
	fx := buildFixture(t, k, "Ed", entries)
	ts := newTestServer(t, fx.routes())

	parent := t.TempDir()
	destDir := filepath.Join(parent, "payload")

	c := &Client{BaseURL: ts.URL, HTTP: http.DefaultClient}
	sha, err := c.FetchPayload(context.Background(), testTag, destDir)
	if err != nil {
		t.Fatalf("FetchPayload: %v", err)
	}
	if want := sha256Hex(fx.payload); sha != want {
		t.Fatalf("returned sha256 = %q, want %q", sha, want)
	}

	got, err := os.ReadFile(filepath.Join(destDir, "skills/example/SKILL.md"))
	if err != nil {
		t.Fatalf("read extracted skill file: %v", err)
	}
	if string(got) != "# example skill\n" {
		t.Fatalf("extracted content mismatch: %q", got)
	}
	got, err = os.ReadFile(filepath.Join(destDir, "templates/CLAUDE.md"))
	if err != nil {
		t.Fatalf("read extracted template file: %v", err)
	}
	if string(got) != "# template\n" {
		t.Fatalf("extracted content mismatch: %q", got)
	}

	// No staging/backup siblings should remain.
	parentEntries, err := os.ReadDir(parent)
	if err != nil {
		t.Fatalf("read parent: %v", err)
	}
	if len(parentEntries) != 1 || parentEntries[0].Name() != "payload" {
		t.Fatalf("stray siblings after success: %+v", parentEntries)
	}
}

func TestFetchPayload_HappyPath_ExistingDestDir(t *testing.T) {
	// Covers the atomicSwap replace-existing-destDir branch on the success path.
	k := generateTestKey(t)
	withPubKey(t, k)
	fx := buildFixture(t, k, "Ed", defaultEntries())
	ts := newTestServer(t, fx.routes())

	parent := t.TempDir()
	destDir := filepath.Join(parent, "payload")
	seedDestDir(t, destDir)

	c := &Client{BaseURL: ts.URL, HTTP: http.DefaultClient}
	if _, err := c.FetchPayload(context.Background(), testTag, destDir); err != nil {
		t.Fatalf("FetchPayload: %v", err)
	}

	if _, err := os.Stat(filepath.Join(destDir, "marker.txt")); !os.IsNotExist(err) {
		t.Fatalf("expected old marker.txt to be replaced, err=%v", err)
	}
	if _, err := os.ReadFile(filepath.Join(destDir, "skills/example/SKILL.md")); err != nil {
		t.Fatalf("expected new payload content: %v", err)
	}

	parentEntries, err := os.ReadDir(parent)
	if err != nil {
		t.Fatalf("read parent: %v", err)
	}
	if len(parentEntries) != 1 || parentEntries[0].Name() != "payload" {
		t.Fatalf("old/staging siblings not cleaned up: %+v", parentEntries)
	}
}

func TestFetchPayload_AlgoED_BlakeVariant(t *testing.T) {
	k := generateTestKey(t)
	withPubKey(t, k)
	fx := buildFixture(t, k, "ED", defaultEntries())
	ts := newTestServer(t, fx.routes())

	parent := t.TempDir()
	destDir := filepath.Join(parent, "payload")

	c := &Client{BaseURL: ts.URL, HTTP: http.DefaultClient}
	sha, err := c.FetchPayload(context.Background(), testTag, destDir)
	if err != nil {
		t.Fatalf("FetchPayload: %v", err)
	}
	if want := sha256Hex(fx.payload); sha != want {
		t.Fatalf("returned sha256 = %q, want %q", sha, want)
	}
}

func TestFetchPayload_NetworkError(t *testing.T) {
	ts := newTestServer(t, map[string]routeResponse{})
	ts.Close() // server is now unreachable — connection refused

	parent := t.TempDir()
	destDir := filepath.Join(parent, "payload")
	marker := seedDestDir(t, destDir)

	c := &Client{BaseURL: ts.URL, HTTP: http.DefaultClient}
	_, err := c.FetchPayload(context.Background(), testTag, destDir)
	if err == nil {
		t.Fatalf("expected network error, got nil")
	}
	assertDestUntouched(t, parent, destDir, marker)
}

// TestFetchPayloadMissingAssetReturnsErrNoPayload covers every release cut
// before P-0003's payload asset shipped: checksums.txt/.minisig download
// fine, but the payload itself 404s. That must surface as the identifiable
// ErrNoPayload sentinel — "nothing to fetch", not a generic failure.
func TestFetchPayloadMissingAssetReturnsErrNoPayload(t *testing.T) {
	k := generateTestKey(t)
	withPubKey(t, k)
	fx := buildFixture(t, k, "Ed", defaultEntries())
	routes := fx.routes()
	routes[routePath(PayloadAsset)] = routeResponse{status: http.StatusNotFound}
	ts := newTestServer(t, routes)

	_, _, err := runFailingFetch(t, ts)
	if !errors.Is(err, ErrNoPayload) {
		t.Fatalf("expected errors.Is(err, ErrNoPayload), got: %v", err)
	}
}

func TestFetchPayload_ChecksumsNotFound(t *testing.T) {
	k := generateTestKey(t)
	withPubKey(t, k)
	fx := buildFixture(t, k, "Ed", defaultEntries())
	routes := fx.routes()
	routes[routePath("checksums.txt")] = routeResponse{status: http.StatusNotFound}
	ts := newTestServer(t, routes)

	_, _, err := runFailingFetch(t, ts)
	if !strings.Contains(err.Error(), "404") {
		t.Fatalf("expected 404 in error, got: %v", err)
	}
}

func TestFetchPayload_CorruptGzip(t *testing.T) {
	k := generateTestKey(t)
	withPubKey(t, k)
	entries := defaultEntries()
	fx := buildFixture(t, k, "Ed", entries)
	// Truncate the payload after the checksum/signature were computed over the
	// original bytes so it still passes verification but fails to decompress.
	fx.payload = fx.payload[:len(fx.payload)/2]
	ts := newTestServer(t, fx.routes())

	_, _, err := runFailingFetch(t, ts)
	if err == nil {
		t.Fatalf("expected corrupt gzip error")
	}
}

func TestFetchPayload_ChecksumMismatch(t *testing.T) {
	k := generateTestKey(t)
	withPubKey(t, k)
	fx := buildFixture(t, k, "Ed", defaultEntries())
	// Swap in a different, still-valid tarball after checksums.txt was signed —
	// the signature is valid but the sha256 no longer matches.
	fx.payload = buildTarGz(t, []tarEntry{{name: "skills/other.md", body: []byte("different\n")}})
	ts := newTestServer(t, fx.routes())

	_, _, err := runFailingFetch(t, ts)
	if !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("expected checksum mismatch error, got: %v", err)
	}
}

func TestFetchPayload_SignatureMismatch(t *testing.T) {
	k := generateTestKey(t)
	withPubKey(t, k)
	fx := buildFixture(t, k, "Ed", defaultEntries())
	// Tamper checksums.txt AFTER signing.
	fx.checksums = append(fx.checksums, []byte("tampered  extra-line\n")...)
	ts := newTestServer(t, fx.routes())

	_, _, err := runFailingFetch(t, ts)
	if !strings.Contains(err.Error(), "signature verification failed") {
		t.Fatalf("expected signature verification failure, got: %v", err)
	}
}

func TestFetchPayload_WrongKeyID(t *testing.T) {
	k := generateTestKey(t)
	withPubKey(t, k)
	payload := buildTarGz(t, defaultEntries())
	checksums := []byte(fmt.Sprintf("%s  %s\n", sha256Hex(payload), PayloadAsset))

	var wrongKeyID [8]byte
	if _, err := rand.Read(wrongKeyID[:]); err != nil {
		t.Fatalf("random key id: %v", err)
	}
	sig := signMinisig(t, k, wrongKeyID, "Ed", checksums, "timestamp:0\tfile:checksums.txt")

	fx := fixture{payload: payload, checksums: checksums, sig: sig}
	ts := newTestServer(t, fx.routes())

	_, _, err := runFailingFetch(t, ts)
	if !strings.Contains(err.Error(), "key id") {
		t.Fatalf("expected key id mismatch error, got: %v", err)
	}
}

func TestFetchPayload_PathTraversal(t *testing.T) {
	k := generateTestKey(t)
	withPubKey(t, k)
	entries := []tarEntry{
		{name: "skills/ok.md", body: []byte("fine\n")},
		{name: "../evil.txt", body: []byte("escape!\n")},
	}
	fx := buildFixture(t, k, "Ed", entries)
	ts := newTestServer(t, fx.routes())

	_, _, err := runFailingFetch(t, ts)
	if !strings.Contains(err.Error(), "unsafe path") {
		t.Fatalf("expected unsafe path error, got: %v", err)
	}
}

// TestVerifyRealReleaseSignature is the proof the minisign wire-format
// reading is actually correct rather than a plausible-looking approximation
// that only happens to satisfy a self-signed test fixture: it verifies
// testdata/checksums.txt + testdata/checksums.txt.minisig — real artifacts
// published on release v2.10.0 — against the REAL pinned MinisignPubKey
// constant (not an overridden test key).
func TestVerifyRealReleaseSignature(t *testing.T) {
	content, err := os.ReadFile("testdata/checksums.txt")
	if err != nil {
		t.Fatalf("read testdata/checksums.txt: %v", err)
	}
	sig, err := os.ReadFile("testdata/checksums.txt.minisig")
	if err != nil {
		t.Fatalf("read testdata/checksums.txt.minisig: %v", err)
	}

	if err := verifyMinisign(content, sig, MinisignPubKey); err != nil {
		t.Fatalf("verifyMinisign rejected genuine v2.10.0 release artifacts: %v", err)
	}

	tampered := append([]byte(nil), content...)
	if len(tampered) == 0 {
		t.Fatal("testdata/checksums.txt is empty")
	}
	tampered[0] ^= 0xFF // flip one byte
	if err := verifyMinisign(tampered, sig, MinisignPubKey); err == nil {
		t.Fatal("verifyMinisign accepted a checksums.txt with one flipped byte")
	}
}

// ── ResolveLatestTag ─────────────────────────────────────────────────────────

func TestResolveLatestTagFromRedirect(t *testing.T) {
	var tagHandlerHit bool
	mux := http.NewServeMux()
	mux.HandleFunc("/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", "/releases/tag/v2.10.0")
		w.WriteHeader(http.StatusFound)
	})
	mux.HandleFunc("/releases/tag/v2.10.0", func(w http.ResponseWriter, r *http.Request) {
		tagHandlerHit = true
		w.WriteHeader(http.StatusOK)
	})
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	c := &Client{BaseURL: ts.URL, HTTP: http.DefaultClient}
	tag, err := c.ResolveLatestTag(context.Background())
	if err != nil {
		t.Fatalf("ResolveLatestTag: %v", err)
	}
	if tag != "v2.10.0" {
		t.Fatalf("tag = %q, want %q", tag, "v2.10.0")
	}
	if tagHandlerHit {
		t.Fatal("client followed the redirect — /releases/tag/v2.10.0 handler was hit, but ResolveLatestTag must read the Location header without following it")
	}
}

func TestResolveLatestTagNoLocationHeader(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK) // no Location header
	})
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	c := &Client{BaseURL: ts.URL, HTTP: http.DefaultClient}
	tag, err := c.ResolveLatestTag(context.Background())
	if err == nil {
		t.Fatalf("expected error for missing Location header, got tag=%q", tag)
	}
	if tag != "" {
		t.Fatalf("expected empty tag on error, got %q", tag)
	}
}

func TestResolveLatestTagServerError(t *testing.T) {
	ts := httptest.NewServer(http.NewServeMux())
	ts.Close() // unreachable — connection refused

	c := &Client{BaseURL: ts.URL, HTTP: http.DefaultClient}
	tag, err := c.ResolveLatestTag(context.Background())
	if err == nil {
		t.Fatalf("expected error when server is unreachable, got tag=%q", tag)
	}
}

func TestResolveLatestTagTrailingSlashLocation(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", "/releases/tag/v2.10.0/")
		w.WriteHeader(http.StatusFound)
	})
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	c := &Client{BaseURL: ts.URL, HTTP: http.DefaultClient}
	tag, err := c.ResolveLatestTag(context.Background())
	if err != nil {
		t.Fatalf("ResolveLatestTag: %v", err)
	}
	if tag != "v2.10.0" {
		t.Fatalf("tag = %q, want %q (trailing-slash Location must still resolve)", tag, "v2.10.0")
	}
}
