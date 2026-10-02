package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"kanji-gacha/config"
	"kanji-gacha/internal/compose"
	"kanji-gacha/internal/dict"
	"kanji-gacha/internal/metrics"
	"kanji-gacha/internal/model"
	"kanji-gacha/internal/store"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/pb33f/libopenapi"
	validator "github.com/pb33f/libopenapi-validator"
	"testing"
	"time"
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

// Test ID: API-01
func TestSaveShareOGP(t *testing.T) {
	s := setup(t)
	// Three concurrent renders under the race detector can outlast the
	// production limit on a busy CI runner; this test is about reuse, not speed.
	s.OGPRender = time.Minute
	w := call(s, "POST", "/api/v1/glyphs", `{"mode":"kun","reading":"あお","okurigana":"い"}`)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if cc := w.Header().Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("POST cache control %q", cc)
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
	const year = "public, max-age=31536000, immutable"
	if cc := w.Header().Get("Cache-Control"); cc != year {
		t.Errorf("result cache control %q", cc)
	}
	w = call(s, "GET", "/g/"+g.ID, "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), "https://example.test/og/"+g.ID+".png") {
		t.Fatal("missing OGP", w.Body.String())
	}
	if cc := w.Header().Get("Cache-Control"); cc != "public, max-age=86400" {
		t.Errorf("share page cache control %q", cc)
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
	if cc := w.Header().Get("Cache-Control"); cc != year {
		t.Errorf("OGP cache control %q", cc)
	}
}

// problemOf decodes an RFC 9457 body and checks the members every problem
// carries: the status matches the response, and code says which problem.
func problemOf(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var p map[string]any
	if e := json.Unmarshal(w.Body.Bytes(), &p); e != nil {
		t.Fatalf("%d: body %q is not JSON", w.Code, w.Body.String())
	}
	if p["type"] != "about:blank" || p["title"] == "" || p["status"] != float64(w.Code) || p["code"] == "" || p["code"] == nil {
		t.Fatalf("%d: problem %v", w.Code, p)
	}
	return p
}

type broken struct{}

func (broken) Get(context.Context, string) ([]byte, error) { return nil, errors.New("offline") }
func (broken) PutIfAbsent(context.Context, string, []byte, string) error {
	return errors.New("offline")
}

// Test ID: API-02
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
		problemOf(t, w)
	}
	// A validation error names the field, the code, and where in the
	// normalized value the bad character sits.
	p := problemOf(t, call(s, "POST", "/api/v1/glyphs", `{"mode":"on","reading":"せ漢"}`))
	errs, _ := p["errors"].([]any)
	if len(errs) != 1 {
		t.Fatalf("errors %v", p["errors"])
	}
	if e := errs[0].(map[string]any); e["field"] != "reading" || e["code"] != "character" || e["position"] != 1.0 || e["normalized"] != "セ漢" {
		t.Fatalf("validation error %v", e)
	}
	// Exactly 1 KiB is still accepted.
	body := `{"mode":"on","reading":"セイ"}`
	body += strings.Repeat(" ", 1024-len(body))
	if w := call(s, "POST", "/api/v1/glyphs", body); w.Code != 200 {
		t.Fatalf("1024-byte body: %d %s", w.Code, w.Body.String())
	}
	s.Store = broken{}
	w := call(s, "POST", "/api/v1/glyphs", `{"mode":"on","reading":"セイ"}`)
	if w.Code != 503 || strings.Contains(w.Body.String(), `"id"`) {
		t.Fatal("unsaved success", w.Code, w.Body.String())
	}
}

// Test ID: API-03
// A share link is opened in a browser, so one that cannot be served answers
// with the SPA, which explains the failure, rather than raw problem+json. The
// status is kept, and the page is neither cached nor carries OGP tags.
func TestShareErrorServesSPA(t *testing.T) {
	s := setup(t)
	id := strings.Repeat("a", 32)
	for _, c := range []struct {
		path   string
		status int
		store  store.Store
	}{
		{"/g/no", 400, nil},
		{"/g/" + id, 404, nil},
		{"/g/" + id + "/", 404, nil},
		{"/g/", 404, nil},
		{"/g/" + id, 503, broken{}},
	} {
		if c.store != nil {
			s.Store = c.store
		}
		w := call(s, "GET", c.path, "")
		if w.Code != c.status {
			t.Errorf("%s: status %d, want %d", c.path, w.Code, c.status)
		}
		if ct := w.Header().Get("Content-Type"); ct != "text/html; charset=utf-8" {
			t.Errorf("%s: content type %q", c.path, ct)
		}
		if cc := w.Header().Get("Cache-Control"); cc != "no-store" {
			t.Errorf("%s: cache control %q", c.path, cc)
		}
		if b := w.Body.String(); !bytes.Equal(w.Body.Bytes(), s.index) || strings.Contains(b, "og:") {
			t.Errorf("%s: body %q", c.path, b)
		}
	}
	// The API keeps answering the same failures with problem+json.
	w := call(s, "GET", "/api/v1/glyphs/"+id, "")
	if w.Code != 503 || w.Header().Get("Content-Type") != "application/problem+json" {
		t.Errorf("api: %d %q", w.Code, w.Header().Get("Content-Type"))
	}
}

