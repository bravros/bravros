package deploy

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Manifest records the SHA of each deployed skill so Deploy and detectSkillsDrift
// can detect content changes without a full file-by-file comparison.
// Scripts records the SHA of each deployed script (scripts/*.sh, scripts/*.py) for
// the detectScriptsDrift signal in selfupdate. Missing field is treated as max drift
// (full redeploy), same pattern as missing Skills.
type Manifest struct {
	Version int               `json:"version"`
	Skills  map[string]string `json:"skills"`
	Scripts map[string]string `json:"scripts,omitempty"`
}

// manifestFileName is the name of the manifest file stored inside the runtime
// skills directory. It is skipped during ComputeSkillSHA walks.
const manifestFileName = ".deploy-manifest.json"

// ManifestFileName is the exported name of the manifest file for callers outside
// the deploy package (e.g., selfupdate).
const ManifestFileName = manifestFileName

// ComputeSkillSHA returns a deterministic hex digest that represents the content
// of all files inside skillDir.
//
// Walk strategy: filepath.WalkDir with os.Stat (not os.Lstat) on every entry so
// that symlinks are resolved to their targets. For each regular file (after
// resolution) we build a "relpath\x00sha256(content)\n" record, sort all records,
// and return the sha256 of the joined byte stream.
//
// Broken symlinks (os.Stat fails on a symlink entry) are returned as hard errors —
// the caller decides whether to skip the skill or abort the deploy.
//
// The manifest file itself (.deploy-manifest.json) is skipped if encountered.
func ComputeSkillSHA(skillDir string) (string, error) {
	var records []string

	err := filepath.WalkDir(skillDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Always use os.Stat to follow symlinks.
		info, statErr := os.Stat(path)
		if statErr != nil {
			// Broken symlink — surface as hard error.
			return fmt.Errorf("broken symlink at %s: %w", path, statErr)
		}

		// Skip directories (including symlinks that resolve to directories).
		if info.IsDir() {
			return nil
		}

		// Skip the manifest file itself (defensive guard).
		if d.Name() == manifestFileName {
			return nil
		}

		rel, err := filepath.Rel(skillDir, path)
		if err != nil {
			return err
		}

		// On macOS filepath.WalkDir may emit the symlink entry as a non-dir
		// DirEntry; open via the resolved path (path follows symlink via os.Open).
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		h := sha256.New()
		if _, err := io.Copy(h, f); err != nil {
			f.Close()
			return err
		}
		f.Close()

		records = append(records, fmt.Sprintf("%s\x00%x\n", rel, h.Sum(nil)))
		return nil
	})
	if err != nil {
		return "", err
	}

	sort.Strings(records)
	overall := sha256.Sum256([]byte(strings.Join(records, "")))
	return fmt.Sprintf("%x", overall), nil
}

// LoadManifest reads the manifest at path. If the file does not exist it returns
// a zero-value manifest with Version 1 and an empty Skills map.
func LoadManifest(path string) (Manifest, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Manifest{Version: 1, Skills: map[string]string{}}, nil
		}
		return Manifest{}, fmt.Errorf("open manifest: %w", err)
	}
	defer f.Close()

	var m Manifest
	if err := json.NewDecoder(f).Decode(&m); err != nil {
		return Manifest{}, fmt.Errorf("decode manifest: %w", err)
	}
	if m.Skills == nil {
		m.Skills = map[string]string{}
	}
	if m.Scripts == nil {
		m.Scripts = map[string]string{}
	}
	return m, nil
}

// SaveManifest writes m to path atomically (temp file + rename) so that a
// concurrent reader never sees a partial write. Parent directories are created
// if they do not exist.
func SaveManifest(path string, m Manifest) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("mkdir manifest parent: %w", err)
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), ".deploy-manifest-*.json.tmp")
	if err != nil {
		return fmt.Errorf("create temp manifest: %w", err)
	}
	tmpName := tmp.Name()

	enc := json.NewEncoder(tmp)
	enc.SetIndent("", "  ")
	if err := enc.Encode(m); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("encode manifest: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("close temp manifest: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("rename manifest: %w", err)
	}
	return nil
}
