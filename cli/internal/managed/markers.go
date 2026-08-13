// Package managed implements the managed-block marker strategy for preserving
// user customizations in settings.json (B-0096) and CLAUDE.md (B-0097) across
// bravros selfupdates.
//
// JSON strategy:
//   A sentinel key "_bravros_managed_keys" at the top level lists which
//   top-level keys are owned by bravros.  On rewrite, only those keys are
//   replaced; everything else is preserved verbatim.
//
//   Example settings.json:
//     {
//       "_bravros_managed_keys": ["hooks", "permissions"],
//       "hooks": { ... auto-generated ... },
//       "userKey": "preserved"
//     }
//
// Markdown strategy:
//   HTML comment markers bracket auto-generated regions:
//     <!-- bravros-managed:start <name> -->
//     ... auto-generated content ...
//     <!-- bravros-managed:end <name> -->
//   Content outside markers is preserved verbatim.
package managed

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
)

// RewriteJSONSection rewrites a single named section in a JSON file while
// preserving all other top-level keys.  The section name is added to the
// _bravros_managed_keys list automatically.
//
// If the file does not exist it is created from scratch.
// If the section does not exist in the file it is inserted.
// The function is idempotent: re-running with the same content is a no-op.
//
// newContent must be a JSON-serialisable value (map, slice, string, …).
func RewriteJSONSection(path, sectionName string, newContent interface{}) error {
	// Read existing file or start from empty object.
	raw := map[string]json.RawMessage{}
	if data, err := os.ReadFile(path); err == nil {
		if jsonErr := json.Unmarshal(data, &raw); jsonErr != nil {
			// Malformed JSON: back up the original and start fresh.
			_ = os.WriteFile(path+".bak", data, 0644)
			raw = map[string]json.RawMessage{}
		}
	}

	// Encode the new section content.
	encoded, err := json.Marshal(newContent)
	if err != nil {
		return fmt.Errorf("managed.RewriteJSONSection: marshal content: %w", err)
	}

	// Idempotency check: skip write if content is unchanged.
	if existing, ok := raw[sectionName]; ok && bytes.Equal(existing, encoded) {
		// Also verify the sentinel key is present.
		if isManaged(raw, sectionName) {
			return nil
		}
	}

	// Update the section.
	raw[sectionName] = json.RawMessage(encoded)

	// Update the sentinel key list.
	raw = ensureManagedKey(raw, sectionName)

	// Write back with stable key ordering (sentinel key first, then managed,
	// then user keys).
	out, err := marshalOrdered(raw)
	if err != nil {
		return fmt.Errorf("managed.RewriteJSONSection: marshal: %w", err)
	}
	return os.WriteFile(path, out, 0644)
}

// ReadJSONSection reads the named section from a JSON file.
// Returns (nil, nil) when the section is absent.
func ReadJSONSection(path, sectionName string) (json.RawMessage, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	raw := map[string]json.RawMessage{}
	if jsonErr := json.Unmarshal(data, &raw); jsonErr != nil {
		return nil, fmt.Errorf("managed.ReadJSONSection: %w", jsonErr)
	}
	return raw[sectionName], nil
}

// RewriteMarkdownSection rewrites a named HTML-comment-delimited region in a
// Markdown (or any text) file.  Content outside the markers is preserved.
//
// Markers (exact form):
//   <!-- bravros-managed:start <name> -->
//   <!-- bravros-managed:end <name> -->
//
// If the file does not exist it is created.
// If the markers are absent the section is appended to the file.
// If only one marker is present an error is returned (malformed file).
// The function is idempotent: re-running with the same content is a no-op.
func RewriteMarkdownSection(path, sectionName, newContent string) error {
	startMarker := fmt.Sprintf("<!-- bravros-managed:start %s -->", sectionName)
	endMarker := fmt.Sprintf("<!-- bravros-managed:end %s -->", sectionName)

	// Read existing file.
	var fileContent string
	if data, err := os.ReadFile(path); err == nil {
		fileContent = string(data)
	}

	startIdx := strings.Index(fileContent, startMarker)
	endIdx := strings.Index(fileContent, endMarker)

	// Validate marker pair consistency.
	if startIdx >= 0 && endIdx < 0 {
		return fmt.Errorf("managed.RewriteMarkdownSection: found start marker but no end marker for %q in %s", sectionName, path)
	}
	if startIdx < 0 && endIdx >= 0 {
		return fmt.Errorf("managed.RewriteMarkdownSection: found end marker but no start marker for %q in %s", sectionName, path)
	}

	// Build new block.
	block := startMarker + "\n" + newContent + "\n" + endMarker

	var updated string
	if startIdx < 0 {
		// Section absent: append it.
		if fileContent != "" && !strings.HasSuffix(fileContent, "\n") {
			fileContent += "\n"
		}
		updated = fileContent + "\n" + block + "\n"
	} else {
		// Section present: replace content between markers.
		endOfEnd := endIdx + len(endMarker)
		before := fileContent[:startIdx]
		after := fileContent[endOfEnd:]
		updated = before + block + after
	}

	// Idempotency: skip write if nothing changed.
	if updated == fileContent {
		return nil
	}

	return os.WriteFile(path, []byte(updated), 0644)
}

