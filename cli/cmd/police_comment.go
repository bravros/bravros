package cmd

import "strings"

// commentOpening is the byte-exact opening sentence of the canonical
// `@claude review` PR comment, sourced from
// skills/pr-review/references/briefing.md. Keep this in sync with that file —
// it is the wire contract the GitHub Action's review workflow depends on.
const commentOpening = "@claude review this PR and check if we are able to merge. Analyze the code changes for any issues, security concerns, or improvements needed."

// commentRequiredBlock is the byte-exact closing block of the canonical
// `@claude review` PR comment, sourced from
// skills/pr-review/references/briefing.md. Keep this in sync with that file.
const commentRequiredBlock = `Required: end your review with EXACTLY ONE of these lines, as plain text, alone on its own line, as the final line:

BRAVROS-VERDICT: approved
BRAVROS-VERDICT: changes-requested

Do NOT wrap the line in an HTML comment, code fence, blockquote, or list item — plain visible text only. Emit approved only if you would merge this PR as-is. A finding you consider non-blocking does not prevent approved. Any blocking finding requires changes-requested.`

// canonicalCommentTemplate renders the full canonical comment body (opening +
// blank line + required block) for inclusion in a block message, so an agent
// blocked once has everything needed to retry correctly.
func canonicalCommentTemplate() string {
	return commentOpening + "\n\n" + commentRequiredBlock
}

// checkPrCommentBody inspects command for a `gh pr comment` invocation whose
// body looks like an @claude review request, and reports a non-empty block
// message when that body does not match the canonical template. An empty
// return means the command may proceed — either it is not a `gh pr comment`
// at all, its body could not be inspected as a review comment, or the body
// conforms.
func checkPrCommentBody(command string) string {
	for _, seg := range commandSegments(command) {
		if !startsWith(seg, "gh", "pr", "comment") {
			continue
		}

		body, hasBodyFile, hasBody := extractCommentBody(seg)
		if hasBodyFile {
			return commentBlockMessage(
				"This `gh pr comment` uses --body-file/-F, so the hook cannot inspect its content. " +
					"Use inline --body \"...\" instead so the comment can be validated against the canonical template.")
		}
		if !hasBody {
			continue
		}
		if !looksLikeClaudeReviewComment(body) {
			continue
		}

		normalized := normalizeCommentBody(body)
		switch {
		case !strings.HasPrefix(normalized, commentOpening):
			return commentBlockMessage("This @claude review comment does not start with the exact canonical opening sentence.")
		case !strings.HasSuffix(normalized, commentRequiredBlock):
			return commentBlockMessage("This @claude review comment does not end with the exact canonical BRAVROS-VERDICT closing block.")
		}
	}
	return ""
}

// extractCommentBody pulls the inline body text out of a `gh pr comment`
// command segment. It recognizes --body/-b (inline) and --body-file/-F
// (including "-F -" for stdin), the latter reported via hasBodyFile since its
// content is never visible to the hook.
func extractCommentBody(seg []string) (body string, hasBodyFile bool, hasBody bool) {
	for i := 0; i < len(seg); i++ {
		f := seg[i]
		switch {
		case f == "--body-file" || f == "-F" || strings.HasPrefix(f, "--body-file="):
			hasBodyFile = true
		case f == "--body" || f == "-b":
			if i+1 < len(seg) {
				body = seg[i+1]
				hasBody = true
			}
		case strings.HasPrefix(f, "--body="):
			body = strings.TrimPrefix(f, "--body=")
			hasBody = true
		}
	}
	return
}

// looksLikeClaudeReviewComment reports whether body is an @claude review
// request the cop should validate, versus some unrelated PR comment.
func looksLikeClaudeReviewComment(body string) bool {
	lower := strings.ToLower(body)
	return strings.Contains(lower, "@claude") && strings.Contains(lower, "review")
}

// normalizeCommentBody normalizes line endings and trims outer whitespace so
// CRLF-mangled but otherwise conforming bodies still validate.
func normalizeCommentBody(body string) string {
	body = strings.ReplaceAll(body, "\r\n", "\n")
	return strings.TrimSpace(body)
}

// commentBlockMessage renders a Police Block message naming what was wrong,
// followed by the full canonical template so the agent can retry in one
// round-trip.
func commentBlockMessage(reason string) string {
	return "✋🏽 Police Block: " + reason + "\n\n" +
		"The gh pr comment body must match the canonical @claude review template exactly " +
		"(extra content between the opening and the closing block is fine):\n\n" +
		canonicalCommentTemplate() + "\n"
}
