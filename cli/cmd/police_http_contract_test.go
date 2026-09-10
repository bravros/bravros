package cmd

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestHTTPOptionContract(t *testing.T) {
	const mutation = `'{"query":"mutation { mergePullRequest(input: {}) { clientMutationId } }"}'`
	for _, flags := range []string{
		"-d " + mutation, "-d" + mutation, "-sd" + mutation,
		"--data=" + mutation, "--data-raw=" + mutation,
		"--data-binary=" + mutation, "--data-urlencode=" + mutation,
		"--data-ascii=" + mutation, "--json=" + mutation,
		"-F 'query=mergePullRequest'", "-F'query=mergePullRequest'",
		"--form='query=mergePullRequest'", "--form-string='query=mergePullRequest'",
		"-T mutation-mergePullRequest.json", "-Tmutation-mergePullRequest.json",
		"--upload-file=mutation-mergePullRequest.json",
		"-sSXPOST -d " + mutation,
		"-X GET -X POST -d " + mutation,
	} {
		command := "curl " + flags + " https://api.github.com/graphql"
		if got, _ := evaluateMergeGate(command); got == mergeAllowed {
			t.Errorf("write escaped: %s", command)
		}
	}
	for _, command := range []string{
		"wget --post-data='query=mergePullRequest' https://api.github.com/graphql",
		"wget --post-file=mutation-mergePullRequest.json https://api.github.com/graphql",
		"wget --post-file mutation-mergePullRequest.json https://api.github.com/graphql",
		"wget --body-data='query=mergePullRequest' --method=POST https://api.github.com/graphql",
	} {
		if got, _ := evaluateMergeGate(command); got == mergeAllowed {
			t.Errorf("write escaped: %s", command)
		}
	}
	for _, flags := range []string{
		"-G -d " + mutation, "--get --data-raw=" + mutation,
		"-X GET -d " + mutation, "--request=HEAD --json=" + mutation,
		"-X POST -X GET -d " + mutation,
		"-I", "-i", "-D POST", "-x POST", "-H '-XPOST'",
		"--header '--data=mergePullRequest'", "--output POST",
		"--request GET --user-agent '-XPOST'", "--proxy POST",
	} {
		command := "curl " + flags + " https://api.github.com/repos/o/r/pulls/9/merge"
		if got, _ := evaluateMergeGate(command); got != mergeAllowed {
			t.Errorf("read blocked %v: %s", got, command)
		}
	}
	if !httpWrites(commandSegments(`wget --method=POST -O --method=GET https://example.test`)) {
		t.Error("wget output value treated as method")
	}
	if !httpWrites(commandSegments(`curl -XGET https://example.test -s: -d data https://example.test`)) {
		t.Error("clustered next missed second write")
	}
	if !httpWrites(commandSegments(`curl --url-query '-G' -d data https://example.test`)) {
		t.Error("URL query value treated as get option")
	}
	if !httpWrites(commandSegments(`curl -X GET https://example.test --next -d data https://example.test`)) {
		t.Error("second transfer write missed")
	}
	if !httpWrites(commandSegments(`curl -d data https://example.test --next -X GET https://example.test`)) {
		t.Error("first transfer write missed")
	}
}

// The installed curl proves request semantics against a loopback server. No
// forge endpoint or credentials are used. Every tested argument vector is also
// judged by the gate, so a parser expectation alone cannot make this test pass.
func TestCurlMethodContractAgainstLocalServer(t *testing.T) {
	curl, err := exec.LookPath("curl")
	if err != nil {
		t.Skip("curl not installed")
	}
	type request struct{ method, body string }
	requests := make(chan request, 32)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		requests <- request{r.Method, string(body)}
		w.Header().Set("Content-Length", "0")
		w.Header().Set("Connection", "close")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	mutation := `{"query":"mutation { mergePullRequest(input: {}) { clientMutationId } }"}`
	upload := filepath.Join(t.TempDir(), "mergePullRequest.json")
	if err := os.WriteFile(upload, []byte(mutation), 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		args   []string
		method string
	}{
		{[]string{"-d" + mutation}, "POST"},
		{[]string{"-sd" + mutation}, "POST"},
		{[]string{"--data-raw", mutation}, "POST"},
		{[]string{"--data-ascii", mutation}, "POST"},
		{[]string{"-Fquery=mergePullRequest"}, "POST"},
		{[]string{"--form-string", "query=mergePullRequest"}, "POST"},
		{[]string{"-T" + upload}, "PUT"},
		{[]string{"--upload-file", upload}, "PUT"},
		{[]string{"--url-query", "-G", "-d" + mutation}, "POST"},
		{[]string{"-G", "-dquery=mergePullRequest"}, "GET"},
		{[]string{"-X", "GET", "-d" + mutation}, "GET"},
		{[]string{"-X", "HEAD", "-d" + mutation}, "HEAD"},
		{[]string{"-I"}, "HEAD"},
		{[]string{"-i"}, "GET"},
		{[]string{"-H", "-XPOST"}, "GET"},
	} {
		args := append([]string{"--noproxy", "*", "--max-time", "5", "-sS"}, tc.args...)
		args = append(args, server.URL+"/graphql")
		out, err := exec.Command(curl, args...).CombinedOutput()
		if err != nil {
			t.Fatalf("curl %v: %v: %s", tc.args, err, out)
		}
		observed := <-requests
		if observed.method != tc.method {
			t.Fatalf("curl %v sent %s, want %s", tc.args, observed.method, tc.method)
		}
		segment := append([]string{"curl"}, tc.args...)
		segment = append(segment, server.URL+"/graphql")
		if got := httpWrites([][]string{segment}); got != httpWriteMethod(observed.method) {
			t.Errorf("parser disagrees with curl %v: method %s, writes %v", tc.args, observed.method, got)
		}
		if httpWriteMethod(observed.method) && !strings.Contains(observed.body, "mergePullRequest") {
			t.Fatalf("missing body for %v: %s", tc.args, observed.body)
		}
	}
}