// conflictStore simulates the loser of a concurrent first OGP request: the
// conditional PUT is rejected, and the winner's object only becomes readable
// after misses (or never, when readErr is set to a real failure).
type conflictStore struct {
	store.Store
	png     string
	misses  int
	winner  []byte
	readErr error
	reads   int
}

func (c *conflictStore) Get(ctx context.Context, k string) ([]byte, error) {
	if k == c.png {
		c.reads++
		if c.reads == 1 {
			return nil, store.ErrNotFound // nothing cached yet, so the handler renders
		}
		if c.readErr != nil {
			return nil, c.readErr
		}
		if c.reads-1 <= c.misses {
			return nil, store.ErrNotFound
		}
		return c.winner, nil
	}
	return c.Store.Get(ctx, k)
}
func (c *conflictStore) PutIfAbsent(ctx context.Context, k string, b []byte, ct string) error {
	if k == c.png {
		return store.ErrExists
	}
	return c.Store.PutIfAbsent(ctx, k, b, ct)
}

func savedGlyph(t *testing.T, s *Server) model.Glyph {
	t.Helper()
	w := call(s, "POST", "/api/v1/glyphs", `{"mode":"on","reading":"セイ"}`)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var g model.Glyph
	if e := json.Unmarshal(w.Body.Bytes(), &g); e != nil {
		t.Fatal(e)
	}
	return g
}

// Test ID: API-04
// The loser of the race serves the object that actually won the write, not its
// own render, which a rolling deployment could have produced differently.
func TestOGPConflictReturnsStoredImage(t *testing.T) {
	s := setup(t)
	g := savedGlyph(t, s)
	winner := []byte("\x89PNG\r\n\x1a\nwinning bytes")
	// The first two rereads miss, as they would while the winner's write lands.
	s.Store = &conflictStore{Store: s.Store, png: g.OGPKey, misses: 2, winner: winner}
	w := call(s, "GET", "/og/"+g.ID+".png", "")
	if w.Code != 200 {
		t.Fatalf("losing the PUT race must still serve the image, got %d %s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "image/png" {
		t.Fatalf("content type %q", ct)
	}
	if !bytes.Equal(w.Body.Bytes(), winner) {
		t.Fatalf("served %d bytes of its own render instead of the stored object", w.Body.Len())
	}
}

// Test ID: API-05
// A storage or authentication failure must not be reported as a served image,
// least of all under a one-year immutable cache policy.
func TestOGPConflictKeepsReadError(t *testing.T) {
	s := setup(t)
	g := savedGlyph(t, s)
	cs := &conflictStore{Store: s.Store, png: g.OGPKey, readErr: errors.New("access denied")}
	s.Store = cs
	w := call(s, "GET", "/og/"+g.ID+".png", "")
	if w.Code != 503 {
		t.Fatalf("status %d, want 503", w.Code)
	}
	if cc := w.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("cache control %q", cc)
	}
	if cs.reads != 2 { // the initial lookup, then one reread that must not be retried
		t.Errorf("read the object %d times, want 2", cs.reads)
	}
}

// failingPNG stores glyphs normally but has no PNG yet and cannot save one.
type failingPNG struct {
	store.Store
	png string
}

func (f failingPNG) Get(ctx context.Context, k string) ([]byte, error) {
	if k == f.png {
		return nil, store.ErrNotFound
	}
	return f.Store.Get(ctx, k)
}
func (f failingPNG) PutIfAbsent(ctx context.Context, k string, b []byte, ct string) error {
	if k == f.png {
		return errors.New("offline")
	}
	return f.Store.PutIfAbsent(ctx, k, b, ct)
}

