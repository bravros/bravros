package managed

// settings.go — entry-level management of the *global* Claude settings file
// (~/.claude/settings.json).
//
// RewriteJSONSection (markers.go) owns whole top-level keys: whatever it is
// handed replaces the previous value. That is far too blunt for `hooks`, which
// is shared property — the user keeps their own SessionStart/PreToolUse entries
// there and bravros only needs to contribute two of its own. So this file
// implements a finer-grained merge:
//
//   - Ownership is per ENTRY, marked with "__managed_by": "bravros".
//     Only entries carrying that marker may be replaced or removed by bravros.
//   - Every other entry, group, hook event and top-level key is preserved.
//   - `statusLine` is written only when it is absent or already bravros-owned;
//     a user-authored statusLine is never overwritten.
//
// The top-level sentinel `_bravros_managed_keys` still lists "hooks" and
// "statusLine" so the file self-documents what bravros contributes, but note
// the ownership inside those keys is entry-level, not wholesale — never feed
// them to RewriteJSONSection.

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	// ManagedByKey marks a single settings entry as bravros-owned.
	ManagedByKey = "__managed_by"
	// ManagedByValue is the marker value bravros stamps.
	ManagedByValue = "bravros"

	sessionStartEvent = "SessionStart"
	hooksKey          = "hooks"
	statusLineKey     = "statusLine"
	sentinelKey       = "_bravros_managed_keys"
	versionKey        = "_bravros_version"
	settingsVersion   = "v3"
)

// bravrosBin and guardedCommand are platform-specific — see settings_unix.go
// (the byte-identical-to-before `sh -c` form) and settings_windows.go (a
// `cmd.exe`-runnable form using %USERPROFILE%).

// managedEntry is one command entry inside a hook group. Field order is the
// emitted key order, which keeps the output byte-stable across runs.
type managedEntry struct {
	ManagedBy string `json:"__managed_by"`
	Type      string `json:"type"`
	Command   string `json:"command"`
}

// bravrosSessionStartEntries are the SessionStart hooks bravros owns.
func bravrosSessionStartEntries() []managedEntry {
	return []managedEntry{
		{ManagedByValue, "command", guardedCommand("selfupdate")},
		{ManagedByValue, "command", guardedCommand("hook verify-install-check")},
	}
}

// bravrosStatusLine is the statusLine block bravros owns.
func bravrosStatusLine() managedEntry {
	return managedEntry{ManagedByValue, "command", bravrosBin + " statusline"}
}

// SettingsSyncResult reports what SyncClaudeSettings did.
type SettingsSyncResult struct {
	Path              string
	Created           bool
	Changed           bool
	BackupPath        string
	StatusLineSkipped bool
}

// SyncClaudeSettings registers the bravros SessionStart hooks and statusline in
// the given settings.json, merging into (never overwriting) what is already
// there. It is idempotent: a second run writes nothing.
//
// The file is created when absent. Before the first modifying write the
// original is copied to <path>.bak (or .bak.1, .bak.2 … when that name is
// already taken — an existing backup is never clobbered).
func SyncClaudeSettings(path string) (SettingsSyncResult, error) {
	res := SettingsSyncResult{Path: path}

	original, err := os.ReadFile(path)
	switch {
	case err == nil:
	case errors.Is(err, os.ErrNotExist):
		res.Created = true
		original = nil
	default:
		return res, fmt.Errorf("cannot read %s: %w", path, err)
	}

	var keys []string
	vals := map[string]json.RawMessage{}
	if len(bytes.TrimSpace(original)) > 0 {
		keys, vals, err = decodeObject(original)
		if err != nil {
			return res, fmt.Errorf("%s is not a valid JSON object (%v) — fix or move it aside, then re-run; bravros will not overwrite a file it cannot parse", path, err)
		}
	}

	// hooks — merge our SessionStart entries in.
	newHooks, err := mergeHooks(vals[hooksKey])
	if err != nil {
		return res, fmt.Errorf("%s: %w", path, err)
	}
	setKey(&keys, vals, hooksKey, newHooks)

	// statusLine — only when unowned or already ours.
	managedKeys := []string{hooksKey}
	owns, err := ownsStatusLine(vals[statusLineKey])
	if err != nil {
		return res, fmt.Errorf("%s: %w", path, err)
	}
	if owns {
		encoded, mErr := json.MarshalIndent(bravrosStatusLine(), "", "  ")
		if mErr != nil {
			return res, mErr
		}
		setKey(&keys, vals, statusLineKey, encoded)
		managedKeys = append(managedKeys, statusLineKey)
	} else {
		res.StatusLineSkipped = true
	}

	// Sentinel + version bookkeeping.
	sentinel, err := json.Marshal(managedKeys)
	if err != nil {
		return res, err
	}
	setKey(&keys, vals, sentinelKey, sentinel)
	setKey(&keys, vals, versionKey, json.RawMessage(`"`+settingsVersion+`"`))

	out, err := marshalSettings(keys, vals)
	if err != nil {
		return res, err
	}

	if bytes.Equal(out, original) {
		return res, nil // idempotent no-op
	}
	res.Changed = true

	if err := assertWritable(path); err != nil {
		return res, err
	}
	if original != nil {
		backup, bErr := writeBackup(path, original)
		if bErr != nil {
			return res, bErr
		}
		res.BackupPath = backup
	}
	if err := atomicWrite(path, out, original); err != nil {
		return res, err
	}
	return res, nil
}

