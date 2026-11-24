// Package skillsretired is the embedded list of skill names the toolkit used to
// ship and no longer does (renamed or removed), with what replaced them.
//
// Both install lanes — `bravros setup` and `bravros deploy` / `selfupdate` —
// consult it to remove a retired skill directory from the host skills dir,
// regardless of which lane (or none) recorded it. Without it a renamed skill
// lingers forever next to its successor: setup only prunes byte-identical
// copies of the CURRENT embed, and the deploy manifest only knows what it
// deployed itself (I-11 / D3, P-0027).
//
// Append-only. Never re-use a retired name for a new skill without removing it
// here first — the shipped-in-source guard in the prune lanes would protect it,
// but the list would then lie.
package skillsretired

// Skill is one retired skill name.
type Skill struct {
	Name        string `json:"name"`        // directory name under <host>/skills/
	Replacement string `json:"replacement"` // successor skill, "" when removed outright
	RetiredIn   string `json:"retired_in"`  // first release that no longer ships Name
}

var list = []Skill{
	{Name: "pest5-upgrade", Replacement: "laravel-pest5-upgrade", RetiredIn: "v2.18.0"},
	// RetiredIn for the P-0027 renames is fixed at publish time (the bump is
	// computed by publish-public.yml); "unreleased" until then.
	{Name: "ship", Replacement: "commit-push", RetiredIn: "unreleased (P-0027)"},
	{Name: "context", Replacement: "claudemd", RetiredIn: "unreleased (P-0027)"},
}

// All returns a copy of the retired list, in declaration order.
func All() []Skill {
	out := make([]Skill, len(list))
	copy(out, list)
	return out
}

// Names returns just the retired names, in declaration order.
func Names() []string {
	out := make([]string, 0, len(list))
	for _, s := range list {
		out = append(out, s.Name)
	}
	return out
}

// Lookup returns the entry for name.
func Lookup(name string) (Skill, bool) {
	for _, s := range list {
		if s.Name == name {
			return s, true
		}
	}
	return Skill{}, false
}
