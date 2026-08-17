// Binary self-replacement — the NETWORK half of the split update model (P-0015 D2).
//
// D2 splits what used to be one verb in two:
//
//   - `bravros selfupdate` (SessionStart) refreshes skills/templates from the
//     payload EMBEDDED in the running binary. Local file copy, zero network,
//     always safe — see cmd/selfupdate.go.
//   - `bravros update` (this file) does the risky half explicitly: resolve the
//     newest release, download the platform archive, verify it against the
//     minisign-signed checksums.txt, and replace the RUNNING executable.
//
// The trust chain is deliberately identical to install.sh and
// cli/internal/fetch: checksums.txt is minisign-verified against the pinned key
// BEFORE a single byte of its content is trusted, the archive's sha256 is then
// compared against the line inside it, and only then is anything extracted. The
// executable on disk is not touched until every check has passed.
package selfupdate

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/bravros/bravros/cli/internal/fetch"
	"golang.org/x/crypto/blake2b"
)

const (
	// ChecksumsAsset and ChecksumsSigAsset are the two release assets that
	// carry the trust chain. checksums.txt covers every platform archive AND
	// bravros-payload.tar.gz (.goreleaser.yml folds it in), so one signature
	// protects the whole release.
	ChecksumsAsset    = "checksums.txt"
	ChecksumsSigAsset = "checksums.txt.minisig"

	// defaultBaseURL is the repository releases live under — the same host
	// install.sh and cli/internal/fetch use.
	defaultBaseURL = "https://github.com/bravros/bravros"

	// maxArchiveBytes caps what a single downloaded asset or extracted file
	// may occupy, so a hostile or corrupt archive cannot fill the disk.
	maxArchiveBytes = 256 << 20 // 256 MiB
)

// BinaryAssetName returns the release asset holding the bravros binary for a
// platform. The name template is load-bearing and shared with install.sh,
// install.ps1 and the Homebrew formula (.goreleaser.yml:56) — "bravros-{Os}-{Arch}",
// tar.gz everywhere except Windows, which gets .zip because Expand-Archive is
// the only unpacker PowerShell 5.1 ships (.goreleaser.yml:57-62).
func BinaryAssetName(goos, goarch string) string {
	if goos == "windows" {
		return fmt.Sprintf("bravros-%s-%s.zip", goos, goarch)
	}
	return fmt.Sprintf("bravros-%s-%s.tar.gz", goos, goarch)
}

// BinaryFileName is the executable's name inside the archive.
func BinaryFileName(goos string) string {
	if goos == "windows" {
		return "bravros.exe"
	}
	return "bravros"
}

// Updater downloads, verifies and installs a bravros binary.
//
// Every field has a working zero value except in tests: BaseURL defaults to the
// real repository, HTTP to a 60s client, GOOS/GOARCH to the running platform,
// and PubKey to the pinned release key. Tests point BaseURL at an httptest
// server and PubKey at a throwaway keypair.
type Updater struct {
	BaseURL string
	HTTP    *http.Client
	GOOS    string
	GOARCH  string
	PubKey  string
}

// UpdateResult reports what an Install actually did — exit codes prove nothing
// here, so callers (and tests) assert on this and on the filesystem.
type UpdateResult struct {
	Tag    string
	Asset  string
	SHA256 string
	// ExePath is the executable that now carries the new build.
	ExePath string
	// BackupPath is non-empty when the previous executable could NOT be
	// removed after the swap — the normal Windows outcome, where the running
	// image keeps a lock on its own file. It is cleaned up by the next run
	// (CleanupOldBinaries), which is exactly the standard rename-then-replace
	// trick: you may RENAME a running .exe on Windows, you may not delete it.
	BackupPath string
}

func (u *Updater) baseURL() string {
	if u.BaseURL != "" {
		return strings.TrimRight(u.BaseURL, "/")
	}
	return defaultBaseURL
}

func (u *Updater) httpClient() *http.Client {
	if u.HTTP != nil {
		return u.HTTP
	}
	return &http.Client{Timeout: 60 * time.Second}
}

func (u *Updater) goos() string {
	if u.GOOS != "" {
		return u.GOOS
	}
	return runtime.GOOS
}

func (u *Updater) goarch() string {
	if u.GOARCH != "" {
		return u.GOARCH
	}
	return runtime.GOARCH
}

func (u *Updater) pubKey() string {
	if u.PubKey != "" {
		return u.PubKey
	}
	// The pinned release key. Imported rather than re-declared so a key
	// rotation lands in exactly one place (cli/internal/fetch/fetch.go:34);
	// fetch imports nothing from this package, so there is no cycle.
	return fetch.MinisignPubKey
}