// ─── hooks merge ───────────────────────────────────────────────────────────

// mergeSessionStartHooks returns the new value for the top-level "hooks" key:
// the existing object with every bravros-managed SessionStart entry dropped and
// the canonical bravros group appended. Other events, other groups and
// user-authored entries are preserved.
func mergeHooks(existing json.RawMessage) (json.RawMessage, error) {
	var eventKeys []string
	eventVals := map[string]json.RawMessage{}
	if len(bytes.TrimSpace(existing)) > 0 {
		var err error
		eventKeys, eventVals, err = decodeObject(existing)
		if err != nil {
			return nil, fmt.Errorf(`"hooks" is not a JSON object (%v) — refusing to merge`, err)
		}
	}

	var groups []json.RawMessage
	if raw, ok := eventVals[sessionStartEvent]; ok {
		if err := json.Unmarshal(raw, &groups); err != nil {
			return nil, fmt.Errorf(`"hooks.SessionStart" is not a JSON array (%v) — refusing to merge`, err)
		}
	}

	kept := make([]json.RawMessage, 0, len(groups)+1)
	for _, group := range groups {
		rebuilt, drop, err := stripManagedEntries(group)
		if err != nil {
			// Not a shape we understand — preserve it untouched.
			kept = append(kept, group)
			continue
		}
		if drop {
			continue // group held nothing but bravros entries
		}
		kept = append(kept, rebuilt)
	}

	// Append our SessionStart hooks
	ours, err := json.Marshal(map[string][]managedEntry{"hooks": bravrosSessionStartEntries()})
	if err != nil {
		return nil, err
	}
	kept = append(kept, ours)

	encodedGroups, err := json.Marshal(kept)
	if err != nil {
		return nil, err
	}
	setKey(&eventKeys, eventVals, sessionStartEvent, encodedGroups)

	// -- PRETOOLUSE MERGING --
	var ptGroups []json.RawMessage
	if ptRaw, ok := eventVals["PreToolUse"]; ok {
		if err := json.Unmarshal(ptRaw, &ptGroups); err != nil {
			return nil, fmt.Errorf(`"hooks.PreToolUse" is not a JSON array (%v) — refusing to merge`, err)
		}
	}

	ptKept := make([]json.RawMessage, 0, len(ptGroups)+1)
	for _, group := range ptGroups {
		rebuilt, drop, err := stripManagedEntries(group)
		if err != nil {
			ptKept = append(ptKept, group)
			continue
		}
		if drop {
			continue
		}
		ptKept = append(ptKept, rebuilt)
	}

	ptOurs, err := json.Marshal(map[string]interface{}{"matcher": ".*", "hooks": bravrosPreToolUseEntries()})
	if err != nil {
		return nil, err
	}
	ptKept = append(ptKept, ptOurs)

	encodedPtGroups, err := json.Marshal(ptKept)
	if err != nil {
		return nil, err
	}
	setKey(&eventKeys, eventVals, "PreToolUse", encodedPtGroups)

	compact, err := encodeObject(eventKeys, eventVals)
	if err != nil {
		return nil, err
	}
	return indent(compact)
}

// stripManagedEntries removes bravros-owned entries from one hook group.
// It returns (rebuiltGroup, dropGroup, error). When the group contains no
// managed entry the original bytes are returned verbatim.
func stripManagedEntries(group json.RawMessage) (json.RawMessage, bool, error) {
	keys, vals, err := decodeObject(group)
	if err != nil {
		return nil, false, err
	}
	raw, ok := vals["hooks"]
	if !ok {
		return group, false, nil
	}
	var entries []json.RawMessage
	if err := json.Unmarshal(raw, &entries); err != nil {
		return nil, false, err
	}

	kept := make([]json.RawMessage, 0, len(entries))
	sawManaged := false
	for _, entry := range entries {
		managed, err := isManagedEntry(entry)
		if err != nil {
			return nil, false, err
		}
		// Adoption: entries written before the __managed_by marker existed
		// carry no mark but are recognizably ours by command. Without this,
		// every sync preserves the unmarked legacy copy as "user-owned" AND
		// appends a marked twin — observed live as 4x-duplicated SessionStart
		// hooks (each session start ran selfupdate four times).
		if !managed {
			managed = isLegacyBravrosEntry(entry)
		}
		if managed {
			sawManaged = true
			continue
		}
		kept = append(kept, entry)
	}
	if !sawManaged {
		return group, false, nil // untouched — preserve verbatim
	}
	if len(kept) == 0 {
		return nil, true, nil // wholly ours — drop it
	}

	encoded, err := json.Marshal(kept)
	if err != nil {
		return nil, false, err
	}
	setKey(&keys, vals, "hooks", encoded)
	rebuilt, err := encodeObject(keys, vals)
	return rebuilt, false, err
}

