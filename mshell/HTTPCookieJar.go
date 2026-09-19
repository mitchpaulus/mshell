package main

import (
	"fmt"
	"math"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"golang.org/x/net/idna"
	"golang.org/x/net/publicsuffix"
)

// httpListCookieJar adapts a normal mshell list to http.CookieJar. The list is
// the entire persistent state; its order records creation order. Each HTTP
// invocation makes a new adapter, so edits and JSON round trips are respected.
//
// No locking: http.Client calls Cookies and SetCookies from the request's own
// goroutine, one request at a time, and each invocation has its own adapter.
type httpListCookieJar struct {
	list *MShellList
	now  func() time.Time
}

var _ http.CookieJar = (*httpListCookieJar)(nil)

func newHTTPListCookieJar(obj MShellObject) (*httpListCookieJar, error) {
	list, ok := obj.(*MShellList)
	if !ok {
		return nil, fmt.Errorf("'cookieJar' must be a list of cookie dictionaries")
	}
	for i, obj := range list.Items {
		d, ok := obj.(*MShellDict)
		if !ok {
			return nil, fmt.Errorf("cookieJar[%d] must be a dictionary", i)
		}
		if err := validateHTTPCookieRecord(d); err != nil {
			return nil, fmt.Errorf("cookieJar[%d]: %w", i, err)
		}
		if j := httpCookieIndex(list.Items[:i], httpCookieKey(d)); j >= 0 {
			return nil, fmt.Errorf("cookieJar[%d]: duplicate domain/path/name", i)
		}
	}
	return &httpListCookieJar{list: list, now: time.Now}, nil
}

func cookieString(d *MShellDict, key string) string {
	return d.Items[key].(MShellString).Content
}

func cookieBool(d *MShellDict, key string) bool {
	return d.Items[key].(MShellBool).Value
}

func httpCookieKey(d *MShellDict) [3]string {
	return [3]string{cookieString(d, "domain"), cookieString(d, "path"), cookieString(d, "name")}
}

func httpCookieIndex(items []MShellObject, key [3]string) int {
	for i, obj := range items {
		if httpCookieKey(obj.(*MShellDict)) == key {
			return i
		}
	}
	return -1
}

// parseJson produces floats even for whole JSON numbers. Accept those without
// allowing fractional, non-finite, or overflowing timestamps.
func httpCookieTimestamp(obj MShellObject) (int64, bool) {
	switch value := obj.(type) {
	case MShellInt:
		return int64(value.Value), true
	case MShellFloat:
		if value.Value >= math.MinInt64 && value.Value < math.MaxInt64 && math.Trunc(value.Value) == value.Value {
			return int64(value.Value), true
		}
	}
	return 0, false
}

