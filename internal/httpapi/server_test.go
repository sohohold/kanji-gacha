package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"kanji-gacha/config"
	"kanji-gacha/internal/compose"
	"kanji-gacha/internal/dict"
	"kanji-gacha/internal/model"
	"kanji-gacha/internal/store"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func setup(t *testing.T) *Server {
	t.Helper()
	c, e := config.Load("")
	if e != nil {
		t.Fatal(e)
	}
	d, e := dict.Load()
	if e != nil {
		t.Fatal(e)
	}
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "index.html"), []byte(`<html><head><!-- og-tags --></head><body></body></html>`), 0644)
	s, e := New(compose.New(c, d), store.Local{Dir: t.TempDir()}, "https://example.test", dir)
	if e != nil {
		t.Fatal(e)
	}
	return s
}
func call(s *Server, method, path, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}
func TestSaveShareOGP(t *testing.T) {
	s := setup(t)
	w := call(s, "POST", "/api/v1/glyphs", `{"mode":"kun","reading":"あお","okurigana":"い"}`)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var g model.Glyph
	if e := json.Unmarshal(w.Body.Bytes(), &g); e != nil {
		t.Fatal(e)
	}
	original := append([]byte(nil), w.Body.Bytes()...)
	s.Engine = nil // Saved retrieval must never call the generator.
	w = call(s, "GET", "/api/v1/glyphs/"+g.ID, "")
	if w.Code != 200 || !bytes.Equal(w.Body.Bytes(), original) {
		t.Fatal("saved result changed")
	}
	w = call(s, "GET", "/g/"+g.ID, "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), "https://example.test/og/"+g.ID+".png") {
		t.Fatal("missing OGP", w.Body.String())
	}
	var wg sync.WaitGroup
	imgs := make(chan []byte, 3)
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w := call(s, "GET", "/og/"+g.ID+".png", "")
			if w.Code != 200 {
				t.Error(w.Code, w.Body.String())
				return
			}
			imgs <- append([]byte(nil), w.Body.Bytes()...)
		}()
	}
	wg.Wait()
	close(imgs)
	var first []byte
	for b := range imgs {
		if first == nil {
			first = b
		} else if !bytes.Equal(first, b) {
			t.Fatal("conflicting PNGs")
		}
	}
	w = call(s, "GET", "/og/"+g.ID+".png", "")
	if !bytes.Equal(w.Body.Bytes(), first) {
		t.Fatal("image not reused")
	}
}

type broken struct{}

func (broken) Get(context.Context, string) ([]byte, error) { return nil, errors.New("offline") }
func (broken) PutIfAbsent(context.Context, string, []byte, string) error {
	return errors.New("offline")
}
func TestErrors(t *testing.T) {
	s := setup(t)
	for _, c := range []struct {
		method, path, body string
		status             int
	}{{"POST", "/api/v1/glyphs", `{"mode":"on","reading":"漢"}`, 422}, {"POST", "/api/v1/glyphs", `{"mode":"on","reading":"ア","seed":4}`, 422}, {"POST", "/api/v1/glyphs", `{"mode":"on","reading":"ア","reading":"イ"}`, 400}, {"POST", "/api/v1/glyphs", strings.Repeat("x", 1025), 413}, {"GET", "/api/v1/glyphs/no", "", 400}, {"GET", "/og/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.png", "", 404}} {
		w := call(s, c.method, c.path, c.body)
		if w.Code != c.status || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("%s: %d %s", c.path, w.Code, w.Body.String())
		}
	}
	s.Store = broken{}
	w := call(s, "POST", "/api/v1/glyphs", `{"mode":"on","reading":"セイ"}`)
	if w.Code != 503 || strings.Contains(w.Body.String(), `"id"`) {
		t.Fatal("unsaved success", w.Code, w.Body.String())
	}
}

// conflictStore simulates the loser of a concurrent first OGP request: the
// conditional PUT is rejected while the winner's object is not yet readable.
type conflictStore struct {
	store.Store
	png string
}

