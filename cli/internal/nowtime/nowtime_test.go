package nowtime

import (
	"regexp"
	"testing"
	"time"
)

func TestNow_Format(t *testing.T) {
	result := Now()
	re := regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}$`)
	if !re.MatchString(result) {
		t.Errorf("Now() = %q, want YYYY-MM-DDTHH:MM format", result)
	}
}

func TestNowRFC3339_Format(t *testing.T) {
	result := NowRFC3339()
	_, err := time.Parse(time.RFC3339, result)
	if err != nil {
		t.Errorf("NowRFC3339() = %q, not valid RFC 3339: %v", result, err)
	}
}

func TestNowDateOnly_Format(t *testing.T) {
	result := NowDateOnly()
	re := regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
	if !re.MatchString(result) {
		t.Errorf("NowDateOnly() = %q, want YYYY-MM-DD format", result)
	}
}

func TestNow_IsParseable(t *testing.T) {
	result := Now()
	_, err := time.Parse(Layout, result)
	if err != nil {
		t.Errorf("Now() = %q, not parseable with Layout: %v", result, err)
	}
}
