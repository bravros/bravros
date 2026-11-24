package skillsretired

import "testing"

func TestListContract(t *testing.T) {
	want := map[string]string{
		"pest5-upgrade": "laravel-pest5-upgrade",
		"ship":          "commit-push",
		"context":       "claudemd",
	}
	seen := map[string]bool{}
	for _, s := range All() {
		if s.Name == "" || s.RetiredIn == "" {
			t.Errorf("incomplete entry: %+v", s)
		}
		if seen[s.Name] {
			t.Errorf("duplicate retired name %q", s.Name)
		}
		seen[s.Name] = true
		if s.Name == s.Replacement {
			t.Errorf("%q replaces itself", s.Name)
		}
	}
	for name, repl := range want {
		got, ok := Lookup(name)
		if !ok || got.Replacement != repl {
			t.Errorf("Lookup(%q) = %+v, %v; want replacement %q", name, got, ok, repl)
		}
	}
	if _, ok := Lookup("commit-push"); ok {
		t.Error("a live skill must not be retired")
	}
	if len(Names()) != len(All()) {
		t.Error("Names and All disagree")
	}
}

func TestAllReturnsCopy(t *testing.T) {
	a := All()
	a[0].Name = "mutated"
	if All()[0].Name == "mutated" {
		t.Error("All must not expose the backing slice")
	}
}
