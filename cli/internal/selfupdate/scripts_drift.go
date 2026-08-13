// Package selfupdate — scripts/ drift signal.
//
// detectScriptsDrift hashes every *.sh and *.py file in the source scripts/
// directory and compares the results against the Scripts section of the deploy
// manifest. It returns the list of script names whose content has changed (or
// that are new / deleted), signalling that install.sh must re-run.
//
// MacStudio gate: announce.sh is only meaningful on the primary Mac Studio
// workstation (it dispatches Alexa TTS). On any other host we exclude it from
// the drift calculation so a change to announce.sh does not trigger an
// unnecessary redeploy on Linux / other machines.
package selfupdate

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/bravros/bravros/cli/internal/deploy"
	"github.com/bravros/bravros/cli/internal/host"
)

// DetectScriptsDrift compares *.sh and *.py files under <repo>/scripts/ against
// the Scripts map in the deploy manifest.
//
// Rules:
//   - announce.sh is excluded on non-MacStudio hosts (MacStudio gate via host.IsMacStudio).
//   - A missing or empty Scripts map in the manifest is treated as max drift (all scripts
//     appear as drifted).
//   - Hash failures are treated as drift (fail-safe: redeploy rather than miss a change).
//
// Returns the sorted list of script basenames that have drifted (or been added / removed).
// An empty slice means no drift. The error path is reserved for IO failures on the scripts
// directory itself; per-file hash failures populate the drifted list rather than returning err.
func DetectScriptsDrift(repo string, manifest deploy.Manifest) ([]string, error) {
	scriptsDir := filepath.Join(repo, "scripts")

	entries, err := os.ReadDir(scriptsDir)
	if err != nil {
		if os.IsNotExist(err) {
			// No scripts/ directory in source — nothing to drift.
			return nil, nil
		}
		return nil, fmt.Errorf("scripts drift: read scripts dir: %w", err)
	}

	isMac := host.IsMacStudio()

	// Build a map of source-side SHAs for relevant scripts.
	sourceSHAs := make(map[string]string)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()

		// Filter to *.sh and *.py only.
		ext := filepath.Ext(name)
		if ext != ".sh" && ext != ".py" {
			continue
		}

		// MacStudio gate: skip announce.sh on non-Mac hosts.
		if name == "announce.sh" && !isMac {
			continue
		}

		sha, err := hashFile(filepath.Join(scriptsDir, name))
		if err != nil {
			// Treat unreadable source file as drift (force redeploy).
			sourceSHAs[name] = ""
		} else {
			sourceSHAs[name] = sha
		}
	}

	// missing Scripts map in manifest ⇒ treat every source script as drifted.
	if len(manifest.Scripts) == 0 && len(sourceSHAs) > 0 {
		var all []string
		for name := range sourceSHAs {
			all = append(all, name)
		}
		return all, nil
	}

	var drifted []string

	// Check each source script against manifest.
	for name, srcSHA := range sourceSHAs {
		manifestSHA, inManifest := manifest.Scripts[name]
		if !inManifest || srcSHA != manifestSHA || srcSHA == "" {
			drifted = append(drifted, name)
		}
	}

	// Check for scripts in the manifest that no longer exist in source (deleted scripts).
	for name := range manifest.Scripts {
		if _, exists := sourceSHAs[name]; !exists {
			// Only include if it would have been in scope (i.e., not announce.sh on non-Mac).
			if name == "announce.sh" && !isMac {
				continue
			}
			drifted = append(drifted, name)
		}
	}

	return drifted, nil
}

// hashFile returns the hex-encoded SHA-256 of the file at path.
func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

// ComputeScriptsSHAs returns a map of script basename → SHA256 for all *.sh and
// *.py files in <repo>/scripts/. This is called after a successful install.sh run
// to update the Scripts section of the manifest.
//
// MacStudio gate: announce.sh is excluded on non-MacStudio hosts.
func ComputeScriptsSHAs(repo string) (map[string]string, error) {
	scriptsDir := filepath.Join(repo, "scripts")

	entries, err := os.ReadDir(scriptsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]string{}, nil
		}
		return nil, fmt.Errorf("scripts SHAs: read scripts dir: %w", err)
	}

	isMac := host.IsMacStudio()
	result := make(map[string]string)

	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		ext := filepath.Ext(name)
		if ext != ".sh" && ext != ".py" {
			continue
		}
		if name == "announce.sh" && !isMac {
			continue
		}
		sha, err := hashFile(filepath.Join(scriptsDir, name))
		if err != nil {
			return nil, fmt.Errorf("scripts SHAs: hash %s: %w", name, err)
		}
		result[name] = sha
	}
	return result, nil
}
