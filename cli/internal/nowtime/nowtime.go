// Package nowtime provides canonical time-formatting for the bravros CLI.
// Skills and commands that need a timestamp should call Now() so the format
// is consistent and testable in one place.
package nowtime

import "time"

// Layout is the minute-precision ISO 8601 format used throughout the bravros
// toolchain. Skills write this into plan/backlog frontmatter (created:) and
// task completion markers.
//
// Format reference: Go uses 2006-01-02T15:04 as the reference time — the
// components map to: year=2006 month=01 day=02 hour=15 minute=04.
const Layout = "2006-01-02T15:04"

// LayoutRFC3339 is the full RFC 3339 layout (seconds + timezone offset).
// Use via bravros time --format rfc3339 when callers need higher precision.
const LayoutRFC3339 = time.RFC3339

// LayoutDateOnly is the date-only layout (YYYY-MM-DD).
const LayoutDateOnly = "2006-01-02"

// Now returns the current local time formatted as YYYY-MM-DDTHH:MM.
// This is the default format for plan frontmatter and task timestamps.
func Now() string {
	return time.Now().Format(Layout)
}

// NowRFC3339 returns the current local time in RFC 3339 format.
func NowRFC3339() string {
	return time.Now().Format(LayoutRFC3339)
}

// NowDateOnly returns the current local date as YYYY-MM-DD.
func NowDateOnly() string {
	return time.Now().Format(LayoutDateOnly)
}
