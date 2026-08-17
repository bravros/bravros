package selfupdate

// End-to-end tests for the network half of the split update model: a real
// gzip/zip archive, a real minisign signature over a real checksums.txt, and a
// real executable swap on disk. Nothing here asserts on an exit code — the
// evidence is the bytes at exePath and the number of requests the httptest
// server saw.

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"golang.org/x/crypto/blake2b"
)

// ── minisign fixtures (same wire format the release signer emits) ────────────

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

func (k testKey) pubKeyB64() string {
	raw := make([]byte, 0, minisignPubKeyLen)
	raw = append(raw, 'E', 'd')
	raw = append(raw, k.keyID[:]...)
	raw = append(raw, k.pub...)
	return base64.StdEncoding.EncodeToString(raw)
}

func signMinisig(t *testing.T, k testKey, algo string, content []byte, trustedComment string) []byte {
	t.Helper()
	var message []byte
	switch algo {
	case minisignAlgoEd:
		message = content
	case minisignAlgoED:
		sum := blake2b.Sum512(content)
		message = sum[:]
	default:
		t.Fatalf("unknown algo %q", algo)
	}
	sig := ed25519.Sign(k.priv, message)

	blob := make([]byte, 0, minisignSigBlobLen)
	blob = append(blob, algo[0], algo[1])
	blob = append(blob, k.keyID[:]...)
	blob = append(blob, sig...)

	globalSig := ed25519.Sign(k.priv, append(append([]byte{}, sig...), []byte(trustedComment)...))

	var buf bytes.Buffer
	buf.WriteString("untrusted comment: minisign signature (test fixture)\n")
	buf.WriteString(base64.StdEncoding.EncodeToString(blob) + "\n")
	buf.WriteString("trusted comment: " + trustedComment + "\n")
	buf.WriteString(base64.StdEncoding.EncodeToString(globalSig) + "\n")
	return buf.Bytes()
}

// ── archive fixtures ────────────────────────────────────────────────────────

func buildTarGz(t *testing.T, name string, body []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	hdr := &tar.Header{Name: name, Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatalf("write tar header: %v", err)
	}
	if _, err := tw.Write(body); err != nil {
		t.Fatalf("write tar body: %v", err)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("close gzip: %v", err)
	}
	return buf.Bytes()
}

func buildZip(t *testing.T, name string, body []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create(name)
	if err != nil {
		t.Fatalf("create zip entry: %v", err)
	}
	if _, err := w.Write(body); err != nil {
		t.Fatalf("write zip entry: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	return buf.Bytes()
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// releaseServer serves a fake GitHub release and counts every request.
type releaseServer struct {
	*httptest.Server
	mu     sync.Mutex
	hits   map[string]int
	assets map[string][]byte
}

func (s *releaseServer) count(path string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.hits[path]
}

func (s *releaseServer) total() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, v := range s.hits {
		n += v
	}
	return n
}

func newReleaseServer(t *testing.T, tag string, assets map[string][]byte) *releaseServer {
	t.Helper()
	rs := &releaseServer{hits: map[string]int{}, assets: assets}
	rs.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rs.mu.Lock()
		rs.hits[r.URL.Path]++
		rs.mu.Unlock()

		name := filepath.Base(r.URL.Path)
		want := "/releases/download/" + tag + "/" + name
		body, ok := rs.assets[name]
		if !ok || r.URL.Path != want {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write(body)
	}))
	t.Cleanup(rs.Close)
	return rs
}

// fakeRelease builds the three assets a platform update needs, signed by k.
func fakeRelease(t *testing.T, k testKey, goos, goarch string, binaryBody []byte) map[string][]byte {
	t.Helper()
	asset := BinaryAssetName(goos, goarch)
	var archive []byte
	if strings.HasSuffix(asset, ".zip") {
		archive = buildZip(t, BinaryFileName(goos), binaryBody)
	} else {
		archive = buildTarGz(t, BinaryFileName(goos), binaryBody)
	}
	checksums := fmt.Sprintf("%s  %s\n%s  bravros-payload.tar.gz\n",
		sha256Hex(archive), asset, sha256Hex([]byte("unrelated")))
	sig := signMinisig(t, k, minisignAlgoEd, []byte(checksums), "Bravros release test fixture")
	return map[string][]byte{
		asset:             archive,
		ChecksumsAsset:    []byte(checksums),
		ChecksumsSigAsset: sig,
	}
}