func (c conflictStore) Get(ctx context.Context, k string) ([]byte, error) {
	if k == c.png {
		return nil, store.ErrNotFound
	}
	return c.Store.Get(ctx, k)
}
func (c conflictStore) PutIfAbsent(ctx context.Context, k string, b []byte, ct string) error {
	if k == c.png {
		return store.ErrExists
	}
	return c.Store.PutIfAbsent(ctx, k, b, ct)
}

func TestOGPConflictReturnsImage(t *testing.T) {
	s := setup(t)
	w := call(s, "POST", "/api/v1/glyphs", `{"mode":"on","reading":"セイ"}`)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var g model.Glyph
	if e := json.Unmarshal(w.Body.Bytes(), &g); e != nil {
		t.Fatal(e)
	}
	s.Store = conflictStore{s.Store, g.OGPKey}
	w = call(s, "GET", "/og/"+g.ID+".png", "")
	if w.Code != 200 {
		t.Fatalf("losing the PUT race must still serve the image, got %d %s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "image/png" {
		t.Fatalf("content type %q", ct)
	}
	if !bytes.HasPrefix(w.Body.Bytes(), []byte("\x89PNG\r\n\x1a\n")) {
		t.Fatal("body is not a PNG")
	}
}

func TestMethodNotAllowedIsProblem(t *testing.T) {
	s := setup(t)
	for _, path := range []string{"/g/" + strings.Repeat("a", 32), "/og/" + strings.Repeat("a", 32) + ".png", "/", "/healthz", "/assets/index.js"} {
		w := call(s, "POST", path, "")
		if w.Code != 405 {
			t.Errorf("%s: status %d", path, w.Code)
		}
		if ct := w.Header().Get("Content-Type"); ct != "application/problem+json" {
			t.Errorf("%s: content type %q", path, ct)
		}
		if cc := w.Header().Get("Cache-Control"); cc != "no-store" {
			t.Errorf("%s: cache control %q", path, cc)
		}
		if a := w.Header().Get("Allow"); a != "GET, HEAD" {
			t.Errorf("%s: allow %q", path, a)
		}
	}
}

// A wrong method on an API resource is a 405 naming the methods it routes, not
// a 404 that denies the resource exists.
func TestAPIMethodNotAllowed(t *testing.T) {
	s := setup(t)
	id := strings.Repeat("a", 32)
	for _, c := range []struct{ method, path, allow string }{
		{"POST", "/api/v1/glyphs/" + id, "GET, HEAD"},
		{"DELETE", "/api/v1/glyphs/" + id, "GET, HEAD"},
		{"GET", "/api/v1/glyphs", "POST"},
		{"DELETE", "/api/v1/glyphs", "POST"},
	} {
		w := call(s, c.method, c.path, "")
		if w.Code != 405 {
			t.Errorf("%s %s: status %d", c.method, c.path, w.Code)
		}
		if a := w.Header().Get("Allow"); a != c.allow {
			t.Errorf("%s %s: allow %q, want %q", c.method, c.path, a, c.allow)
		}
		if ct := w.Header().Get("Content-Type"); ct != "application/problem+json" {
			t.Errorf("%s %s: content type %q", c.method, c.path, ct)
		}
	}
	// An API path with no route at all stays a 404.
	for _, method := range []string{"GET", "POST"} {
		w := call(s, method, "/api/v1/unknown", "")
		if w.Code != 404 {
			t.Errorf("%s /api/v1/unknown: status %d", method, w.Code)
		}
		if a := w.Header().Get("Allow"); a != "" {
			t.Errorf("%s /api/v1/unknown: advertises Allow %q", method, a)
		}
	}
}

func TestRateLimitUsesViewerAddress(t *testing.T) {
	s := setup(t)
	s.TrustCloudFront = true
	r := httptest.NewRequest("POST", "/api/v1/glyphs", nil)
	r.Header.Set("X-Forwarded-For", "203.0.113.9, 70.132.1.1")
	r.Header.Set("CloudFront-Viewer-Address", "198.51.100.7:52011")
	for i := 0; i < 10; i++ {
		if !s.allow(r) {
			t.Fatalf("viewer 198.51.100.7 exhausted after %d requests", i)
		}
	}
	if _, ok := s.ips["198.51.100.7"]; !ok {
		t.Fatalf("rate limit keyed on %v, not the viewer address", s.ips)
	}
	other := httptest.NewRequest("POST", "/api/v1/glyphs", nil)
	other.Header.Set("X-Forwarded-For", "203.0.113.9, 70.132.1.1")
	other.Header.Set("CloudFront-Viewer-Address", "198.51.100.8:41000")
	if !s.allow(other) {
		t.Fatal("a second viewer behind the same edge must have its own bucket")
	}
}

// A path no GET route serves is a missing resource, not a wrong method.
func TestUnknownPathIsNotFound(t *testing.T) {
	s := setup(t)
	for _, method := range []string{"GET", "POST", "DELETE"} {
		w := call(s, method, "/does-not-exist", "")
		if w.Code != 404 {
			t.Errorf("%s /does-not-exist: status %d", method, w.Code)
		}
		if ct := w.Header().Get("Content-Type"); ct != "application/problem+json" {
			t.Errorf("%s /does-not-exist: content type %q", method, ct)
		}
		if a := w.Header().Get("Allow"); a != "" {
			t.Errorf("%s /does-not-exist: advertises Allow %q", method, a)
		}
	}
}

func TestViewerIP(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"198.51.100.7:52011", "198.51.100.7"},
		{"[2001:db8::1]:52011", "2001:db8::1"},
		{"2001:db8::1:52011", "2001:db8::1"},
		// A port that is also a valid hex group must not be kept: the whole
		// value parses as an address, and the port changes per connection.
		{"2001:db8::1:443", "2001:db8::1"},
		{"2001:db8::1:80", "2001:db8::1"},
		{"198.51.100.7", "198.51.100.7"},
		// Unbracketed IPv6 with no port cannot be told apart from one with a
		// port, so it is dropped rather than keyed ambiguously.
		{"2001:db8::1", ""},
		{"", ""},
		{"not-an-address", ""},
		{"example.test:80", ""},
	} {
		if got := viewerIP(c.in); got != c.want {
			t.Errorf("viewerIP(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// An IPv6 viewer must get its own bucket rather than falling back to the
// adapter's RemoteAddr, which every viewer shares.
func TestRateLimitIPv6Viewer(t *testing.T) {
	s := setup(t)
	s.TrustCloudFront = true
	r := httptest.NewRequest("POST", "/api/v1/glyphs", nil)
	r.Header.Set("CloudFront-Viewer-Address", "[2001:db8::1]:52011")
	if !s.allow(r) {
		t.Fatal("first request rejected")
	}
	if _, ok := s.ips["2001:db8::1"]; !ok {
		t.Fatalf("rate limit keyed on %v, not the viewer address", s.ips)
	}
}

// A viewer must not earn a fresh bucket by reconnecting from a new source port.
func TestRateLimitIgnoresViewerPort(t *testing.T) {
	s := setup(t)
	s.TrustCloudFront = true
	for _, addr := range []string{"2001:db8::1:443", "2001:db8::1:80", "2001:db8::1:52011"} {
		r := httptest.NewRequest("POST", "/api/v1/glyphs", nil)
		r.Header.Set("CloudFront-Viewer-Address", addr)
		s.allow(r)
	}
	if len(s.ips) != 1 {
		t.Fatalf("one viewer took %d buckets: %v", len(s.ips), s.ips)
	}
	if _, ok := s.ips["2001:db8::1"]; !ok {
		t.Fatalf("rate limit keyed on %v", s.ips)
	}
}