// Test ID: API-15
// A first OGP request whose image cannot be drawn or saved is a 503 that no
// cache keeps, so a later request can still produce the image.
func TestOGPRenderOrSaveFailure(t *testing.T) {
	for _, c := range []struct {
		name  string
		tweak func(*Server, model.Glyph)
	}{
		{"render", func(s *Server, _ model.Glyph) { s.OGPRender = time.Nanosecond }},
		{"save", func(s *Server, g model.Glyph) { s.Store = failingPNG{Store: s.Store, png: g.OGPKey} }},
	} {
		s := setup(t)
		g := savedGlyph(t, s)
		c.tweak(s, g)
		w := call(s, "GET", "/og/"+g.ID+".png", "")
		if w.Code != 503 {
			t.Errorf("%s: status %d, want 503", c.name, w.Code)
		}
		if cc := w.Header().Get("Cache-Control"); cc != "no-store" {
			t.Errorf("%s: cache control %q", c.name, cc)
		}
	}
}

// Test ID: API-06
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
		problemOf(t, w)
	}
}

// Test ID: API-07
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
		problemOf(t, w)
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

// Test ID: API-08
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

// Test ID: API-09
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
		problemOf(t, w)
	}
}

// Test ID: API-10
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

// Test ID: API-11
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
	if _, ok := s.ips["2001:db8::/64"]; !ok {
		t.Fatalf("rate limit keyed on %v, not the viewer's /64", s.ips)
	}
}

// Test ID: API-12
// Addresses within one /64 belong to one subscriber, who can pick any of them.
func TestRateLimitGroupsIPv6By64(t *testing.T) {
	s := setup(t)
	s.TrustCloudFront = true
	for i := 0; i < 10; i++ {
		r := httptest.NewRequest("POST", "/api/v1/glyphs", nil)
		r.Header.Set("CloudFront-Viewer-Address", fmt.Sprintf("2001:db8:0:1:%x::1:52011", i+1))
		if !s.allow(r) {
			t.Fatalf("exhausted after %d requests", i)
		}
	}
	r := httptest.NewRequest("POST", "/api/v1/glyphs", nil)
	r.Header.Set("CloudFront-Viewer-Address", "2001:db8:0:1:ffff::1:52011")
	if s.allow(r) {
		t.Fatalf("a fresh address in the same /64 got a fresh bucket: %v", s.ips)
	}
	r.Header.Set("CloudFront-Viewer-Address", "2001:db8:0:2::1:52011")
	if !s.allow(r) {
		t.Fatal("a neighbouring /64 must have its own bucket")
	}
}

// Test ID: API-13
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
	if _, ok := s.ips["2001:db8::/64"]; !ok {
		t.Fatalf("rate limit keyed on %v", s.ips)
	}
}

// Test ID: API-14
// Each draw writes one metric record naming its rarity and nothing that
// identifies the request; requests that never reach the generator write none.
func TestGenerateMetrics(t *testing.T) {
	s := setup(t)
	var buf bytes.Buffer
	s.Metrics = metrics.NewEMF(&buf, "KanjiGacha")
	w := call(s, "POST", "/api/v1/glyphs", `{"mode":"on","reading":"セイ"}`)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var g model.Glyph
	json.Unmarshal(w.Body.Bytes(), &g)
	lines := strings.Split(strings.TrimSuffix(buf.String(), "\n"), "\n")
	if len(lines) != 1 {
		t.Fatalf("want one record, got %q", buf.String())
	}
	var rec map[string]any
	if e := json.Unmarshal([]byte(lines[0]), &rec); e != nil {
		t.Fatal(e)
	}
	fb := 0.0
	if g.Fallback {
		fb = 1
	}
	if rec["Rarity"] != g.Rarity || rec["Glyphs"] != 1.0 || rec["Fallback"] != fb {
		t.Fatalf("record %v does not match glyph rarity %s fallback %v", rec, g.Rarity, g.Fallback)
	}
	if ms, ok := rec["GenerationTime"].(float64); !ok || ms < 0 {
		t.Fatal("generation time", rec["GenerationTime"])
	}
	for _, leak := range []string{g.ID, "セイ", g.Reading.Display, "svg", "127.0.0.1", "192.0.2.1"} {
		if leak != "" && strings.Contains(buf.String(), leak) {
			t.Fatalf("record carries %q: %s", leak, buf.String())
		}
	}

	// A save failure is still a completed draw.
	buf.Reset()
	s.Store = broken{}
	if w := call(s, "POST", "/api/v1/glyphs", `{"mode":"on","reading":"セイ"}`); w.Code != 503 {
		t.Fatal(w.Code)
	}
	if strings.Count(buf.String(), `"Glyphs":1`) != 1 {
		t.Fatalf("save failure: %q", buf.String())
	}

	buf.Reset()
	call(s, "POST", "/api/v1/glyphs", `{"mode":"on","reading":"漢"}`)
	s.GenerationEnabled = false
	call(s, "POST", "/api/v1/glyphs", `{"mode":"on","reading":"セイ"}`)
	s.GenerationEnabled = true
	s.ips["192.0.2.1"] = bucket{tokens: 0, seen: s.Now()}
	if w := call(s, "POST", "/api/v1/glyphs", `{"mode":"on","reading":"セイ"}`); w.Code != 429 {
		t.Fatal("rate limit not reached", w.Code)
	}
	if buf.Len() != 0 {
		t.Fatalf("rejected requests recorded metrics: %q", buf.String())
	}
}

