package cmd

import (
	"context"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/bravros/bravros/cli/internal/config"
)

// ---------------------------------------------------------------------------
// Server-side merge routes (B-0036)
//
// `gh pr merge` is not the only way an agent reaches a protected branch, and
// the two routes below were both proven open on 2026-09-09 while the gate was
// installed, current and unsuppressed:
//
//   gh pr merge 2044 -R paylog/ev --merge          → base lookup ran in the
//     WRONG repo (cwd), errored, and fail-open allowed it through.
//   gh api -X PUT repos/paylog/ev/pulls/2044/merge → never inspected at all.
//
// Neither performs a git push, so `.githooks/pre-push` cannot see either one:
// the merge happens server-side through the REST API. This hook is the only
// layer that can.
// ---------------------------------------------------------------------------

// ghValueFlags are the `gh` flags that consume the following argument, so a
// positional-argument scan must skip their values rather than mistake one for
// the endpoint.
var ghValueFlags = map[string]bool{
	"-X": true, "--method": true,
	"-f": true, "--field": true,
	"-F": true, "--raw-field": true,
	"-H": true, "--header": true,
	"-q": true, "--jq": true,
	"-t": true, "--template": true,
	"--input": true, "--hostname": true, "--cache": true,
	"-p": true, "--preview": true,
}

// gitGlobalValueFlags are the `git` global options that sit BEFORE the
// subcommand and consume the following argument, so `git -c k=v push` and
// `git -C dir push` still anchor on "push" rather than falling out of the gate
// entirely (PR 90 round 4).
var gitGlobalValueFlags = map[string]bool{
	"-C": true, "-c": true,
	"--git-dir": true, "--work-tree": true, "--namespace": true,
	"--exec-path": true, "--config-env": true,
}

// gitPushValueFlags are the `git push` flags that consume the following
// argument. Per `git push -h`, only these four take a SEPARATE value;
// --force-with-lease takes an optional attached one and never eats a token.
// A positional scan that does not skip these picks the flag's value as the push
// destination — `git push -o ci.skip <url> main` resolved "ci.skip"
// (PR 90 round 4, blocking).
var gitPushValueFlags = map[string]bool{
	"-o": true, "--push-option": true,
	"--repo": true, "--receive-pack": true, "--exec": true,
}

// gitPushPositionals returns the positional arguments of a `git push` — the
// repository and refspecs — with flags and the values they consume removed.
//
// Both push scanners read it, so neither can drift from the other's idea of
// what a positional is. That drift is what left `git push -o ci.skip` with a
// phantom refspec, suppressing the bare-push fallback to the current branch.
func gitPushPositionals(fields []string) []string {
	var out []string
	repo := ""
	options := true
	start := 0
	if len(fields) > start && filepathBase(unquote(fields[start])) == "git" {
		start++
	}
	if len(fields) > start {
		start++
	}
	for i := start; i < len(fields); i++ {
		f := unquote(fields[i])
		if options && f == "--" {
			options = false
			continue
		}
		if options && strings.HasPrefix(f, "--repo=") {
			repo = strings.TrimPrefix(f, "--repo=")
			continue
		}
		if options && strings.HasPrefix(f, "-") {
			if !strings.Contains(f, "=") && gitPushValueFlags[f] {
				i++
				if f == "--repo" && i < len(fields) {
					repo = unquote(fields[i])
				}
			}
			continue
		}
		out = append(out, f)
	}
	if repo != "" {
		out = append([]string{repo}, out...)
	}
	return out
}

// ghPRMergeValueFlags are the `gh pr merge` flags that consume the following
// argument. Scanning for the PR number must skip their values so a numeric flag
// value is never mistaken for the PR argument (PR 90 review, minor 1).
var ghPRMergeValueFlags = map[string]bool{
	"-R": true, "--repo": true,
	"-b": true, "--body": true,
	"-F": true, "--body-file": true,
	"-t": true, "--subject": true,
	"--match-head-commit": true,
	"--author-email":      true,
}

// ghOption keeps option values separate from flags. A jq expression or request
// body beginning with "-X" is data, not a later override of the HTTP method.
type ghOption struct {
	name, value string
}

