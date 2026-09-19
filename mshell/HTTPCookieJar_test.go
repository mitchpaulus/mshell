package main

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/publicsuffix"
)

func cookieTestURL(raw string) *url.URL {
	u, err := url.Parse(raw)
	if err != nil {
		panic(err)
	}
	return u
}

func cookieTestJar(t *testing.T) *httpListCookieJar {
	t.Helper()
	j, err := newHTTPListCookieJar(NewList(0))
	if err != nil {
		t.Fatal(err)
	}
	return j
}

func cookieTestValues(cookies []*http.Cookie) []string {
	values := []string{}
	for _, c := range cookies {
		values = append(values, c.Name+"="+c.Value)
	}
	return values
}

func TestHTTPCookieJarScopeMatchesStandardJar(t *testing.T) {
	// Exercise domain, IP, public suffix, path and transport rules against Go's
	// implementation, without network access or wall-clock expiry races.
	cases := []struct {
		origin string
		cookie string
		queries []string
	}{
		{"https://Example.COM/a/login", "s=1", []string{"https://example.com/a", "https://example.com/a/b", "https://example.com/ab", "https://sub.example.com/a"}},
		{"https://www.example.com/", "s=1; Domain=.EXAMPLE.com; Path=/", []string{"https://example.com/", "https://sub.example.com/", "https://badexample.com/", "https://example.org/"}},
		{"https://www.example.com/", "s=1; Domain=other.com", []string{"https://www.example.com/", "https://other.com/"}},
		{"https://foo.co.uk/", "s=1; Domain=co.uk", []string{"https://foo.co.uk/", "https://bar.co.uk/"}},
		{"https://foo.github.io/", "s=1; Domain=github.io", []string{"https://foo.github.io/", "https://bar.github.io/"}},
		{"https://com/", "s=1; Domain=com", []string{"https://com/", "https://foo.com/"}},
		{"https://example.com/", "s=1; Domain=example.com.", []string{"https://example.com/"}},
		{"https://example.com/", "s=1; Domain=..example.com", []string{"https://example.com/"}},
		{"https://example.com/a/login", "s=1; Path=relative", []string{"https://example.com/a/x", "https://example.com/b"}},
		{"https://example.com/", "s=1; Path=/a/", []string{"https://example.com/a", "https://example.com/a/", "https://example.com/a/b"}},
		{"https://example.com/", "s=1; Secure", []string{"https://example.com/", "http://example.com/"}},
		{"http://127.0.0.1:80/", "s=1; Domain=127.0.0.1", []string{"http://127.0.0.1:90/", "http://127.0.0.2/"}},
		{"http://127.0.0.1/", "s=1; Domain=.127.0.0.1", []string{"http://127.0.0.1/"}},
		{"http://[::1]:80/", "s=1", []string{"http://[::1]:90/", "http://[::2]/"}},
		{"https://bücher.example/", "s=1", []string{"https://xn--bcher-kva.example/", "https://bücher.example/"}},
		{"https://example.com./", "s=1", []string{"https://example.com/", "https://EXAMPLE.COM./"}},
	}
	for _, tc := range cases {
		t.Run(tc.origin+" "+tc.cookie, func(t *testing.T) {
			j := cookieTestJar(t)
			standard, _ := cookiejar.New(&cookiejar.Options{PublicSuffixList: publicsuffix.List})
			c, err := http.ParseSetCookie(tc.cookie)
			if err != nil {
				t.Fatal(err)
			}
			origin := cookieTestURL(tc.origin)
			j.SetCookies(origin, []*http.Cookie{c})
			standard.SetCookies(origin, []*http.Cookie{c})
			for _, raw := range tc.queries {
				u := cookieTestURL(raw)
				got, want := cookieTestValues(j.Cookies(u)), cookieTestValues(standard.Cookies(u))
				if !reflect.DeepEqual(got, want) {
					t.Errorf("%s: got %v, want %v", raw, got, want)
				}
			}
		})
	}
}

