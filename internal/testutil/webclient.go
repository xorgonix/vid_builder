package testutil

import (
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

// Client is a cookie-jar HTTP client for driving the application handler in
// tests. It never follows redirects, so status codes and Location headers are
// observable (important for Post/Redirect/Get and authorization assertions).
type Client struct {
	t     *testing.T
	http  *http.Client
	base  string
	token string
}

var csrfRe = regexp.MustCompile(`name="_csrf" value="([^"]+)"`)

// NewClient builds a redirect-preserving client around an http.Handler. The
// handler is served by an httptest server so callers can use relative paths.
func NewClient(t *testing.T, h http.Handler) *Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("testutil: cookie jar: %v", err)
	}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return &Client{t: t, base: srv.URL, http: &http.Client{
		Jar:     jar,
		Timeout: 0,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}}
}

// Request performs a request and returns the response. body may be nil. When a
// CSRF token has been captured it is attached to every mutation automatically.
func (c *Client) Request(method, path string, form url.Values, headers map[string]string) *http.Response {
	c.t.Helper()
	var body io.Reader
	if form != nil {
		cp := url.Values{}
		for k, v := range form {
			cp[k] = v
		}
		if c.token != "" && cp.Get("_csrf") == "" {
			cp.Set("_csrf", c.token)
		}
		body = strings.NewReader(cp.Encode())
	}
	req, err := http.NewRequest(method, c.base+path, body)
	if err != nil {
		c.t.Fatalf("testutil: request %s %s: %v", method, path, err)
	}
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if c.token != "" {
		req.Header.Set("X-CSRF-Token", c.token)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		c.t.Fatalf("testutil: do %s %s: %v", method, path, err)
	}
	return resp
}

// Get performs a GET.
func (c *Client) Get(path string) *http.Response {
	return c.Request(http.MethodGet, path, nil, nil)
}

// GetHX performs a GET marked as an HTMX request.
func (c *Client) GetHX(path string) *http.Response {
	return c.Request(http.MethodGet, path, nil, map[string]string{"HX-Request": "true"})
}

// PostForm performs a POST with a form body.
func (c *Client) PostForm(path string, form url.Values) *http.Response {
	return c.Request(http.MethodPost, path, form, nil)
}

// Patch performs a PATCH with a form body.
func (c *Client) Patch(path string, form url.Values) *http.Response {
	return c.Request(http.MethodPatch, path, form, nil)
}

// Delete performs a DELETE, sending an empty form so the CSRF token is attached.
func (c *Client) Delete(path string) *http.Response {
	return c.Request(http.MethodDelete, path, url.Values{}, nil)
}

// Token returns the captured CSRF token, or "" if none.
func (c *Client) Token() string { return c.token }

// Body reads and closes the response body, returning it as a string.
func Body(t *testing.T, resp *http.Response) string {
	t.Helper()
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("testutil: read body: %v", err)
	}
	return string(b)
}

// MustStatus asserts a status code and returns the body.
func MustStatus(t *testing.T, resp *http.Response, want int) string {
	t.Helper()
	body := Body(t, resp)
	if resp.StatusCode != want {
		t.Fatalf("testutil: status = %d, want %d (Location=%q body=%.200s)",
			resp.StatusCode, want, resp.Header.Get("Location"), body)
	}
	return body
}

// CSRF extracts the CSRF token from HTML, failing if absent.
func CSRF(t *testing.T, html string) string {
	t.Helper()
	m := csrfRe.FindStringSubmatch(html)
	if len(m) < 2 {
		t.Fatalf("testutil: no CSRF token in page")
	}
	return m[1]
}

// Login signs the client in as the given user and asserts success, then
// captures the session CSRF token from the account page.
func (c *Client) Login(email, password string) {
	c.t.Helper()
	page := MustStatus(c.t, c.Get("/login"), http.StatusOK)
	token := CSRF(c.t, page)
	resp := c.PostForm("/login", url.Values{"_csrf": {token}, "email": {email}, "password": {password}})
	Body(c.t, resp)
	if resp.StatusCode != http.StatusSeeOther {
		c.t.Fatalf("testutil: login status = %d, want 303", resp.StatusCode)
	}
	c.captureCSRF()
}

// captureCSRF reads a token from an authenticated page for later mutations.
func (c *Client) captureCSRF() {
	c.t.Helper()
	page := MustStatus(c.t, c.Get("/account"), http.StatusOK)
	c.token = CSRF(c.t, page)
}

// LoginAdmin signs in with the seeded bootstrap credentials.
func (c *Client) LoginAdmin() { c.Login(AdminEmail, AdminPassword) }

// Location returns the Location header of a redirect response.
func Location(resp *http.Response) string { return resp.Header.Get("Location") }