// AssetName is the archive Install will download for this Updater's platform.
func (u *Updater) AssetName() string {
	return BinaryAssetName(u.goos(), u.goarch())
}

func (u *Updater) assetURL(tag, asset string) string {
	if tag == "" {
		return fmt.Sprintf("%s/releases/latest/download/%s", u.baseURL(), asset)
	}
	return fmt.Sprintf("%s/releases/download/%s/%s", u.baseURL(), tag, asset)
}

// Install downloads the release archive for tag, verifies it end to end, and
// replaces the executable at exePath. exePath is left untouched unless every
// check passed.
func (u *Updater) Install(ctx context.Context, tag, exePath string) (UpdateResult, error) {
	res := UpdateResult{Tag: tag, Asset: u.AssetName(), ExePath: exePath}

	tmpDir, err := os.MkdirTemp("", "bravros-update-")
	if err != nil {
		return res, fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	checksumsPath := filepath.Join(tmpDir, ChecksumsAsset)
	sigPath := filepath.Join(tmpDir, ChecksumsSigAsset)
	archivePath := filepath.Join(tmpDir, res.Asset)

	if err := u.download(ctx, u.assetURL(tag, ChecksumsAsset), checksumsPath); err != nil {
		return res, fmt.Errorf("download %s: %w", ChecksumsAsset, err)
	}
	if err := u.download(ctx, u.assetURL(tag, ChecksumsSigAsset), sigPath); err != nil {
		return res, fmt.Errorf("download %s: %w", ChecksumsSigAsset, err)
	}
	if err := u.download(ctx, u.assetURL(tag, res.Asset), archivePath); err != nil {
		return res, fmt.Errorf("download %s: %w", res.Asset, err)
	}

	checksums, err := os.ReadFile(checksumsPath)
	if err != nil {
		return res, fmt.Errorf("read %s: %w", ChecksumsAsset, err)
	}
	sig, err := os.ReadFile(sigPath)
	if err != nil {
		return res, fmt.Errorf("read %s: %w", ChecksumsSigAsset, err)
	}

	// Signature first — nothing inside checksums.txt is trusted before this.
	if err := VerifyMinisign(checksums, sig, u.pubKey()); err != nil {
		return res, fmt.Errorf("signature verification failed: %w", err)
	}

	want, err := FindChecksum(string(checksums), res.Asset)
	if err != nil {
		return res, err
	}
	got, err := sha256File(archivePath)
	if err != nil {
		return res, fmt.Errorf("hash %s: %w", res.Asset, err)
	}
	if !strings.EqualFold(want, got) {
		return res, fmt.Errorf("checksum mismatch for %s: expected %s, got %s", res.Asset, want, got)
	}
	res.SHA256 = got

	staged := filepath.Join(tmpDir, BinaryFileName(u.goos()))
	if err := extractBinary(archivePath, staged, BinaryFileName(u.goos())); err != nil {
		return res, err
	}

	backup, err := ReplaceExecutable(exePath, staged)
	if err != nil {
		return res, err
	}
	res.BackupPath = backup
	return res, nil
}

func (u *Updater) download(ctx context.Context, url, dest string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	resp, err := u.httpClient().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status %d fetching %s", resp.StatusCode, url)
	}
	out, err := os.Create(dest)
	if err != nil {
		return fmt.Errorf("create %s: %w", dest, err)
	}
	defer out.Close()
	if _, err := io.Copy(out, io.LimitReader(resp.Body, maxArchiveBytes)); err != nil {
		return fmt.Errorf("write %s: %w", dest, err)
	}
	return nil
}

// ─── replacement ────────────────────────────────────────────────────────────

// backupInfix marks a sidelined previous executable: "bravros.old-<rand>".
const backupInfix = ".old-"