// ReadMarkdownSection reads the content between the named markers.
// Returns ("", nil) when the section is absent.
func ReadMarkdownSection(path, sectionName string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		return "", err
	}
	content := string(data)
	startMarker := fmt.Sprintf("<!-- bravros-managed:start %s -->", sectionName)
	endMarker := fmt.Sprintf("<!-- bravros-managed:end %s -->", sectionName)

	startIdx := strings.Index(content, startMarker)
	if startIdx < 0 {
		return "", nil
	}
	inner := content[startIdx+len(startMarker):]
	endIdx := strings.Index(inner, endMarker)
	if endIdx < 0 {
		return "", fmt.Errorf("managed.ReadMarkdownSection: missing end marker for %q", sectionName)
	}
	return strings.TrimSpace(inner[:endIdx]), nil
}

// ─── helpers ───────────────────────────────────────────────────────────────

// isManaged reports whether sectionName appears in _bravros_managed_keys.
func isManaged(raw map[string]json.RawMessage, sectionName string) bool {
	keysRaw, ok := raw["_bravros_managed_keys"]
	if !ok {
		return false
	}
	var keys []string
	if err := json.Unmarshal(keysRaw, &keys); err != nil {
		return false
	}
	for _, k := range keys {
		if k == sectionName {
			return true
		}
	}
	return false
}

// ensureManagedKey adds sectionName to _bravros_managed_keys if absent.
func ensureManagedKey(raw map[string]json.RawMessage, sectionName string) map[string]json.RawMessage {
	var keys []string
	if existing, ok := raw["_bravros_managed_keys"]; ok {
		_ = json.Unmarshal(existing, &keys)
	}
	found := false
	for _, k := range keys {
		if k == sectionName {
			found = true
			break
		}
	}
	if !found {
		keys = append(keys, sectionName)
	}
	encoded, _ := json.Marshal(keys)
	raw["_bravros_managed_keys"] = json.RawMessage(encoded)
	return raw
}

// marshalOrdered emits the JSON object with _bravros_managed_keys first,
// then the managed section keys, then remaining user keys — for stable output.
func marshalOrdered(raw map[string]json.RawMessage) ([]byte, error) {
	var keys []string
	if existing, ok := raw["_bravros_managed_keys"]; ok {
		_ = json.Unmarshal(existing, &keys)
	}

	// Build ordered key list.
	ordered := []string{"_bravros_managed_keys"}
	seen := map[string]bool{"_bravros_managed_keys": true}
	for _, k := range keys {
		if !seen[k] {
			ordered = append(ordered, k)
			seen[k] = true
		}
	}
	for k := range raw {
		if !seen[k] {
			ordered = append(ordered, k)
			seen[k] = true
		}
	}

	// Filter ordered down to keys that actually have a value in raw — orphan
	// entries in _bravros_managed_keys (referenced but absent from the file)
	// must not influence the comma layout, otherwise a trailing-orphan would
	// leave a stray comma after the previous key.
	filtered := ordered[:0:0]
	for _, k := range ordered {
		if _, ok := raw[k]; ok {
			filtered = append(filtered, k)
		}
	}

	var buf bytes.Buffer
	buf.WriteString("{\n")
	for i, k := range filtered {
		keyBytes, _ := json.Marshal(k)
		buf.Write(keyBytes)
		buf.WriteString(": ")
		buf.Write(raw[k])
		if i < len(filtered)-1 {
			buf.WriteString(",")
		}
		buf.WriteString("\n")
	}
	buf.WriteString("}")
	return buf.Bytes(), nil
}