// Test ID: API-16
// Generation takes JSON only; another or missing Content-Type is a 415.
func TestRejectNonJSON(t *testing.T) {
	s := setup(t)
	for _, ct := range []string{"text/plain", "application/x-www-form-urlencoded", ""} {
		r := httptest.NewRequest("POST", "/api/v1/glyphs", strings.NewReader(`{"mode":"on","reading":"セイ"}`))
		if ct != "" {
			r.Header.Set("Content-Type", ct)
		}
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		if w.Code != 415 || w.Header().Get("Content-Type") != "application/problem+json" {
			t.Errorf("%q: %d %s", ct, w.Code, w.Header().Get("Content-Type"))
		}
	}
}

// colliding rejects the first collisions glyph writes as if the ID were taken.
type colliding struct {
	store.Store
	collisions int
	keys       []string
}

func (c *colliding) PutIfAbsent(ctx context.Context, k string, b []byte, ct string) error {
	if strings.HasPrefix(k, "glyphs/") {
		c.keys = append(c.keys, k)
		if len(c.keys) <= c.collisions {
			return store.ErrExists
		}
	}
	return c.Store.PutIfAbsent(ctx, k, b, ct)
}

// Test ID: API-17
// An ID collision is retried under a fresh ID, and the response names the ID
// that was actually saved.
func TestIDCollisionRetries(t *testing.T) {
	s := setup(t)
	c := &colliding{Store: s.Store, collisions: 1}
	s.Store = c
	w := call(s, "POST", "/api/v1/glyphs", `{"mode":"on","reading":"セイ"}`)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var g model.Glyph
	if e := json.Unmarshal(w.Body.Bytes(), &g); e != nil {
		t.Fatal(e)
	}
	if len(c.keys) != 2 || c.keys[0] == c.keys[1] || c.keys[1] != "glyphs/"+g.ID+".json" {
		t.Fatalf("wrote %v, answered %s", c.keys, g.ID)
	}
	if w := call(s, "GET", "/api/v1/glyphs/"+g.ID, ""); w.Code != 200 {
		t.Fatalf("saved glyph not found: %d", w.Code)
	}
	// Repeated collisions end in a 503 rather than an unsaved link.
	s.Store = &colliding{Store: c.Store, collisions: 100}
	if w := call(s, "POST", "/api/v1/glyphs", `{"mode":"on","reading":"セイ"}`); w.Code != 503 || strings.Contains(w.Body.String(), `"id"`) {
		t.Fatalf("after collisions: %d %s", w.Code, w.Body.String())
	}
}

// deadlines records the deadline of every storage call.
type deadlines struct {
	store.Store
	at []time.Time
}

func (d *deadlines) record(ctx context.Context) {
	dl, _ := ctx.Deadline()
	d.at = append(d.at, dl)
}
func (d *deadlines) Get(ctx context.Context, k string) ([]byte, error) {
	d.record(ctx)
	return d.Store.Get(ctx, k)
}
func (d *deadlines) PutIfAbsent(ctx context.Context, k string, b []byte, ct string) error {
	d.record(ctx)
	return d.Store.PutIfAbsent(ctx, k, b, ct)
}