// ReplaceExecutable installs newBinary at exePath and returns the path of the
// sidelined previous executable when it could not be deleted ("" when it was).
//
// Try the direct atomic replace first, and fall back to the aside-dance only
// when the platform refuses it:
//
//	rename(staged, exePath)                // POSIX: atomic, no window, done
//	                                       // Windows: fails, running image locked
//	rename(exePath, exePath+".old-xxxx")   // legal on Windows for a RUNNING exe
//	rename(staged,  exePath)
//	remove(exePath+".old-xxxx")            // fails on Windows; deferred, not fatal
//
// This is still one code path with no build tags, but it does NOT flatten both
// platforms onto the worse of the two behaviors. On POSIX, rename(2) atomically
// replaces the target while the running process keeps its unlinked inode, so
// there is never an instant with no binary at exePath. Doing the aside-dance
// unconditionally would manufacture exactly the window POSIX avoids.
//
// A failure at the second rename rolls the old executable back into place, so a
// broken download can never leave the user without a bravros.
func ReplaceExecutable(exePath, newBinary string) (string, error) {
	if exePath == "" {
		return "", fmt.Errorf("no executable path to replace")
	}
	dir := filepath.Dir(exePath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("prepare %s: %w", dir, err)
	}

	// Stage inside the destination directory so the final rename is same-device.
	staged := filepath.Join(dir, "."+filepath.Base(exePath)+".new-"+randomSuffix())
	if err := copyFileMode(newBinary, staged, 0o755); err != nil {
		return "", err
	}
	defer os.Remove(staged) // no-op once the rename below has consumed it

	// Attempt the direct replace FIRST. On POSIX, rename(2) atomically replaces
	// the target and the running process keeps its now-unlinked inode, so this
	// succeeds and there is never an instant with no binary at exePath. Taking
	// the aside-dance below unconditionally would MANUFACTURE a window that
	// POSIX does not otherwise have — the failure it opens (crash between the
	// two renames leaves the user with no bravros at all) is strictly worse than
	// the branch it avoids.
	if err := os.Rename(staged, exePath); err == nil {
		return "", nil
	}

	// The direct replace failed. On Windows that is expected and not an error
	// condition: a RUNNING image is locked against replacement, though renaming
	// it away is permitted. Move the current binary aside, then install over the
	// now-free name. This ordering is the only one Windows allows, and it is why
	// the no-binary window exists there — accepted per D3, which makes native
	// Windows a documented degraded tier rather than a first-class target.
	//
	// Reaching here on POSIX means something genuinely wrong (read-only
	// directory, permissions), and the dance will fail again below and report.
	backup := ""
	if _, err := os.Lstat(exePath); err == nil {
		backup = exePath + backupInfix + randomSuffix()
		if err := os.Rename(exePath, backup); err != nil {
			return "", fmt.Errorf("move current binary aside: %w", err)
		}
	}

	if err := os.Rename(staged, exePath); err != nil {
		if backup != "" {
			if rbErr := os.Rename(backup, exePath); rbErr != nil {
				return "", fmt.Errorf("install failed (%v) AND rollback failed (%v); previous binary preserved at %s", err, rbErr, backup)
			}
		}
		return "", fmt.Errorf("install new binary, rolled back: %w", err)
	}

	if backup == "" {
		return "", nil
	}
	if err := os.Remove(backup); err != nil {
		// Windows: the running image cannot delete its own file. Leave it —
		// CleanupOldBinaries sweeps it on the next run.
		return backup, nil
	}
	return "", nil
}

// CleanupOldBinaries removes sidelined executables left behind by a previous
// update (the Windows case above). Best effort by construction: a file still
// locked by a running process is skipped and retried next time. Returns the
// number removed.
func CleanupOldBinaries(dir string) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	removed := 0
	for _, e := range entries {
		if e.IsDir() || !strings.Contains(e.Name(), backupInfix) {
			continue
		}
		if err := os.Remove(filepath.Join(dir, e.Name())); err == nil {
			removed++
		}
	}
	return removed
}

func copyFileMode(src, dst string, mode os.FileMode) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("read %s: %w", src, err)
	}
	if err := os.WriteFile(dst, data, mode); err != nil {
		return fmt.Errorf("write %s: %w", dst, err)
	}
	// WriteFile honors umask on creation; force the exec bit explicitly.
	if err := os.Chmod(dst, mode); err != nil {
		return fmt.Errorf("chmod %s: %w", dst, err)
	}
	return nil
}

func randomSuffix() string {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 16)
	}
	return hex.EncodeToString(b)
}

// ─── archive extraction ─────────────────────────────────────────────────────

// extractBinary pulls the single entry named binaryName out of archivePath and
// writes it to destPath. Both archive shapes the release publishes are handled:
// .zip for Windows, .tar.gz everywhere else. Only the one named entry is ever
// written, so there is no path-traversal surface at all.
func extractBinary(archivePath, destPath, binaryName string) error {
	if strings.HasSuffix(archivePath, ".zip") {
		return extractBinaryZip(archivePath, destPath, binaryName)
	}
	return extractBinaryTarGz(archivePath, destPath, binaryName)
}