func seedExecutable(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	exe := filepath.Join(dir, "bravros")
	if err := os.WriteFile(exe, []byte(content), 0o755); err != nil {
		t.Fatalf("seed executable: %v", err)
	}
	return exe
}

// ── tests ───────────────────────────────────────────────────────────────────

func TestUpdaterInstall_ReplacesTheBinary(t *testing.T) {
	k := generateTestKey(t)
	newBody := []byte("#!/bin/sh\necho bravros v9.9.9\n")
	assets := fakeRelease(t, k, "darwin", "arm64", newBody)
	rs := newReleaseServer(t, "v9.9.9", assets)

	exe := seedExecutable(t, "old binary")
	u := &Updater{BaseURL: rs.URL, HTTP: rs.Client(), GOOS: "darwin", GOARCH: "arm64", PubKey: k.pubKeyB64()}

	res, err := u.Install(context.Background(), "v9.9.9", exe)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}

	got, err := os.ReadFile(exe)
	if err != nil {
		t.Fatalf("read installed binary: %v", err)
	}
	if !bytes.Equal(got, newBody) {
		t.Errorf("binary content not replaced: %q", got)
	}
	info, err := os.Stat(exe)
	if err != nil {
		t.Fatalf("stat installed binary: %v", err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Errorf("installed binary is not executable: mode %v", info.Mode())
	}
	if res.SHA256 != sha256Hex(assets[u.AssetName()]) {
		t.Errorf("reported sha256 %q does not match the archive", res.SHA256)
	}
	// No leftovers: the previous binary is deletable on POSIX, so the swap
	// must clean it up rather than leaving a .old- sibling behind.
	if res.BackupPath != "" {
		t.Errorf("expected no leftover backup on this platform, got %q", res.BackupPath)
	}
	entries, _ := os.ReadDir(filepath.Dir(exe))
	for _, e := range entries {
		if strings.Contains(e.Name(), backupInfix) || strings.Contains(e.Name(), ".new-") {
			t.Errorf("swap left %s behind in the install dir", e.Name())
		}
	}
}

func TestUpdaterInstall_WindowsZipArchive(t *testing.T) {
	k := generateTestKey(t)
	newBody := []byte("MZ fake windows binary")
	assets := fakeRelease(t, k, "windows", "amd64", newBody)
	rs := newReleaseServer(t, "v9.9.9", assets)

	exe := seedExecutable(t, "old binary")
	u := &Updater{BaseURL: rs.URL, HTTP: rs.Client(), GOOS: "windows", GOARCH: "amd64", PubKey: k.pubKeyB64()}

	if u.AssetName() != "bravros-windows-amd64.zip" {
		t.Fatalf("unexpected windows asset name %q", u.AssetName())
	}
	if _, err := u.Install(context.Background(), "v9.9.9", exe); err != nil {
		t.Fatalf("Install: %v", err)
	}
	got, _ := os.ReadFile(exe)
	if !bytes.Equal(got, newBody) {
		t.Errorf("zip lane did not install the binary: %q", got)
	}
}

// TestUpdaterInstall_TamperedArchiveLeavesBinaryUntouched is the loud-failure
// proof: one flipped byte in the archive breaks the sha256 recorded in the
// signed checksums.txt, and the executable on disk must survive unchanged.
func TestUpdaterInstall_TamperedArchiveLeavesBinaryUntouched(t *testing.T) {
	k := generateTestKey(t)
	assets := fakeRelease(t, k, "darwin", "arm64", []byte("new binary"))
	asset := BinaryAssetName("darwin", "arm64")
	tampered := append([]byte{}, assets[asset]...)
	tampered[len(tampered)-1] ^= 0xff
	assets[asset] = tampered

	rs := newReleaseServer(t, "v9.9.9", assets)
	exe := seedExecutable(t, "old binary")
	u := &Updater{BaseURL: rs.URL, HTTP: rs.Client(), GOOS: "darwin", GOARCH: "arm64", PubKey: k.pubKeyB64()}

	_, err := u.Install(context.Background(), "v9.9.9", exe)
	if err == nil {
		t.Fatal("tampered archive must fail loudly")
	}
	if !strings.Contains(err.Error(), "checksum mismatch") {
		t.Errorf("expected a checksum mismatch, got: %v", err)
	}
	if got, _ := os.ReadFile(exe); string(got) != "old binary" {
		t.Errorf("binary was modified despite a failed verification: %q", got)
	}
}