func ghOptions(fields []string) []ghOption {
	var out []ghOption
	for i := 0; i < len(fields); i++ {
		f := unquote(fields[i])
		if f == "--" {
			break
		}
		if !strings.HasPrefix(f, "-") {
			continue
		}
		name, value, attached := strings.Cut(f, "=")
		takesValue := ghValueFlags[name] || ghPRMergeValueFlags[name]
		if !attached && !takesValue && len(f) > 2 && f[1] != '-' {
			name = f[:2]
			takesValue = ghValueFlags[name] || ghPRMergeValueFlags[name]
			if takesValue {
				value, attached = f[2:], true
			}
		}
		if takesValue && !attached && i+1 < len(fields) {
			i++
			value = unquote(fields[i])
		}
		out = append(out, ghOption{name, value})
	}
	return out
}

// ghFlagValue returns the last real occurrence, matching gh's scalar flags.
// Values of other options are never reinterpreted as options themselves.
func ghFlagValue(fields []string, names ...string) string {
	value := ""
	for _, option := range ghOptions(fields) {
		for _, name := range names {
			if option.name == name {
				value = option.value
			}
		}
	}
	return value
}

func ghHasOption(fields []string, names ...string) bool {
	for _, option := range ghOptions(fields) {
		for _, name := range names {
			if option.name == name {
				return true
			}
		}
	}
	return false
}

// ghAPIMethod models fields and --input in detached and attached forms.
func ghAPIMethod(fields []string) string {
	if method := ghFlagValue(fields, "--method", "-X"); method != "" {
		return strings.ToUpper(method)
	}
	if ghHasOption(fields, "-f", "-F", "--field", "--raw-field", "--input") {
		return "POST"
	}
	return "GET"
}

// ghAPIEndpoint skips complete flag/value pairs rather than scanning values.
func ghAPIEndpoint(fields []string) string {
	seen, literal := false, false
	for i := 0; i < len(fields); i++ {
		f := unquote(fields[i])
		if !seen {
			if f == "api" {
				seen = true
			}
			continue
		}
		if !literal && f == "--" {
			literal = true
			continue
		}
		if !literal && strings.HasPrefix(f, "-") {
			if !strings.Contains(f, "=") && (ghValueFlags[f] || ghPRMergeValueFlags[f]) {
				i++
			}
			continue
		}
		return f
	}
	return ""
}

func apiFieldValue(fields []string, key string) string {
	value := ""
	for _, option := range ghOptions(fields) {
		switch option.name {
		case "-f", "-F", "--field", "--raw-field":
			if v, ok := strings.CutPrefix(option.value, key+"="); ok {
				value = unquote(v)
			}
		}
	}
	return value
}

// apiFieldMagic reports whether a `gh api` field value is one of gh's "magic
// values" — `@file` reads the value from a file, `@-`/`-` from stdin — rather
// than the literal value itself.
//
// The hook cannot read either, so such a value carries no information about the
// merge: `gh api graphql -F query=@mutation.graphql` merges exactly as the
// inline form does, but the literal token "@mutation.graphql" contains no
// mutation name and sailed past the substring check (PR 90 review, blocking 1).
// Treated the same as a body routed through --input: content we cannot read is
// content we have not cleared.
func apiFieldMagic(v string) bool {
	return v == "-" || strings.HasPrefix(v, "@")
}

// apiFieldUnreadable reports a field value whose real content this gate cannot
// see: gh's magic values, or a value the SHELL will expand before gh sees it.
//
// `-f base=$BASE` and `-f query="$Q"` hold a variable name, not a branch or a
// mutation, so the protected-branch lookup and the mutation-name scan both
// matched nothing and returned mergeAllowed — the fail-open the whole B-0036
// three-state design exists to prevent, reached by the single most natural
// thing an agent writes (PR 90 round 12 review).
func apiFieldUnreadable(v string) bool {
	return apiFieldMagic(v) || strings.ContainsAny(v, "$`")
}

