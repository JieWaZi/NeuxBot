package localweb

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func manager(t *testing.T) *Manager {
	t.Helper()
	m, e := New("127.0.0.1:8432", "", "")
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(m.Close)
	return m
}
func request(method string) *http.Request {
	r := httptest.NewRequest(method, "http://127.0.0.1:8432/api/web/session", nil)
	r.Header.Set("Origin", "http://127.0.0.1:8432")
	return r
}
func session(t *testing.T, m *Manager) (*Session, *http.Cookie) {
	t.Helper()
	w := httptest.NewRecorder()
	s, status := m.CreateSession(w, request("POST"))
	if status != 200 {
		t.Fatalf("session: %d", status)
	}
	c := w.Result().Cookies()[0]
	if !c.HttpOnly || c.SameSite != http.SameSiteStrictMode || c.Domain != "" || c.Path != "/" {
		t.Fatal("unsafe cookie")
	}
	return s, c
}

func TestSessionCreationRequiresTrustedLocalOrigin(t *testing.T) {
	m := manager(t)
	for _, origin := range []string{"", "http://evil.example", "http://127.0.0.1:9999", "null"} {
		r := request("POST")
		r.Header.Set("Origin", origin)
		w := httptest.NewRecorder()
		if _, status := m.CreateSession(w, r); status != http.StatusForbidden || len(w.Result().Cookies()) != 0 {
			t.Fatalf("untrusted origin %q: %d", origin, status)
		}
	}
	for _, invalid := range []string{"host", "method", "authorization", "multiple-origins"} {
		r := request("POST")
		switch invalid {
		case "host":
			r.Host = "evil.example"
		case "method":
			r.Method = "GET"
		case "authorization":
			r.Header.Set("Authorization", "Bearer invalid")
		case "multiple-origins":
			r.Header.Add("Origin", "http://evil.example")
		}
		if _, status := m.CreateSession(httptest.NewRecorder(), r); status != http.StatusForbidden {
			t.Fatalf("accepted %s: %d", invalid, status)
		}
	}
	for range 32 {
		session(t, m)
	}
	if _, status := m.CreateSession(httptest.NewRecorder(), request("POST")); status != http.StatusTooManyRequests {
		t.Fatalf("unbounded sessions: %d", status)
	}
}

func TestSessionOriginCSRFAndRevocation(t *testing.T) {
	m := manager(t)
	s, c := session(t, m)
	r := request("GET")
	r.AddCookie(c)
	if _, ok := m.Authenticate(r, false); !ok {
		t.Fatal("refresh")
	}
	r.Header.Set("Authorization", "Bearer invalid")
	if _, ok := m.Authenticate(r, false); ok {
		t.Fatal("invalid bearer fell back")
	}
	r.Header.Del("Authorization")
	r.Method = "POST"
	if _, ok := m.Authenticate(r, false); ok {
		t.Fatal("missing csrf")
	}
	r.Header.Set(CSRFHeader, s.CSRF)
	if _, ok := m.Authenticate(r, false); !ok {
		t.Fatal("valid csrf")
	}
	r.Header.Set("Origin", "http://evil.example")
	if _, ok := m.Authenticate(r, false); ok {
		t.Fatal("foreign origin")
	}
	r.Header.Set("Origin", "http://127.0.0.1:8432")
	r.Host = "evil.example"
	if _, ok := m.Authenticate(r, false); ok {
		t.Fatal("foreign host")
	}
	r.Host = "127.0.0.1:8432"
	r.Method = "GET"
	r.Header.Del(CSRFHeader)
	if _, ok := m.Authenticate(r, true); !ok {
		t.Fatal("ws handshake")
	}
	r.Header.Del("Origin")
	if _, ok := m.Authenticate(r, true); ok {
		t.Fatal("ws missing origin")
	}
	m.Logout(httptest.NewRecorder(), r)
	select {
	case <-s.Context.Done():
	default:
		t.Fatal("subscription not revoked")
	}
	if _, ok := m.Authenticate(r, false); ok {
		t.Fatal("logged out")
	}
}

func TestSessionExpiresAndCoreCloses(t *testing.T) {
	m := manager(t)
	s, c := session(t, m)
	m.now = func() time.Time { return s.ExpiresAt }
	r := request("GET")
	r.AddCookie(c)
	if _, ok := m.Authenticate(r, false); ok {
		t.Fatal("expired session")
	}
	select {
	case <-s.Context.Done():
	default:
		t.Fatal("expired subscription")
	}
	m.now = time.Now
	s, _ = session(t, m)
	m.Close()
	select {
	case <-s.Context.Done():
	default:
		t.Fatal("Core close")
	}
}

func TestStaticFilesCannotEscapeOrExposeDirectories(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "index.html"), []byte("app"), 0600)
	os.Mkdir(filepath.Join(dir, "assets"), 0700)
	os.WriteFile(filepath.Join(dir, "assets", "app-hash.js"), []byte("app"), 0600)
	outside := filepath.Join(t.TempDir(), "secret")
	os.WriteFile(outside, []byte("secret"), 0600)
	if err := os.Symlink(outside, filepath.Join(dir, "secret")); err != nil {
		t.Fatal(err)
	}
	m, err := New("127.0.0.1:8432", dir, "")
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	for _, p := range []string{"/", "/assets/app-hash.js", "/assets/", "/secret", "/../secret", "/missing", "/api/v1/missing"} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "http://127.0.0.1:8432"+p, nil)
		m.ServeStatic(w, r)
		if p == "/" || strings.HasSuffix(p, ".js") {
			if w.Code != 200 {
				t.Fatal(p, w.Code)
			}
		} else if w.Code != 404 {
			t.Fatal(p, w.Code)
		}
		if strings.Contains(w.Body.String(), "secret") {
			t.Fatal("escaped static root")
		}
	}
}

func TestDataDirectoryExclusive(t *testing.T) {
	dir := t.TempDir()
	release, err := LockDataDirectory(dir)
	if err != nil {
		t.Fatal(err)
	}
	if second, err := LockDataDirectory(dir); err == nil {
		second()
		t.Fatal("second Core accepted")
	}
	if err = release(); err != nil {
		t.Fatal(err)
	}
	release, err = LockDataDirectory(dir)
	if err != nil {
		t.Fatal(err)
	}
	release()
}