func extractBinaryTarGz(archivePath, destPath, binaryName string) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("open %s: %w", archivePath, err)
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("read %s as gzip: %w", archivePath, err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read %s: %w", archivePath, err)
		}
		if hdr.Typeflag != tar.TypeReg || filepath.Base(hdr.Name) != binaryName {
			continue
		}
		return writeExtracted(destPath, tr)
	}
	return fmt.Errorf("%s contains no %s entry", filepath.Base(archivePath), binaryName)
}

func extractBinaryZip(archivePath, destPath, binaryName string) error {
	zr, err := zip.OpenReader(archivePath)
	if err != nil {
		return fmt.Errorf("open %s: %w", archivePath, err)
	}
	defer zr.Close()

	for _, f := range zr.File {
		if f.FileInfo().IsDir() || filepath.Base(f.Name) != binaryName {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return fmt.Errorf("read %s from %s: %w", f.Name, archivePath, err)
		}
		defer rc.Close()
		return writeExtracted(destPath, rc)
	}
	return fmt.Errorf("%s contains no %s entry", filepath.Base(archivePath), binaryName)
}

func writeExtracted(destPath string, r io.Reader) error {
	out, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return fmt.Errorf("create %s: %w", destPath, err)
	}
	defer out.Close()
	if _, err := io.Copy(out, io.LimitReader(r, maxArchiveBytes)); err != nil {
		return fmt.Errorf("write %s: %w", destPath, err)
	}
	return nil
}

// ─── trust chain ────────────────────────────────────────────────────────────

func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// FindChecksum locates the sha256 hex digest for asset inside a sha256sum-style
// checksums.txt ("<hex>  <filename>", with the optional "*" binary-mode marker).
func FindChecksum(checksums, asset string) (string, error) {
	scanner := bufio.NewScanner(strings.NewReader(checksums))
	for scanner.Scan() {
		fields := strings.Fields(strings.TrimSpace(scanner.Text()))
		if len(fields) != 2 {
			continue
		}
		if strings.TrimPrefix(fields[1], "*") == asset {
			return fields[0], nil
		}
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("scan checksums: %w", err)
	}
	return "", fmt.Errorf("no checksum entry for %q", asset)
}

const (
	minisignAlgoEd = "Ed" // Ed25519 over the raw file content
	minisignAlgoED = "ED" // Ed25519 over blake2b-512(file content)

	minisignSigBlobLen = 74 // 2-byte algo + 8-byte key id + 64-byte signature
	minisignPubKeyLen  = 42 // 2-byte algo + 8-byte key id + 32-byte public key
)

// VerifyMinisign verifies a legacy-format ".minisig" signature over content
// against a base64 minisign public key (algo || key id || Ed25519 key).
//
// KNOWN DUPLICATION, deliberate and pinned by a test: cli/internal/fetch has
// the same verifier as an UNEXPORTED function (fetch/minisign.go:35). The
// payload lane cannot export it without widening that package's API, and this
// lane cannot import it. TestVerifyMinisign_AcceptsRealReleaseSignature runs
// this implementation against the very fixture the fetch tests use — the real
// signed checksums.txt of a published release — so the two can never drift into
// disagreement unnoticed. Collapsing them into one shared package is a
// follow-up, not a Phase 6 edit (fetch/ is outside this phase's scope).
func VerifyMinisign(content, sigText []byte, pubKeyB64 string) error {
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
	sigBlob, err := base64.StdEncoding.DecodeString(strings.TrimSpace(lines[1]))
	if err != nil {
		return fmt.Errorf("decode signature: %w", err)
	}
	if len(sigBlob) != minisignSigBlobLen {
		return fmt.Errorf("signature: unexpected length %d (want %d)", len(sigBlob), minisignSigBlobLen)
	}

	const trustedPrefix = "trusted comment: "
	if !strings.HasPrefix(lines[2], trustedPrefix) {
		return fmt.Errorf("malformed minisig: missing trusted comment line")
	}
	trustedComment := strings.TrimPrefix(lines[2], trustedPrefix)

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

	globalSig, err := base64.StdEncoding.DecodeString(strings.TrimSpace(lines[3]))
	if err != nil {
		return fmt.Errorf("decode global signature: %w", err)
	}
	globalMessage := append(append([]byte{}, sig...), []byte(trustedComment)...)
	if !ed25519.Verify(pubKeyBytes, globalMessage, globalSig) {
		return fmt.Errorf("trusted comment signature verification failed")
	}
	return nil
}