// apiTargetsProtected reports whether a `gh api` call would merge into a
// protected branch. Two REST routes reach a protected branch:
//
//	PUT  /repos/{owner}/{repo}/pulls/{n}/merge  — merge a PR (base from forge)
//	POST /repos/{owner}/{repo}/merges           — merge branches (base inline)
//
// A read-only GET on either path is not a merge and is never gated.
func apiMergeVerdict(fields []string) (mergeVerdict, string, string) {
	endpoint := ghAPIEndpoint(fields)
	if endpoint == "" {
		return mergeAllowed, "", ""
	}
	method := ghAPIMethod(fields)
	if method == "GET" || method == "HEAD" {
		return mergeAllowed, "", ""
	}

	// Keep the forge identity: identical owner/repo strings on different hosts
	// are different repositories. The PR lookup must use the request's host too.
	host := ghFlagValue(fields, "--hostname")
	p := endpoint
	if strings.Contains(p, "://") {
		parsed, err := url.Parse(p)
		if err != nil || parsed.Host == "" {
			return mergeIndeterminate, "an API request whose host could not be read", relocatedPushTarget
		}
		host, p = parsed.Host, parsed.Path
	}
	if host == "api.github.com" {
		host = "github.com"
	}
	p = strings.TrimPrefix(p, "/")
	p = strings.TrimPrefix(p, "api/v3/") // GitHub Enterprise REST prefix.
	p = strings.TrimPrefix(p, "repos/")
	if i := strings.IndexAny(p, "?#"); i >= 0 {
		p = p[:i]
	}
	targetRepo := func(repo string) string {
		if host != "" {
			return host + "/" + repo
		}
		return repo
	}
	// GraphQL is a second, entirely separate merge surface: mergePullRequest and
	// mergeBranch mutations merge server-side with no REST /pulls/{n}/merge call
	// and no git push, so neither pre-push nor the REST parsing below sees them
	// (PR 90 review, blocking 1).
	if p == "graphql" {
		return graphqlMergeVerdict(fields)
	}

	parts := strings.Split(strings.Trim(p, "/"), "/")

	// `gh api repos/{owner}/{repo}/...` with literal placeholders is valid gh
	// syntax: gh substitutes them from -R or the cwd repo. Resolve them the same
	// way rather than sending "{owner}" to the forge and failing closed on a
	// perfectly routine call (PR 90 review, minor 2).
	if len(parts) >= 2 && (strings.HasPrefix(parts[0], "{") || strings.HasPrefix(parts[1], "{")) {
		if repo := ghFlagValue(fields, "--repo", "-R"); repo != "" {
			if o, n, ok := strings.Cut(repo, "/"); ok {
				parts[0], parts[1] = o, n
			}
		} else if local := currentRepoSlug(); local != "" {
			if o, n, ok := strings.Cut(local, "/"); ok {
				parts[0], parts[1] = o, n
			}
		}
	}

	// {owner}/{repo}/merges — the base branch is a request field.
	if len(parts) == 3 && parts[2] == "merges" {
		repo := targetRepo(parts[0] + "/" + parts[1])
		base := apiFieldValue(fields, "base")
		if base == "" || apiFieldUnreadable(base) || ghHasOption(fields, "--input") {
			// Body routed through --input, or a base read from a file or stdin:
			// a merge whose target we cannot see is not a merge we have cleared.
			return mergeIndeterminate, "merging branches via the API", repo
		}
		if protectedBranches[base] {
			return mergeProtected, "merging branches into a protected branch via the API", repo
		}
		return mergeAllowed, "", repo
	}

	// {owner}/{repo}/pulls/{n}/merge — resolve the PR's base from the forge.
	if len(parts) == 5 && parts[2] == "pulls" && parts[4] == "merge" {
		repo := targetRepo(parts[0] + "/" + parts[1])
		if _, err := strconv.Atoi(parts[3]); err != nil {
			// `pulls/$PR/merge`, `pulls/${PR}/merge`, `pulls/{pull_number}/merge`.
			// This is the merge route whatever fills that slot, so a number the
			// gate cannot read is a merge it cannot clear. Returning allowed
			// here was a fail-open on B-0036's own route (round 12 review).
			return mergeIndeterminate, "merging a pull request whose number could not be read", repo
		}
		protected, ok := prBaseProtected(repo, parts[3])
		if !ok {
			return mergeIndeterminate, "merging a pull request via the API", repo
		}
		if protected {
			return mergeProtected, "merging a pull request into a protected branch via the API", repo
		}
		return mergeAllowed, "", repo
	}

	return mergeAllowed, "", ""
}

// A command-local gh environment override is not inherited by this hook's
// lookup subprocess. Detect it instead of resolving a different repository.
func commandGHContextChanged(segs [][]string) bool {
	for _, seg := range segs {
		for _, raw := range seg {
			token := unquote(raw)
			if strings.HasPrefix(token, "GH_HOST=") || strings.HasPrefix(token, "GH_REPO=") {
				return true
			}
		}
	}
	return false
}