func validateHTTPCookieRecord(d *MShellDict) error {
	for _, key := range []string{"name", "value", "domain", "path", "sameSite"} {
		if _, ok := d.Items[key].(MShellString); !ok {
			return fmt.Errorf("'%s' must be a string", key)
		}
	}
	for _, key := range []string{"hostOnly", "secure", "httpOnly", "quoted"} {
		if _, ok := d.Items[key].(MShellBool); !ok {
			return fmt.Errorf("'%s' must be a bool", key)
		}
	}
	if _, ok := httpCookieTimestamp(d.Items["lastAccess"]); !ok {
		return fmt.Errorf("'lastAccess' must be a whole-number Unix timestamp")
	}
	if _, null := d.Items["expires"].(MShellNull); !null {
		if _, ok := httpCookieTimestamp(d.Items["expires"]); !ok {
			return fmt.Errorf("'expires' must be a whole-number Unix timestamp or null")
		}
	}
	switch cookieString(d, "sameSite") {
	case "", "lax", "strict", "none":
	default:
		return fmt.Errorf("'sameSite' must be '', 'lax', 'strict', or 'none'")
	}
	domain := cookieString(d, "domain")
	canonical, err := httpCookieHost(domain)
	if err != nil || canonical != domain || strings.HasPrefix(domain, ".") {
		return fmt.Errorf("'domain' must be a normalized hostname or IP address")
	}
	if !cookieBool(d, "hostOnly") {
		suffix, _ := publicsuffix.PublicSuffix(domain)
		if httpCookieIsIP(domain) || suffix == domain {
			return fmt.Errorf("IP addresses and public suffixes require 'hostOnly': true")
		}
	}
	c := &http.Cookie{
		Name: cookieString(d, "name"), Value: cookieString(d, "value"),
		Path: cookieString(d, "path"), Secure: cookieBool(d, "secure"),
	}
	if !strings.HasPrefix(c.Path, "/") {
		return fmt.Errorf("'path' must begin with '/'")
	}
	if err := c.Valid(); err != nil {
		return fmt.Errorf("invalid cookie name, value, or path")
	}
	if strings.HasPrefix(c.Name, "__Secure-") && !c.Secure {
		return fmt.Errorf("__Secure- cookies require 'secure': true")
	}
	if strings.HasPrefix(c.Name, "__Host-") && (!c.Secure || !cookieBool(d, "hostOnly") || c.Path != "/") {
		return fmt.Errorf("__Host- cookies require secure, hostOnly, and path '/'")
	}
	if partitioned, ok := d.Items["partitioned"]; ok {
		flag, ok := partitioned.(MShellBool)
		if !ok || flag.Value {
			return fmt.Errorf("partitioned cookies are not supported")
		}
	}
	return nil
}

func httpCookieIsIP(host string) bool {
	return strings.ContainsAny(host, ":%") || net.ParseIP(host) != nil
}

func httpCookieHost(host string) (string, error) {
	host = strings.TrimSuffix(host, ".")
	if host == "" {
		return "", fmt.Errorf("empty hostname")
	}
	if httpCookieIsIP(host) {
		// An IPv6 zone is part of the exact host, never a domain suffix.
		address := strings.SplitN(host, "%", 2)[0]
		if net.ParseIP(address) == nil {
			return "", fmt.Errorf("invalid IP address")
		}
		return strings.ToLower(host), nil
	}
	ascii, err := idna.Lookup.ToASCII(host)
	return strings.ToLower(ascii), err
}

func httpCookieDomain(host, domain string) (string, bool, bool) {
	if domain == "" {
		return host, true, true
	}
	if httpCookieIsIP(host) {
		return host, true, domain == host
	}
	domain = strings.ToLower(strings.TrimPrefix(domain, "."))
	canonical, err := httpCookieHost(domain)
	// Domain attributes must already be ASCII, with no trailing/extra dot.
	if err != nil || canonical != domain || strings.HasPrefix(domain, ".") {
		return "", false, false
	}
	suffix, _ := publicsuffix.PublicSuffix(domain)
	if suffix == domain {
		return host, true, host == domain
	}
	return domain, false, host == domain || strings.HasSuffix(host, "."+domain)
}

func httpCookieDefaultPath(path string) string {
	if !strings.HasPrefix(path, "/") {
		return "/"
	}
	if i := strings.LastIndex(path, "/"); i > 0 {
		return path[:i]
	}
	return "/"
}

func httpCookiePathMatches(requestPath, cookiePath string) bool {
	return requestPath == cookiePath || (strings.HasPrefix(requestPath, cookiePath) &&
		(strings.HasSuffix(cookiePath, "/") || requestPath[len(cookiePath)] == '/'))
}

// Returns the canonical request host and the current time, dropping expired
// cookies from the list in place, as the other list builtins do.
func (j *httpListCookieJar) begin(u *url.URL) (string, int64, bool) {
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", 0, false
	}
	host, err := httpCookieHost(u.Hostname())
	if err != nil {
		return "", 0, false
	}
	now := j.now().Unix()
	j.list.Items = slices.DeleteFunc(j.list.Items, func(obj MShellObject) bool {
		expires, ok := httpCookieTimestamp(obj.(*MShellDict).Items["expires"])
		return ok && expires <= now
	})
	return host, now, true
}