// Test ID: API-18
// Drawing an OGP image is limited to 3s by default, and the storage calls of
// the request run under the request's remaining budget, inside the Lambda's 10s.
func TestOGPTimeBudget(t *testing.T) {
	s := setup(t)
	if s.OGPRender != 3*time.Second {
		t.Fatalf("default OGP render limit %v, want 3s", s.OGPRender)
	}
	g := savedGlyph(t, s)
	d := &deadlines{Store: s.Store}
	s.Store = d
	if w := call(s, "GET", "/og/"+g.ID+".png", ""); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if len(d.at) < 3 { // the PNG lookup, the glyph, and the PNG write
		t.Fatalf("%d storage calls", len(d.at))
	}
	// Every call shares the request's one deadline, so time spent rendering
	// or on earlier calls comes out of what later calls get.
	for i, at := range d.at {
		if at.IsZero() || !at.Equal(d.at[0]) {
			t.Errorf("storage call %d has deadline %v, want the request's %v", i, at, d.at[0])
		}
	}
	if left := time.Until(d.at[0]); left <= 0 || left > 10*time.Second {
		t.Errorf("request budget %v, want within the 10s Lambda timeout", left)
	}
}

// Test ID: API-19
// Every response the routes give, success or problem, matches the contract in
// api/openapi.yaml: its status, content type and body schema.
func TestResponsesMatchOpenAPI(t *testing.T) {
	spec, e := os.ReadFile("../../api/openapi.yaml")
	if e != nil {
		t.Fatal(e)
	}
	doc, e := libopenapi.NewDocument(spec)
	if e != nil {
		t.Fatal(e)
	}
	v, errs := validator.NewValidator(doc)
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	s := setup(t)
	g := savedGlyph(t, s)
	missing := strings.Repeat("a", 32)
	for _, c := range []struct {
		method, path, body string
		status             int
	}{
		{"POST", "/api/v1/glyphs", `{"mode":"kun","reading":"あお","okurigana":"い"}`, 200},
		{"POST", "/api/v1/glyphs", `{"mode":"on","reading":"漢"}`, 422},
		{"POST", "/api/v1/glyphs", `{"mode":"on","reading":"ア","reading":"イ"}`, 400},
		{"POST", "/api/v1/glyphs", strings.Repeat("x", 1025), 413},
		{"GET", "/api/v1/glyphs/" + g.ID, "", 200},
		{"GET", "/api/v1/glyphs/no", "", 400},
		{"GET", "/api/v1/glyphs/" + missing, "", 404},
		{"GET", "/g/" + g.ID, "", 200},
		{"GET", "/g/" + missing, "", 404},
		{"GET", "/og/" + g.ID + ".png", "", 200},
		{"GET", "/og/" + missing + ".png", "", 404},
	} {
		w := call(s, c.method, c.path, c.body)
		if w.Code != c.status {
			t.Errorf("%s %s: status %d, want %d", c.method, c.path, w.Code, c.status)
			continue
		}
		r := httptest.NewRequest(c.method, c.path, strings.NewReader(c.body))
		r.Header.Set("Content-Type", "application/json")
		matches(t, v, r, w)
	}
	// The remaining POST problems: wrong media type, rate limit, paused.
	r := httptest.NewRequest("POST", "/api/v1/glyphs", strings.NewReader(`{}`))
	r.Header.Set("Content-Type", "text/plain")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 415 {
		t.Errorf("text/plain: %d", w.Code)
	}
	matches(t, v, r, w)
	s.GenerationEnabled = false
	if w := call(s, "POST", "/api/v1/glyphs", `{"mode":"on","reading":"セイ"}`); w.Code != 503 {
		t.Errorf("paused: %d", w.Code)
	} else {
		matches(t, v, httptest.NewRequest("POST", "/api/v1/glyphs", nil), w)
	}
	s.GenerationEnabled = true
	s.ips["192.0.2.1"] = bucket{tokens: 0, seen: s.Now()}
	if w := call(s, "POST", "/api/v1/glyphs", `{"mode":"on","reading":"セイ"}`); w.Code != 429 {
		t.Errorf("rate limit: %d", w.Code)
	} else {
		matches(t, v, httptest.NewRequest("POST", "/api/v1/glyphs", nil), w)
	}
}

func matches(t *testing.T, v validator.Validator, r *http.Request, w *httptest.ResponseRecorder) {
	t.Helper()
	if ok, errs := v.ValidateHttpResponse(r, w.Result()); !ok {
		for _, err := range errs {
			t.Errorf("%s %s %d: %s %s", r.Method, r.URL.Path, w.Code, err.Message, err.Reason)
			for _, se := range err.SchemaValidationErrors {
				t.Errorf("  %s", se.Error())
			}
		}
	}
}