func apiPRMergeRoute(fields []string) bool {
	endpoint := ghAPIEndpoint(fields)
	if i := strings.IndexAny(endpoint, "?#"); i >= 0 {
		endpoint = endpoint[:i]
	}
	endpoint = strings.TrimRight(endpoint, "/")
	return strings.Contains(endpoint, "/pulls/") && strings.HasSuffix(endpoint, "/merge")
}

// mergePRTarget preserves a PR URL's repository for the local opt-out. gh pr
// view accepts that URL directly, even when no -R flag accompanies the merge.
func mergePRTarget(repo, pr string) string {
	if strings.Contains(pr, "://") {
		parsed, err := url.Parse(pr)
		if err != nil || parsed.Host == "" {
			return relocatedPushTarget
		}
		parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
		if len(parts) != 4 || parts[2] != "pull" {
			return relocatedPushTarget
		}
		return parsed.Host + "/" + parts[0] + "/" + parts[1]
	}
	return repo
}

// relocatedPushTarget stands in for a push whose working tree was moved by
// `git -C` / `--git-dir`. It can never equal a real owner/repo slug, so
// policeDirectMainAllowed's equality check always fails and the local opt-out
// cannot excuse a push into a tree this process never inspected.
const relocatedPushTarget = "\x00relocated"

// policeDirectMainAllowed reports whether the repo the agent is standing in has
// EXPLICITLY opted out of the protected-branch gate via police.direct_main in
// .bravros/config.json — the escape hatch for repos that are direct-push-to-main
// by design (the paylog workspace meta-repo, /git-this personal repos).
//
// The opt-out describes the local repo, so it may only excuse a command aimed
// at that same repo. A command naming a DIFFERENT repo explicitly (gh pr merge
// -R other/repo, gh api repos/other/repo/...) is never excused by local config
// — otherwise standing in a direct-main scratch repo would unlock main
// everywhere on the machine.
//
// Called only once the gate has already decided to block, so the config read
// stays off the hot path that every Bash call pays.
func policeDirectMainAllowed(targetRepo string) bool {
	cfg, found := config.LoadBravrosConfig()
	if !found || cfg.Police == nil || !cfg.Police.DirectMain {
		return false
	}
	if targetRepo == "" {
		return true
	}
	// An unqualified gh repository defaults to its configured forge. Never
	// compare just owner/name when either side names a host explicitly.
	target := strings.TrimSuffix(strings.Trim(targetRepo, "/"), ".git")
	if len(strings.Split(target, "/")) == 2 {
		host := os.Getenv("GH_HOST")
		if host == "" {
			host = "github.com"
		}
		target = host + "/" + target
	}
	local := pushRepoIdentity(gitIn("remote", "get-url", "origin"))
	return local != "" && strings.EqualFold(local, target)
}