func TestHTTPCookieJarExpirationOrderingAndJSON(t *testing.T) {
	j := cookieTestJar(t)
	now := time.Unix(1790000000, 0)
	j.now = func() time.Time { return now }
	u := cookieTestURL("https://example.com/a/login")
	j.SetCookies(u, []*http.Cookie{
		{Name: "s", Value: "root", Path: "/"},
		{Name: "s", Value: "scoped", Path: "/a", MaxAge: 10, Expires: now.Add(-time.Hour)},
		{Name: "other", Value: "v", Path: "/a", Quoted: true, HttpOnly: true, SameSite: http.SameSiteNoneMode},
	})
	j.SetCookies(u, []*http.Cookie{{Name: "s", Value: "updated", Path: "/a", MaxAge: 10}})
	want := []string{"s=updated", "other=v", "s=root"}
	if got := cookieTestValues(j.Cookies(u)); !reflect.DeepEqual(got, want) {
		t.Fatalf("order: %v, want %v", got, want)
	}
	if cookieString(j.list.Items[0].(*MShellDict), "value") != "root" {
		t.Fatal("sending cookies reordered the shared list")
	}
	var decoded any
	if err := json.Unmarshal([]byte(j.list.ToJson()), &decoded); err != nil {
		t.Fatal(err)
	}
	restored, err := newHTTPListCookieJar(ParseJsonObjToMshell(decoded))
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(9 * time.Second)
	restored.now = func() time.Time { return now }
	if got := cookieTestValues(restored.Cookies(u)); !reflect.DeepEqual(got, want) {
		t.Fatalf("JSON round trip: %v", got)
	}
	d := restored.list.Items[2].(*MShellDict)
	if !cookieBool(d, "httpOnly") || !cookieBool(d, "quoted") || cookieString(d, "sameSite") != "none" {
		t.Fatal("cookie attributes were lost")
	}
	now = now.Add(time.Second)
	if got := cookieTestValues(restored.Cookies(u)); !reflect.DeepEqual(got, []string{"other=v", "s=root"}) {
		t.Fatalf("Max-Age was not an absolute deadline: %v", got)
	}
	restored.SetCookies(u, []*http.Cookie{{Name: "s", Path: "/", MaxAge: -1}})
	restored.SetCookies(u, []*http.Cookie{{Name: "other", Path: "/a", Expires: now.Add(-time.Second)}})
	if len(restored.list.Items) != 0 {
		t.Fatal("deletions did not update the list")
	}
	// A very large Max-Age must not overflow into an already-expired cookie.
	restored.SetCookies(u, []*http.Cookie{{Name: "long", Value: "v", MaxAge: int(^uint(0) >> 1)}})
	if len(restored.Cookies(u)) != 1 {
		t.Fatal("large Max-Age overflowed")
	}
}

func TestHTTPCookieJarRejectsMalformedRecords(t *testing.T) {
	for _, obj := range []MShellObject{NewDict(), MShellString{Content: "bad"}, &MShellList{Items: []MShellObject{MShellInt{Value: 1}}}} {
		if _, err := newHTTPListCookieJar(obj); err == nil {
			t.Fatal("accepted invalid jar")
		}
	}
	mutations := map[string]func(*MShellDict){
		"missing": func(d *MShellDict) { delete(d.Items, "expires") },
		"bool": func(d *MShellDict) { d.Items["secure"] = MShellInt{Value: 1} },
		"expiry": func(d *MShellDict) { d.Items["expires"] = MShellString{Content: "soon"} },
		"fractional expiry": func(d *MShellDict) { d.Items["expires"] = MShellFloat{Value: 1.5} },
		"infinite expiry": func(d *MShellDict) { d.Items["expires"] = MShellFloat{Value: math.Inf(1)} },
		"nan access": func(d *MShellDict) { d.Items["lastAccess"] = MShellFloat{Value: math.NaN()} },
		"overflow expiry": func(d *MShellDict) { d.Items["expires"] = MShellFloat{Value: 9223372036854775808.0} },
		"path": func(d *MShellDict) { d.Items["path"] = MShellString{Content: "relative"} },
		"suffix": func(d *MShellDict) { d.Items["domain"] = MShellString{Content: "com"}; d.Items["hostOnly"] = MShellBool{Value: false} },
		"domain": func(d *MShellDict) { d.Items["domain"] = MShellString{Content: ".example.com"} },
		"value": func(d *MShellDict) { d.Items["value"] = MShellString{Content: "bad;value"} },
		"sameSite": func(d *MShellDict) { d.Items["sameSite"] = MShellString{Content: "unknown"} },
		"partitioned": func(d *MShellDict) { d.Items["partitioned"] = MShellBool{Value: true} },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			j := cookieTestJar(t)
			j.SetCookies(cookieTestURL("https://example.com/"), []*http.Cookie{{Name: "s", Value: "v"}})
			mutate(j.list.Items[0].(*MShellDict))
			if _, err := newHTTPListCookieJar(j.list); err == nil {
				t.Fatal("accepted malformed record")
			}
		})
	}
	j := cookieTestJar(t)
	j.SetCookies(cookieTestURL("https://example.com/"), []*http.Cookie{{Name: "s", Value: "v"}})
	j.list.Items = append(j.list.Items, j.list.Items[0])
	if _, err := newHTTPListCookieJar(j.list); err == nil {
		t.Fatal("accepted duplicate cookie identity")
	}
}