// isManagedEntry reports whether a hook entry carries the bravros marker.
func isManagedEntry(entry json.RawMessage) (bool, error) {
	var probe struct {
		ManagedBy string `json:"__managed_by"`
	}
	if err := json.Unmarshal(entry, &probe); err != nil {
		return false, err
	}
	return probe.ManagedBy == ManagedByValue, nil
}

// isLegacyBravrosEntry recognizes a hook entry written by a pre-marker bravros
// install: a command entry whose command invokes the bravros binary with one of
// the verbs this package has ever registered. Deliberately narrow — "bravros"
// alone is not enough (a user's own wrapper script may mention it), the verb
// must match too, so a genuinely user-authored hook is never adopted.
func isLegacyBravrosEntry(entry json.RawMessage) bool {
	var probe struct {
		Type    string `json:"type"`
		Command string `json:"command"`
	}
	if err := json.Unmarshal(entry, &probe); err != nil {
		return false
	}
	if probe.Type != "command" {
		return false
	}
	return commandLooksBravros(probe.Command)
}

// commandLooksBravros reports whether cmd invokes the bravros binary with a
// verb this package registers (hooks or statusline).
func commandLooksBravros(cmd string) bool {
	if !strings.Contains(cmd, "bravros") {
		return false
	}
	for _, verb := range []string{"bravros selfupdate", "bravros hook verify-install-check", "bravros statusline", "bravros police pretooluse"} {
		if strings.Contains(cmd, verb) {
			return true
		}
	}
	return false
}

// ownsStatusLine reports whether bravros may write the statusLine key: true
// when it is absent or already carries the bravros marker, false when the user
// authored it.
func ownsStatusLine(existing json.RawMessage) (bool, error) {
	if len(bytes.TrimSpace(existing)) == 0 || bytes.Equal(bytes.TrimSpace(existing), []byte("null")) {
		return true, nil
	}
	managed, err := isManagedEntry(existing)
	if err != nil {
		// A statusLine we cannot parse as an object is the user's business.
		return false, nil
	}
	if managed {
		return true, nil
	}
	// Adoption: a pre-marker install wrote the bravros statusline without
	// __managed_by, so every later sync warned "your own statusLine was left
	// untouched" about a statusLine that is literally `bravros statusline`.
	// Recognize it by command and take ownership (the rewrite adds the marker).
	var probe struct {
		Type    string `json:"type"`
		Command string `json:"command"`
	}
	if json.Unmarshal(existing, &probe) == nil && probe.Type == "command" && commandLooksBravros(probe.Command) {
		return true, nil
	}
	return false, nil
}

// ─── ordered JSON helpers ──────────────────────────────────────────────────

// decodeObject returns a JSON object's keys in source order plus their raw
// values, so a rewrite can preserve the author's key ordering.
func decodeObject(raw []byte) ([]string, map[string]json.RawMessage, error) {
	// json.Decoder streams, so a truncated document ends the token loop without
	// an error. Validate first — a half-written settings.json must fail loudly,
	// never be silently "recovered" into a rewrite that drops the tail.
	if !json.Valid(raw) {
		return nil, nil, fmt.Errorf("invalid JSON")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	tok, err := dec.Token()
	if err != nil {
		return nil, nil, err
	}
	if delim, ok := tok.(json.Delim); !ok || delim != '{' {
		return nil, nil, fmt.Errorf("expected a JSON object")
	}
	var keys []string
	vals := map[string]json.RawMessage{}
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return nil, nil, err
		}
		key, ok := keyTok.(string)
		if !ok {
			return nil, nil, fmt.Errorf("expected an object key, got %v", keyTok)
		}
		var val json.RawMessage
		if err := dec.Decode(&val); err != nil {
			return nil, nil, err
		}
		if _, dup := vals[key]; !dup {
			keys = append(keys, key)
		}
		vals[key] = val
	}
	return keys, vals, nil
}

