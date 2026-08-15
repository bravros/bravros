// Package fetch downloads, verifies, and atomically installs the Bravros
// payload asset (skills/ + templates/) published on every GitHub release.
//
// The trust chain mirrors install.sh exactly (cli/CLAUDE.md and
// .goreleaser.yml document why): checksums.txt is minisign-verified against
// the pinned MinisignPubKey BEFORE any of its content is trusted, only then
// is the payload's sha256 compared against the line inside it, and only then
// is the archive extracted. destDir is never modified until every check has
// passed — any failure leaves it exactly as it was found.
package fetch

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/bravros/bravros/cli/internal/config"
)

// PayloadAsset is the release asset carrying skills/ + templates/.
const PayloadAsset = "bravros-payload.tar.gz"

// MinisignPubKey is the pinned key, identical to the one in install.sh.
const MinisignPubKey = "RWQqHlahq4RjNnCasO/8yMsgtLGfdHejILKMxxpsulIs1rII6IgMO26G"

// defaultBaseURL is the GitHub repository releases live under.
const defaultBaseURL = "https://github.com/bravros/bravros"

// ErrNoPayload is returned when the release exists but publishes no payload
// asset. Every release cut before P-0003 is in this state; callers treat it
// as "nothing to fetch", not as a failure.
var ErrNoPayload = errors.New("release has no payload asset")

// httpStatusError carries the HTTP status code of a failed asset download so
// callers can distinguish, e.g., "404 on the payload asset" from other
// download failures via errors.As.
type httpStatusError struct {
	status int
	url    string
}

func (e *httpStatusError) Error() string {
	return fmt.Sprintf("unexpected status %d fetching %s", e.status, e.url)
}

// Client fetches and verifies release payloads.
type Client struct {
	// BaseURL is the repository root, e.g. "https://github.com/bravros/bravros".
	// Tests point this at an httptest.Server.
	BaseURL string

	// HTTP is the client used for all requests. Defaults to http.DefaultClient
	// when nil.
	HTTP *http.Client
}

// NewClient returns a Client configured against the real bravros repository.
func NewClient() *Client {
	return &Client{
		BaseURL: defaultBaseURL,
		HTTP:    &http.Client{Timeout: 60 * time.Second},
	}
}

// DefaultPayloadDir returns the on-disk home of the fetched payload:
// config.ConfigDir() + "/payload".
func DefaultPayloadDir() string {
	return filepath.Join(config.ConfigDir(), "payload")
}

func (c *Client) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return http.DefaultClient
}

func (c *Client) baseURL() string {
	if c.BaseURL != "" {
		return strings.TrimRight(c.BaseURL, "/")
	}
	return defaultBaseURL
}

// ResolveLatestTag returns the newest published tag (e.g. "v3.51.0") WITHOUT
// using api.github.com — it issues a redirect-following-disabled GET to
// <BaseURL>/releases/latest and parses the tag out of the Location header.
// Requires no token and does not consume the 60/hr API budget.
func (c *Client) ResolveLatestTag(ctx context.Context) (string, error) {
	noRedirect := *c.httpClient()
	noRedirect.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}

	reqURL := c.baseURL() + "/releases/latest"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}

	resp, err := noRedirect.Do(req)
	if err != nil {
		return "", fmt.Errorf("resolve latest tag: %w", err)
	}
	defer resp.Body.Close()

	loc := resp.Header.Get("Location")
	if loc == "" {
		return "", fmt.Errorf("resolve latest tag: no redirect Location in response (status %d)", resp.StatusCode)
	}

	parsed, err := url.Parse(loc)
	if err != nil {
		return "", fmt.Errorf("resolve latest tag: parse Location %q: %w", loc, err)
	}

	tag := path.Base(parsed.Path)
	if tag == "" || tag == "." || tag == "/" {
		return "", fmt.Errorf("resolve latest tag: could not extract tag from Location %q", loc)
	}
	return tag, nil
}