func TestHTTPCookieJarPrefixesAndPartitioning(t *testing.T) {
	j := cookieTestJar(t)
	u := cookieTestURL("https://example.com/")
	j.SetCookies(u, []*http.Cookie{
		{Name: "__Secure-bad", Value: "v"},
		{Name: "__Host-domain", Value: "v", Secure: true, Path: "/", Domain: "example.com"},
		{Name: "__Host-defaultPath", Value: "v", Secure: true},
		{Name: "partitioned", Value: "v", Secure: true, Partitioned: true},
		{Name: "__Host-good", Value: "v", Secure: true, Path: "/"},
	})
	j.SetCookies(cookieTestURL("http://example.com/"), []*http.Cookie{{Name: "__Host-good", Value: "overwrite", Secure: true, Path: "/"}})
	if got := cookieTestValues(j.Cookies(u)); !reflect.DeepEqual(got, []string{"__Host-good=v"}) {
		t.Fatalf("prefix/partition handling: %v", got)
	}
}

func TestHTTPCookieJarHTTPIntegration(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/login":
			if r.Method != "POST" {
				t.Error("login must use POST")
			}
			http.SetCookie(w, &http.Cookie{Name: "session", Value: "first", Path: "/"})
			http.Redirect(w, r, "/account", http.StatusSeeOther)
		case "/account":
			fmt.Fprint(w, r.Header.Get("Cookie"))
		case "/update":
			http.SetCookie(w, &http.Cookie{Name: "session", Value: "second", Path: "/"})
			w.WriteHeader(http.StatusUnauthorized)
		case "/delete":
			http.SetCookie(w, &http.Cookie{Name: "session", MaxAge: -1, Path: "/"})
		case "/broken":
			http.SetCookie(w, &http.Cookie{Name: "partial", Value: "yes", Path: "/"})
			http.Redirect(w, r, "ftp://example.com/", http.StatusFound)
		default:
			t.Errorf("unexpected network request: %s", r.URL)
		}
	}))
	defer server.Close()
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	initFile := filepath.Join(t.TempDir(), "init.msh")
	if err := os.WriteFile(initFile, nil, 0600); err != nil {
		t.Fatal(err)
	}
	run := func(script string, wantFailure bool) string {
		t.Helper()
		cmd := exec.Command(filepath.Join(root, "mshell", "mshell"), "-c", strings.ReplaceAll(script, "BASE", server.URL))
		cmd.Env = append(os.Environ(), "MSHSTDLIB="+filepath.Join(root, "lib", "std.msh"), "MSHINIT="+initFile)
		out, err := cmd.CombinedOutput()
		if (err != nil) != wantFailure {
			t.Fatalf("script error %v:\n%s", err, out)
		}
		return string(out)
	}
	out := run(`
[] jar!
{'url': 'BASE/login', 'cookieJar': @jar} httpPost ? first!
@first :body? utf8Str wl
{'url': 'BASE/update', 'cookieJar': @jar} httpGet ? :status? wl
@first :cookieJar? :0: :value? wl
@jar toJson parseJson restored!
{'url': 'BASE/account', 'cookieJar': @restored} httpGet ? :body? utf8Str wl
{'url': 'BASE/delete', 'cookieJar': @jar} httpGet ? drop
@first :cookieJar? len wl
{'url': 'BASE/broken', 'cookieJar': @jar} httpGet drop
@jar :0: :name? wl
`, false)
	if out != "session=first\n401\nsecond\nsession=second\n0\npartial\n" {
		t.Fatalf("shared jar/redirect/error/JSON behavior:\n%s", out)
	}
	out = run(`
[] jar!
{'url': 'BASE/login', 'cookieJar': @jar, 'followRedirects': false} httpPost ? :status? wl
@jar :0: :value? wl
{'url': 'BASE/account'} httpGet ? 'cookieJar' in str wl
`, false)
	if out != "303\nfirst\nfalse\n" {
		t.Fatalf("redirect disabled / no jar behavior:\n%s", out)
	}
	for _, script := range []string{
		`{'url': 'BASE/never', 'cookieJar': {}} httpGet`,
		`{'url': 'BASE/never', 'cookieJar': [1]} httpPost`,
		`{'url': 'BASE/never', 'cookieJar': [], 'headers': {'cOoKiE': ''}} httpGet`,
	} {
		out := run(script, true)
		if !strings.Contains(out, "cookieJar") && !strings.Contains(out, "cookie jar") {
			t.Fatalf("missing actionable validation error: %s", out)
		}
	}
}
