package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"kanji-gacha/internal/compose"
	"kanji-gacha/internal/glyphid"
	"kanji-gacha/internal/kana"
	"kanji-gacha/internal/metrics"
	"kanji-gacha/internal/model"
	"kanji-gacha/internal/render"
	"kanji-gacha/internal/store"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type bucket struct {
	tokens float64
	seen   time.Time
}
type Server struct {
	Engine            *compose.Engine
	Store             store.Store
	BaseURL           string
	WebDir            string
	GenerationEnabled bool
	TrustCloudFront   bool
	Metrics           *metrics.EMF
	Now               func() time.Time
	mu                sync.Mutex
	ips               map[string]bucket
	index             []byte
}

func New(engine *compose.Engine, st store.Store, base, web string) (*Server, error) {
	u, e := url.Parse(base)
	if e != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.RawQuery != "" || u.Fragment != "" {
		return nil, fmt.Errorf("BASE_URL must be an absolute http(s) URL")
	}
	s := &Server{Engine: engine, Store: st, BaseURL: strings.TrimRight(base, "/"), WebDir: web, GenerationEnabled: true, Now: time.Now, ips: map[string]bucket{}}
	s.index, e = os.ReadFile(filepath.Join(web, "index.html"))
	if e != nil {
		return nil, fmt.Errorf("build the frontend first: %w", e)
	}
	return s, nil
}
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })
	mux.HandleFunc("POST /api/v1/glyphs", s.generate)
	mux.HandleFunc("GET /api/v1/glyphs/{id}", s.get)
	mux.HandleFunc("GET /g/{id}", s.share)
	// A share path of any other shape, such as one given a trailing slash, is
	// still a link someone opened; the SPA tells them to check it.
	mux.HandleFunc("GET /g/{rest...}", func(w http.ResponseWriter, r *http.Request) { s.shareError(w, 404) })
	mux.HandleFunc("GET /og/{image}", s.og)
	mux.Handle("GET /assets/", http.FileServer(http.Dir(s.WebDir)))
	mux.HandleFunc("GET /kanji-ui.otf", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=86400")
		http.ServeFile(w, r, filepath.Join(s.WebDir, "kanji-ui.otf"))
	})
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			problem(w, 404, "not_found", nil)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		w.Write(s.index)
	})
	// These stop the default mux from answering a wrong method with a
	// text/plain 405 that carries no Cache-Control. A path some other method
	// routes is a wrong method; anything else is a missing resource.
	fallback := func(w http.ResponseWriter, r *http.Request) {
		if allow := routedMethods(mux, r); len(allow) > 0 {
			w.Header().Set("Allow", strings.Join(allow, ", "))
			problem(w, 405, "method_not_allowed", nil)
			return
		}
		problem(w, 404, "not_found", nil)
	}
	mux.HandleFunc("/api/", fallback)
	mux.HandleFunc("/", fallback)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		mux.ServeHTTP(w, r)
	})
}

// fallbackPatterns are the two catch-alls; a path that only they match has no
// route of its own.
var fallbackPatterns = map[string]bool{"/": true, "/api/": true}

// routedMethods reports which methods reach a real route for this path, so a
// wrong-method response can name them and a path with no route stays a 404.
// Asking the mux keeps this from drifting as routes change.
func routedMethods(mux *http.ServeMux, r *http.Request) []string {
	var out []string
	for _, m := range []string{http.MethodGet, http.MethodHead, http.MethodPost} {
		probe := r.Clone(r.Context())
		probe.Method = m
		if _, pattern := mux.Handler(probe); pattern != "" && !fallbackPatterns[pattern] {
			out = append(out, m)
		}
	}
	return out
}
func problem(w http.ResponseWriter, status int, code string, issue *kana.Issue) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	v := map[string]any{"type": "about:blank", "title": http.StatusText(status), "status": status, "code": code}
	if issue != nil {
		v["errors"] = []*kana.Issue{issue}
	}
	json.NewEncoder(w).Encode(v)
}