// FetchPayload downloads the payload for tag (empty tag == "latest"),
// verifies it, and atomically installs it at destDir. On ANY failure destDir
// is left exactly as it was. Returns the sha256 hex of the verified tarball.
func (c *Client) FetchPayload(ctx context.Context, tag, destDir string) (sha256hex string, err error) {
	tmpDir, err := os.MkdirTemp("", "bravros-fetch-*")
	if err != nil {
		return "", fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	checksumsPath := filepath.Join(tmpDir, "checksums.txt")
	sigPath := filepath.Join(tmpDir, "checksums.txt.minisig")
	payloadPath := filepath.Join(tmpDir, PayloadAsset)

	if dlErr := c.downloadTo(ctx, c.assetURL(tag, "checksums.txt"), checksumsPath); dlErr != nil {
		return "", fmt.Errorf("download checksums.txt: %w", dlErr)
	}
	if dlErr := c.downloadTo(ctx, c.assetURL(tag, "checksums.txt.minisig"), sigPath); dlErr != nil {
		return "", fmt.Errorf("download checksums.txt.minisig: %w", dlErr)
	}
	if dlErr := c.downloadTo(ctx, c.assetURL(tag, PayloadAsset), payloadPath); dlErr != nil {
		var statusErr *httpStatusError
		if errors.As(dlErr, &statusErr) && statusErr.status == http.StatusNotFound {
			return "", fmt.Errorf("%s: %w", PayloadAsset, ErrNoPayload)
		}
		return "", fmt.Errorf("download %s: %w", PayloadAsset, dlErr)
	}

	checksumsBytes, rErr := os.ReadFile(checksumsPath)
	if rErr != nil {
		return "", fmt.Errorf("read checksums.txt: %w", rErr)
	}
	sigBytes, rErr := os.ReadFile(sigPath)
	if rErr != nil {
		return "", fmt.Errorf("read checksums.txt.minisig: %w", rErr)
	}

	// Verify the signature over checksums.txt BEFORE trusting a single byte
	// of its content — this is the load-bearing ordering of the whole chain.
	if vErr := verifyMinisign(checksumsBytes, sigBytes, pubKey); vErr != nil {
		return "", fmt.Errorf("signature verification failed: %w", vErr)
	}

	expectedSHA, fErr := findChecksum(string(checksumsBytes), PayloadAsset)
	if fErr != nil {
		return "", fmt.Errorf("locate checksum: %w", fErr)
	}

	actualSHA, sErr := sha256File(payloadPath)
	if sErr != nil {
		return "", fmt.Errorf("hash payload: %w", sErr)
	}

	if !strings.EqualFold(expectedSHA, actualSHA) {
		return "", fmt.Errorf("checksum mismatch for %s: expected %s, got %s", PayloadAsset, expectedSHA, actualSHA)
	}

	stagingDir := destDir + ".new-" + randomSuffix()
	defer os.RemoveAll(stagingDir)

	if xErr := extractTarGz(payloadPath, stagingDir); xErr != nil {
		return "", fmt.Errorf("extract payload: %w", xErr)
	}

	if sw := atomicSwap(destDir, stagingDir); sw != nil {
		return "", fmt.Errorf("install payload: %w", sw)
	}

	return actualSHA, nil
}

// assetURL builds the download URL for a named release asset. An empty tag
// resolves to the "latest" release alias.
func (c *Client) assetURL(tag, asset string) string {
	if tag == "" {
		return fmt.Sprintf("%s/releases/latest/download/%s", c.baseURL(), asset)
	}
	return fmt.Sprintf("%s/releases/download/%s/%s", c.baseURL(), tag, asset)
}

func (c *Client) downloadTo(ctx context.Context, assetURL, dest string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, assetURL, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return &httpStatusError{status: resp.StatusCode, url: assetURL}
	}

	out, err := os.Create(dest)
	if err != nil {
		return fmt.Errorf("create %s: %w", dest, err)
	}
	defer out.Close()

	if _, err := io.Copy(out, resp.Body); err != nil {
		return fmt.Errorf("write %s: %w", dest, err)
	}
	return nil
}

// atomicSwap installs stagingDir at destDir. If destDir already exists it is
// preserved as destDir+".old-<rand>" until the swap succeeds, then removed;
// on a failed second rename the old content is rolled back into place.
func atomicSwap(destDir, stagingDir string) error {
	if err := os.MkdirAll(filepath.Dir(destDir), 0o755); err != nil {
		return fmt.Errorf("prepare parent dir: %w", err)
	}

	_, statErr := os.Lstat(destDir)
	destExists := statErr == nil

	if !destExists {
		if err := os.Rename(stagingDir, destDir); err != nil {
			return fmt.Errorf("move staging into place: %w", err)
		}
		return nil
	}

	oldDir := destDir + ".old-" + randomSuffix()
	if err := os.Rename(destDir, oldDir); err != nil {
		return fmt.Errorf("move existing payload aside: %w", err)
	}

	if err := os.Rename(stagingDir, destDir); err != nil {
		if rbErr := os.Rename(oldDir, destDir); rbErr != nil {
			return fmt.Errorf("swap failed (%v) AND rollback failed (%v); previous payload preserved at %s", err, rbErr, oldDir)
		}
		return fmt.Errorf("swap failed, rolled back to previous payload: %w", err)
	}

	_ = os.RemoveAll(oldDir)
	return nil
}

func randomSuffix() string {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}