// gitIn runs a git command in this process's working directory and returns its
// trimmed stdout, or "" on any failure.
//
// It takes no directory: every question the gate still asks git is about the
// SESSION's repo — which repo the opt-out belongs to, and whether the pre-push
// hook is installed here. Where a push actually lands is the hook's business,
// answered in the real directory rather than a parsed one.
func gitIn(args ...string) string {
	ctx, cancel := context.WithTimeout(context.Background(), gitInTimeout)
	defer cancel()

	c := exec.CommandContext(ctx, "git", args...)
	c.WaitDelay = time.Second // see lookupPR: a lingering child must not hold the pipe
	out, err := c.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// gitInTimeout bounds each git question; a variable so tests can shrink it.
var gitInTimeout = 3 * time.Second

// currentRepoSlug returns the session repo's owner/name from its origin remote,
// or "" when that cannot be determined.
func currentRepoSlug() string {
	return repoSlugFromURL(gitIn("config", "--get", "remote.origin.url"))
}

// repoSlugFromURL extracts owner/repo from any git remote URL form:
// git@host:owner/repo.git, https://host/owner/repo.git, ssh://host/owner/repo.
func repoSlugFromURL(raw string) string {
	u := strings.TrimSuffix(strings.TrimSpace(raw), ".git")
	if i := strings.Index(u, "://"); i >= 0 {
		u = u[i+3:]
		if j := strings.Index(u, "/"); j >= 0 {
			u = u[j+1:]
		}
	} else if i := strings.Index(u, ":"); i >= 0 {
		u = u[i+1:]
	}
	parts := strings.Split(strings.Trim(u, "/"), "/")
	if len(parts) < 2 {
		return ""
	}
	return parts[len(parts)-2] + "/" + parts[len(parts)-1]
}

// graphqlMergeVerdict inspects a `gh api graphql` call for a merge mutation.
//
// Resolving a GraphQL node id (PR_kwDO...) back to a base branch would need a
// second forge round-trip, so a recognised merge mutation is reported
// indeterminate: it demands the operator's token rather than guessing. GraphQL
// merges are rare in routine flows, so the over-block costs little.
//
// A body the hook cannot read is also indeterminate — same rule the REST
// /merges route already applies to --input: content we cannot read is content
// we have not cleared.
func graphqlMergeVerdict(fields []string) (mergeVerdict, string, string) {
	query := apiFieldValue(fields, "query")
	if apiFieldUnreadable(query) || ghHasOption(fields, "--input") {
		// `-F query=@mutation.graphql`, `query=-`, `query="$Q"`: the real
		// mutation is in a file, on stdin, or in a variable the shell expands
		// after this gate has already answered.
		return mergeIndeterminate, "a GraphQL call whose query could not be read", ""
	}
	if query == "" {
		// No inline query: the mutation is in a file, on stdin, or in an
		// unexpanded variable. Only treat that as a merge attempt when the call
		// actually carries a body; a bare `gh api graphql` is malformed, not a merge.
		if ghHasOption(fields, "--input", "-f", "--field", "-F", "--raw-field") {
			return mergeIndeterminate, "a GraphQL call whose query could not be read", ""
		}
		return mergeAllowed, "", ""
	}
	// The last two do not merge synchronously, but each arms the PR to land on
	// its base once requirements are met — with no branch protection, that is
	// immediately. Same surface, delayed (PR 90 rounds 3 and 4, minor).
	for _, mutation := range mergeMutations {
		if strings.Contains(query, mutation) {
			return mergeIndeterminate, "merging via a GraphQL mutation", ""
		}
	}
	return mergeAllowed, "", ""
}

// pushTargetRepo resolves the destination repo of a `git push`, returning "" when
// the push goes to the local repo's own origin (the ordinary case).
//
// A non-empty result means the push names a repo that is not provably the
// session's, which the local police.direct_main opt-out must never excuse.
//
// Only two things resolve to "": no destination at all (the tracked upstream),
// and a destination that resolves to the session's own repo. Every other
// explicit destination returns something non-empty — an unparseable URL, an
// unknown remote name, a filesystem path — because unresolvable must not
// collapse into "local repo, allow it".
func pushTargetRepo(fields []string) string {
	dest := pushRemote(fields)
	session := pushRepoIdentity(gitIn("remote", "get-url", "origin"))
	urls := gitIn("remote", "get-url", "--push", "--all", dest)
	if urls == "" {
		urls = dest
	}
	for _, raw := range strings.Split(urls, "\n") {
		identity := pushRepoIdentity(raw)
		if session == "" || identity == "" || identity != session {
			if identity == "" {
				return raw
			}
			slug := repoSlugFromURL(raw)
			if slug != "" && !strings.EqualFold(slug, currentRepoSlug()) {
				return slug
			}
			return relocatedPushTarget
		}
	}
	return ""
}

func pushRemote(fields []string) string {
	if pos := gitPushPositionals(fields); len(pos) > 0 {
		return pos[0]
	}
	branch := gitIn("symbolic-ref", "--quiet", "--short", "HEAD")
	for _, key := range []string{"branch." + branch + ".pushRemote", "remote.pushDefault", "branch." + branch + ".remote"} {
		if remote := gitIn("config", "--get", key); remote != "" {
			return remote
		}
	}
	return "origin"
}

// Keep forge host in the identity: identical slugs on different hosts do not
// share a local opt-out. Filesystem remotes are deliberately unresolved.
func pushRepoIdentity(raw string) string {
	raw = strings.TrimSpace(raw)
	if i := strings.Index(raw, "://"); i >= 0 {
		raw = raw[i+3:]
	} else if at := strings.Index(raw, "@"); at >= 0 {
		raw = raw[at+1:]
		raw = strings.Replace(raw, ":", "/", 1)
	} else {
		return ""
	}
	if at := strings.Index(raw, "@"); at >= 0 {
		raw = raw[at+1:]
	}
	if !strings.Contains(raw, "/") {
		return ""
	}
	return strings.ToLower(strings.TrimSuffix(strings.TrimSuffix(raw, "/"), ".git"))
}

// remoteSlug resolves a named git remote to its owner/repo slug.
func remoteSlug(name string) string {
	return repoSlugFromURL(gitIn("config", "--get", "remote."+name+".url"))
}