// viewerIP reads the address out of a CloudFront-Viewer-Address value, which
// carries a port: "198.51.100.7:52011" or "[2001:db8::1]:52011". An unbracketed
// IPv6 value is handled too, since the exact shape is not guaranteed. A value
// this cannot resolve is dropped, leaving the shared RemoteAddr bucket, which
// is restrictive rather than exploitable.
func viewerIP(v string) string {
	if v == "" {
		return ""
	}
	if !strings.Contains(v, ":") { // an address carrying no port
		if net.ParseIP(v) != nil {
			return v
		}
		return ""
	}
	if host, _, e := net.SplitHostPort(v); e == nil {
		if net.ParseIP(host) != nil {
			return host
		}
		return ""
	}
	// Unbracketed IPv6 with a port. Never take the whole value as the address:
	// a port that is also a valid hex group ("2001:db8::1:443") parses, and
	// keying on it would hand one viewer a fresh bucket per connection.
	if i := strings.LastIndex(v, ":"); i > 0 {
		if host := v[:i]; net.ParseIP(host) != nil {
			return host
		}
	}
	return ""
}
func (s *Server) allow(r *http.Request) bool {
	ip, _, e := net.SplitHostPort(r.RemoteAddr)
	if e != nil {
		ip = r.RemoteAddr
	}
	if s.TrustCloudFront {
		// CloudFront-Viewer-Address is added by CloudFront itself and cannot be
		// set by the viewer, unlike X-Forwarded-For whose entries depend on what
		// the Function URL appends.
		if v := viewerIP(r.Header.Get("CloudFront-Viewer-Address")); v != "" {
			ip = v
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.Now()
	b, ok := s.ips[ip]
	if !ok {
		b = bucket{tokens: 10, seen: now}
	}
	b.tokens = min(10, b.tokens+now.Sub(b.seen).Seconds())
	b.seen = now
	allowed := b.tokens >= 1
	if allowed {
		b.tokens--
	}
	if len(s.ips) > 4096 {
		for k, v := range s.ips {
			if now.Sub(v.seen) > time.Minute {
				delete(s.ips, k)
			}
		}
		if len(s.ips) > 8192 {
			return false
		}
	}
	s.ips[ip] = b
	return allowed
}
func (s *Server) generate(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	ct, _, e := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if e != nil || ct != "application/json" {
		problem(w, 415, "content_type", nil)
		return
	}
	body, e := io.ReadAll(http.MaxBytesReader(w, r.Body, 1024))
	if e != nil {
		var large *http.MaxBytesError
		if errors.As(e, &large) {
			problem(w, 413, "body_limit", nil)
		} else {
			problem(w, 400, "body", nil)
		}
		return
	}
	in, status, e := kana.Decode(body)
	if e != nil {
		var issue *kana.Issue
		errors.As(e, &issue)
		problem(w, status, "invalid_input", issue)
		return
	}
	reading, issue := kana.Validate(in)
	if issue != nil {
		problem(w, 422, "invalid_input", issue)
		return
	}
	if !s.GenerationEnabled {
		problem(w, 503, "generation_paused", nil)
		return
	}
	if !s.allow(r) {
		w.Header().Set("Retry-After", "1")
		problem(w, 429, "rate_limit", nil)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	// The draw is counted once the generator returns, whether or not saving it
	// succeeds; a failed save is a storage problem, not a different draw.
	start := time.Now()
	g, e := s.Engine.Generate(ctx, reading, s.Now())
	if e != nil {
		s.Metrics.GenerationFailed()
		problem(w, 503, "generation_failed", nil)
		return
	}
	s.Metrics.Generated(time.Since(start), g.Rarity, g.Fallback)
	// Moderation affects shared titles only; original input and generated shape are preserved.
	g.OGPReading = sharedReading(g.Reading.Display)
	for i := 0; i < 3; i++ {
		id, e := glyphid.New()
		if e != nil {
			problem(w, 503, "id_failed", nil)
			return
		}
		g.ID = id
		g.OGPKey = "og/" + id + ".png"
		b, e := json.Marshal(g)
		if e != nil {
			problem(w, 503, "serialization_failed", nil)
			return
		}
		e = s.Store.PutIfAbsent(ctx, "glyphs/"+id+".json", b, "application/json")
		if errors.Is(e, store.ErrExists) {
			continue
		}
		if e != nil {
			problem(w, 503, "save_failed", nil)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(b)
		return
	}
	problem(w, 503, "id_collision", nil)
}
func sharedReading(s string) string {
	n := kana.Normalize(s)
	for _, word := range []string{"シネ", "コロス"} {
		if strings.Contains(n, word) {
			return "生成結果"
		}
	}
	return s
}

// load reads a saved glyph. On failure it returns the status and problem code
// to report, leaving the response to the caller: the API answers with
// problem+json, while the share page answers with the SPA.
func (s *Server) load(ctx context.Context, id string) (model.Glyph, []byte, int, string) {
	var g model.Glyph
	if !glyphid.Valid(id) {
		return g, nil, 400, "invalid_id"
	}
	b, e := s.Store.Get(ctx, "glyphs/"+id+".json")
	if e != nil {
		if errors.Is(e, store.ErrNotFound) {
			return g, nil, 404, "not_found"
		}
		return g, nil, 503, "storage_unavailable"
	}
	if json.Unmarshal(b, &g) != nil || g.ID != id || g.SchemaVersion != 1 {
		return g, nil, 503, "invalid_record"
	}
	return g, b, 0, ""
}
func (s *Server) get(w http.ResponseWriter, r *http.Request) {
	_, b, status, code := s.load(r.Context(), r.PathValue("id"))
	if status != 0 {
		problem(w, status, code, nil)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Write(b)
}
func (s *Server) share(w http.ResponseWriter, r *http.Request) {
	g, _, status, _ := s.load(r.Context(), r.PathValue("id"))
	if status != 0 {
		s.shareError(w, status)
		return
	}
	escape := html.EscapeString
	title := g.OGPReading + " | 架空漢字ガチャ"
	link := s.BaseURL + "/g/" + g.ID
	image := s.BaseURL + "/og/" + g.ID + ".png"
	tags := fmt.Sprintf(`<meta property="og:type" content="website"><meta property="og:title" content="%s"><meta property="og:description" content="読みを入力して字形を生成するアプリです。"><meta property="og:url" content="%s"><meta property="og:image" content="%s"><meta property="og:image:width" content="1200"><meta property="og:image:height" content="630"><meta name="twitter:card" content="summary_large_image">`, escape(title), escape(link), escape(image))
	page := strings.Replace(string(s.index), "<!-- og-tags -->", tags, 1)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Write([]byte(page))
}

// shareError answers a share link that cannot be served with the SPA, keeping
// the status. People open these links in a browser, so problem+json would be
// shown to them as raw text; the SPA instead reads the path, asks the API for
// the glyph and shows the error in its own words. The page carries no OGP tags
// and is not cached, so a link that starts working is not held back.
func (s *Server) shareError(w http.ResponseWriter, status int) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	w.Write(s.index)
}

// rereadAfterConflict fetches the object whose conditional PUT won the race.
// That write can take a moment to become readable, so a missing object is
// retried; anything else is reported. The locally rendered bytes are never
// substituted: a rolling deployment can have instances on different renderer or
// font versions, and serving a variant of the stored object under an immutable
// cache policy would pin the difference for a year.
func (s *Server) rereadAfterConflict(ctx context.Context, key string) ([]byte, error) {
	for delay := 25 * time.Millisecond; ; delay *= 2 {
		b, e := s.Store.Get(ctx, key)
		if e == nil {
			return b, nil
		}
		if !errors.Is(e, store.ErrNotFound) || delay > 200*time.Millisecond {
			return nil, e
		}
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}
func (s *Server) og(w http.ResponseWriter, r *http.Request) {
	image := r.PathValue("image")
	if !strings.HasSuffix(image, ".png") {
		problem(w, 400, "invalid_id", nil)
		return
	}
	id := strings.TrimSuffix(image, ".png")
	if !glyphid.Valid(id) {
		problem(w, 400, "invalid_id", nil)
		return
	}
	// The request budget covers the S3 round trips; only the rendering itself is
	// held to the 3s limit.
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	r = r.WithContext(ctx)
	key := "og/" + id + ".png"
	b, e := s.Store.Get(ctx, key)
	if errors.Is(e, store.ErrNotFound) {
		g, _, status, code := s.load(ctx, id)
		if status != 0 {
			problem(w, status, code, nil)
			return
		}
		rctx, rcancel := context.WithTimeout(ctx, 3*time.Second)
		b, e = render.PNG(rctx, g)
		rcancel()
		if e == nil {
			e = s.Store.PutIfAbsent(ctx, key, b, "image/png")
			if errors.Is(e, store.ErrExists) {
				b, e = s.rereadAfterConflict(ctx, key)
			}
		}
	}
	if e != nil {
		problem(w, 503, "image_unavailable", nil)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Write(b)
}