func (j *httpListCookieJar) Cookies(u *url.URL) []*http.Cookie {
	host, now, ok := j.begin(u)
	if !ok {
		return nil
	}
	path := u.Path
	if path == "" {
		path = "/"
	}
	hostIsIP := httpCookieIsIP(host)
	var selected []*MShellDict
	for _, obj := range j.list.Items {
		d := obj.(*MShellDict)
		domain := cookieString(d, "domain")
		if host != domain && (cookieBool(d, "hostOnly") || hostIsIP || !strings.HasSuffix(host, "."+domain)) {
			continue
		}
		if cookieBool(d, "secure") && u.Scheme != "https" {
			continue
		}
		if !httpCookiePathMatches(path, cookieString(d, "path")) {
			continue
		}
		d.Items["lastAccess"] = MShellInt{Value: int(now)}
		selected = append(selected, d)
	}
	// Longest path first; the stable sort keeps the list's creation order.
	slices.SortStableFunc(selected, func(a, b *MShellDict) int {
		return len(cookieString(b, "path")) - len(cookieString(a, "path"))
	})
	cookies := make([]*http.Cookie, len(selected))
	for i, d := range selected {
		cookies[i] = &http.Cookie{Name: cookieString(d, "name"), Value: cookieString(d, "value"), Quoted: cookieBool(d, "quoted")}
	}
	return cookies
}

func (j *httpListCookieJar) SetCookies(u *url.URL, cookies []*http.Cookie) {
	host, now, ok := j.begin(u)
	if !ok {
		return
	}
	for _, c := range cookies {
		// There is no top-level browsing site with which to partition a jar.
		if c.Partitioned {
			continue
		}
		domain, hostOnly, ok := httpCookieDomain(host, c.Domain)
		if !ok {
			continue
		}
		path := c.Path
		if !strings.HasPrefix(path, "/") {
			path = httpCookieDefaultPath(u.Path)
		}
		if strings.HasPrefix(c.Name, "__Secure-") && (!c.Secure || u.Scheme != "https") {
			continue
		}
		if strings.HasPrefix(c.Name, "__Host-") && (!c.Secure || u.Scheme != "https" || c.Domain != "" || c.Path != "/") {
			continue
		}
		var expires MShellObject = MShellNull{}
		remove := c.MaxAge < 0
		if c.MaxAge > 0 {
			// Avoid overflow for Max-Age values over ~290 years.
			const latest = int64(253402300799) // 9999-12-31T23:59:59Z
			deadline := latest
			if int64(c.MaxAge) < latest-now {
				deadline = now + int64(c.MaxAge)
			}
			expires = MShellInt{Value: int(deadline)}
		} else if !c.Expires.IsZero() {
			expires = MShellInt{Value: int(c.Expires.Unix())}
			remove = remove || c.Expires.Unix() <= now
		}
		sameSite := ""
		switch c.SameSite {
		case http.SameSiteLaxMode:
			sameSite = "lax"
		case http.SameSiteStrictMode:
			sameSite = "strict"
		case http.SameSiteNoneMode:
			sameSite = "none"
		}
		key := [3]string{domain, path, c.Name}
		index := httpCookieIndex(j.list.Items, key)
		if remove {
			if index >= 0 {
				j.list.Items = slices.Delete(j.list.Items, index, index+1)
			}
			continue
		}
		d := &MShellDict{Items: map[string]MShellObject{
			"name": MShellString{Content: c.Name}, "value": MShellString{Content: c.Value},
			"domain": MShellString{Content: domain}, "path": MShellString{Content: path},
			"hostOnly": MShellBool{Value: hostOnly}, "secure": MShellBool{Value: c.Secure},
			"httpOnly": MShellBool{Value: c.HttpOnly}, "quoted": MShellBool{Value: c.Quoted},
			"sameSite": MShellString{Content: sameSite}, "expires": expires,
			"lastAccess": MShellInt{Value: int(now)},
		}}
		if index >= 0 {
			j.list.Items[index] = d
		} else {
			j.list.Items = append(j.list.Items, d)
		}
	}
}
