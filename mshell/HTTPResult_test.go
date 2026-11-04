package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// httpGet and httpPost give ok with the response for any status, and err
// with why when there is no response or its body cannot be read.
func TestHTTPResult(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/missing":
			w.WriteHeader(http.StatusNotFound)
		case "/short":
			// Promise more body than is sent, so reading it fails.
			w.Header().Set("Content-Length", "100")
			w.Write([]byte("partial"))
		}
	}))
	closed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	closedURL := closed.URL
	closed.Close()
	defer server.Close()

	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	initFile := filepath.Join(t.TempDir(), "init.msh")
	if err := os.WriteFile(initFile, nil, 0600); err != nil {
		t.Fatal(err)
	}
	script := `
def show (Result[HttpResponse str] -- ) match
    ok r  : @r :status? str wl,
    err e : $"err: {@e}" wl,
end end
{'url': 'BASE/missing'} httpGet show
{'url': 'BASE/missing'} httpPost show
{'url': 'BASE/short'} httpGet show
{'url': 'CLOSED/'} httpGet show
`
	script = strings.ReplaceAll(script, "BASE", server.URL)
	script = strings.ReplaceAll(script, "CLOSED", closedURL)
	cmd := exec.Command(filepath.Join(root, "mshell", "mshell"), "-c", script)
	cmd.Env = append(os.Environ(), "MSHSTDLIB="+filepath.Join(root, "lib", "std.msh"), "MSHINIT="+initFile)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("script error %v:\n%s", err, out)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) != 4 {
		t.Fatalf("expected 4 lines:\n%s", out)
	}
	if lines[0] != "404" || lines[1] != "404" {
		t.Fatalf("a 404 is a response, so it is ok:\n%s", out)
	}
	if !strings.HasPrefix(lines[2], "err: Error reading response body: ") {
		t.Fatalf("a body that cannot be read is err:\n%s", out)
	}
	if !strings.HasPrefix(lines[3], "err: Get \""+closedURL+"/\": ") {
		t.Fatalf("a refused connection is err naming the request:\n%s", out)
	}
}