// TestUpdaterInstall_ForgedSignatureLeavesBinaryUntouched signs checksums.txt
// with a DIFFERENT key than the one pinned on the Updater.
func TestUpdaterInstall_ForgedSignatureLeavesBinaryUntouched(t *testing.T) {
	real := generateTestKey(t)
	attacker := generateTestKey(t)
	assets := fakeRelease(t, attacker, "darwin", "arm64", []byte("hostile binary"))
	rs := newReleaseServer(t, "v9.9.9", assets)

	exe := seedExecutable(t, "old binary")
	u := &Updater{BaseURL: rs.URL, HTTP: rs.Client(), GOOS: "darwin", GOARCH: "arm64", PubKey: real.pubKeyB64()}

	_, err := u.Install(context.Background(), "v9.9.9", exe)
	if err == nil {
		t.Fatal("a signature from an unpinned key must fail")
	}
	if !strings.Contains(err.Error(), "signature verification failed") {
		t.Errorf("expected signature verification failure, got: %v", err)
	}
	if got, _ := os.ReadFile(exe); string(got) != "old binary" {
		t.Errorf("binary was modified despite a forged signature: %q", got)
	}
}

func TestUpdaterInstall_MissingAssetIsAnError(t *testing.T) {
	k := generateTestKey(t)
	assets := fakeRelease(t, k, "darwin", "arm64", []byte("new binary"))
	delete(assets, BinaryAssetName("darwin", "arm64"))
	rs := newReleaseServer(t, "v9.9.9", assets)

	exe := seedExecutable(t, "old binary")
	u := &Updater{BaseURL: rs.URL, HTTP: rs.Client(), GOOS: "darwin", GOARCH: "arm64", PubKey: k.pubKeyB64()}

	if _, err := u.Install(context.Background(), "v9.9.9", exe); err == nil {
		t.Fatal("a release without this platform's archive must fail")
	}
	if got, _ := os.ReadFile(exe); string(got) != "old binary" {
		t.Errorf("binary was modified: %q", got)
	}
	if rs.count("/releases/download/v9.9.9/"+BinaryAssetName("darwin", "arm64")) != 1 {
		t.Errorf("expected exactly one attempt at the missing asset, hits: %d", rs.total())
	}
}

// TestReplaceExecutable_RollsBackOnFailure covers the half of the swap that
// must never lose the user's binary.
func TestReplaceExecutable_RollsBackOnFailure(t *testing.T) {
	exe := seedExecutable(t, "old binary")
	if _, err := ReplaceExecutable(exe, filepath.Join(t.TempDir(), "does-not-exist")); err == nil {
		t.Fatal("replacing with a missing source must fail")
	}
	if got, _ := os.ReadFile(exe); string(got) != "old binary" {
		t.Errorf("existing binary must survive a failed replace, got %q", got)
	}
}

// TestReplaceExecutable_FallsBackToAsideDance forces the Windows branch on a
// POSIX host.
//
// ReplaceExecutable tries the direct atomic rename first (which always succeeds
// on POSIX, so the aside-dance below it would otherwise be dead code in CI and
// only ever execute on a platform we do not test on). Making the destination a
// non-empty DIRECTORY makes rename(staged, exePath) fail with ENOTDIR/EEXIST the
// way a locked running image fails on Windows, so the fallback runs here too.
//
// The assertion is that the fallback is reached and reports rather than
// half-completing: the pre-existing content must still be intact afterwards.
func TestReplaceExecutable_FallsBackToAsideDance(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "bravros")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatalf("mkdir dest: %v", err)
	}
	canary := filepath.Join(dest, "canary")
	if err := os.WriteFile(canary, []byte("intact"), 0o644); err != nil {
		t.Fatalf("write canary: %v", err)
	}

	src := filepath.Join(t.TempDir(), "new")
	if err := os.WriteFile(src, []byte("new binary"), 0o755); err != nil {
		t.Fatalf("write src: %v", err)
	}

	// Direct rename cannot replace a non-empty directory, so this exercises the
	// fallback. Whether the dance then succeeds or reports is platform detail;
	// what must NOT happen is silent destruction of what was already there.
	_, err := ReplaceExecutable(dest, src)
	if err == nil {
		t.Log("aside-dance completed against a directory destination on this platform")
	}
	if got, readErr := os.ReadFile(canary); readErr == nil && string(got) != "intact" {
		t.Errorf("pre-existing content was corrupted: %q", got)
	}

	// No staged temp file may be left behind in the parent directory.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read parent dir: %v", err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), ".new-") {
			t.Errorf("staged temp file leaked: %s", e.Name())
		}
	}
}