// encodeObject renders keys/vals as a compact JSON object in key order.
func encodeObject(keys []string, vals map[string]json.RawMessage) ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')
	written := 0
	for _, key := range keys {
		val, ok := vals[key]
		if !ok {
			continue
		}
		if written > 0 {
			buf.WriteByte(',')
		}
		encodedKey, err := json.Marshal(key)
		if err != nil {
			return nil, err
		}
		buf.Write(encodedKey)
		buf.WriteByte(':')
		buf.Write(val)
		written++
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

// setKey assigns a value, appending the key when it is new so existing key
// order survives.
func setKey(keys *[]string, vals map[string]json.RawMessage, key string, val json.RawMessage) {
	if _, ok := vals[key]; !ok {
		*keys = append(*keys, key)
	}
	vals[key] = val
}

// indent normalises whitespace: compact first, then re-indent. Normalising
// (rather than preserving) is what makes repeated runs byte-stable.
func indent(raw []byte) ([]byte, error) {
	var compact bytes.Buffer
	if err := json.Compact(&compact, raw); err != nil {
		return nil, err
	}
	var pretty bytes.Buffer
	if err := json.Indent(&pretty, compact.Bytes(), "", "  "); err != nil {
		return nil, err
	}
	return pretty.Bytes(), nil
}

// marshalSettings renders the whole settings document: the sentinel key first,
// then every other key in its original order, 2-space indented and newline
// terminated.
func marshalSettings(keys []string, vals map[string]json.RawMessage) ([]byte, error) {
	ordered := make([]string, 0, len(keys)+1)
	if _, ok := vals[sentinelKey]; ok {
		ordered = append(ordered, sentinelKey)
	}
	for _, key := range keys {
		if key != sentinelKey {
			ordered = append(ordered, key)
		}
	}
	compact, err := encodeObject(ordered, vals)
	if err != nil {
		return nil, err
	}
	pretty, err := indent(compact)
	if err != nil {
		return nil, err
	}
	return append(pretty, '\n'), nil
}

// ─── write path ────────────────────────────────────────────────────────────

// assertWritable fails loudly when the destination cannot be written — an
// immutable file (macOS `chflags uchg`), a read-only mount or a permissions
// problem must never be a silent skip.
func assertWritable(path string) error {
	if info, err := os.Stat(path); err == nil {
		file, err := os.OpenFile(path, os.O_WRONLY, info.Mode().Perm())
		if err != nil {
			return fmt.Errorf("cannot write %s: %w\n"+
				"  If the file is locked, unlock it and re-run:\n"+
				"    chflags nouchg %s     # macOS immutable flag\n"+
				"    chmod u+w %s          # permissions", path, err, path, path)
		}
		_ = file.Close()
		return nil
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("cannot create %s: %w", dir, err)
	}
	probe, err := os.CreateTemp(dir, ".bravros-write-probe-*")
	if err != nil {
		return fmt.Errorf("cannot write into %s: %w\n  Check directory permissions, then re-run", dir, err)
	}
	name := probe.Name()
	_ = probe.Close()
	_ = os.Remove(name)
	return nil
}

// writeBackup copies the pre-modification content next to the file, never
// clobbering an existing backup: settings.json.bak, then .bak.1, .bak.2, …
func writeBackup(path string, content []byte) (string, error) {
	candidate := path + ".bak"
	for i := 1; ; i++ {
		if _, err := os.Stat(candidate); errors.Is(err, os.ErrNotExist) {
			break
		}
		if i > 100 {
			return "", fmt.Errorf("cannot find a free backup name next to %s (100 backups already present)", path)
		}
		candidate = fmt.Sprintf("%s.bak.%d", path, i)
	}
	if err := os.WriteFile(candidate, content, 0o600); err != nil {
		return "", fmt.Errorf("cannot write backup %s: %w", candidate, err)
	}
	return candidate, nil
}

// atomicWrite writes via a sibling temp file + rename so a failure can never
// leave a truncated settings.json behind. The original file mode is preserved.
func atomicWrite(path string, content, original []byte) error {
	mode := os.FileMode(0o644)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	} else if original == nil {
		mode = 0o600 // fresh file: settings can carry tokens
	}

	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".settings-*.json")
	if err != nil {
		return fmt.Errorf("cannot stage a write in %s: %w", dir, err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if _, err := tmp.Write(content); err != nil {
		tmp.Close()
		return fmt.Errorf("cannot write %s: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("cannot close %s: %w", tmpName, err)
	}
	if err := os.Chmod(tmpName, mode); err != nil {
		return fmt.Errorf("cannot chmod %s: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("cannot replace %s: %w\n"+
			"  If the file is locked, unlock it and re-run:  chflags nouchg %s", path, err, path)
	}
	return nil
}

// bravrosPreToolUseEntries are the PreToolUse hooks bravros owns.
func bravrosPreToolUseEntries() []managedEntry {
	return []managedEntry{
		{ManagedByValue, "command", guardedCommand("police pretooluse")},
	}
}