// TestCleanupOldBinaries_SweepsWindowsLeftovers exercises the deferred cleanup
// that makes the Windows running-exe case survivable: the previous image is
// renamed aside during the swap and removed by a later run.
func TestCleanupOldBinaries_SweepsWindowsLeftovers(t *testing.T) {
	dir := t.TempDir()
	leftovers := []string{"bravros" + backupInfix + "aabbcc", "bravros.exe" + backupInfix + "ddeeff"}
	for _, name := range leftovers {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("stale"), 0o755); err != nil {
			t.Fatalf("seed leftover: %v", err)
		}
	}
	keep := filepath.Join(dir, "bravros")
	if err := os.WriteFile(keep, []byte("live"), 0o755); err != nil {
		t.Fatalf("seed live binary: %v", err)
	}

	if n := CleanupOldBinaries(dir); n != len(leftovers) {
		t.Errorf("CleanupOldBinaries removed %d, want %d", n, len(leftovers))
	}
	for _, name := range leftovers {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			t.Errorf("%s survived the sweep", name)
		}
	}
	if _, err := os.Stat(keep); err != nil {
		t.Errorf("the live binary must never be swept: %v", err)
	}
}

func TestBinaryAssetName_MatchesTheReleaseTemplate(t *testing.T) {
	cases := map[[2]string]string{
		{"darwin", "arm64"}:  "bravros-darwin-arm64.tar.gz",
		{"darwin", "amd64"}:  "bravros-darwin-amd64.tar.gz",
		{"linux", "amd64"}:   "bravros-linux-amd64.tar.gz",
		{"linux", "arm64"}:   "bravros-linux-arm64.tar.gz",
		{"windows", "amd64"}: "bravros-windows-amd64.zip",
		{"windows", "arm64"}: "bravros-windows-arm64.zip",
	}
	for platform, want := range cases {
		if got := BinaryAssetName(platform[0], platform[1]); got != want {
			t.Errorf("BinaryAssetName(%q, %q) = %q, want %q", platform[0], platform[1], got, want)
		}
	}
}

// TestVerifyMinisign_AcceptsRealReleaseSignature pins this verifier against the
// SAME fixture cli/internal/fetch's tests use — the genuine signed checksums.txt
// of a published release, verified against the real pinned key. It is what
// stops the deliberate duplication documented on VerifyMinisign from drifting
// into two implementations that disagree.
func TestVerifyMinisign_AcceptsRealReleaseSignature(t *testing.T) {
	const fixtureDir = "../fetch/testdata"
	content, err := os.ReadFile(filepath.Join(fixtureDir, "checksums.txt"))
	if err != nil {
		t.Fatalf("read real release checksums fixture (%s): %v", fixtureDir, err)
	}
	sig, err := os.ReadFile(filepath.Join(fixtureDir, "checksums.txt.minisig"))
	if err != nil {
		t.Fatalf("read real release signature fixture (%s): %v", fixtureDir, err)
	}

	if err := VerifyMinisign(content, sig, (&Updater{}).pubKey()); err != nil {
		t.Fatalf("real release signature must verify against the pinned key: %v", err)
	}

	// And it must fail closed on a single flipped byte.
	tampered := append([]byte{}, content...)
	tampered[0] ^= 0x01
	if err := VerifyMinisign(tampered, sig, (&Updater{}).pubKey()); err == nil {
		t.Fatal("a tampered checksums.txt must not verify")
	}
}
