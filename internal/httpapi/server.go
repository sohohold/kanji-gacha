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
	mux.HandleFunc("GET /og/{image}", s.og)
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) { problem(w, 404, "not_found", nil) })
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
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		mux.ServeHTTP(w, r)
	})
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
func (s *Server) allow(r *http.Request) bool {
	ip, _, e := net.SplitHostPort(r.RemoteAddr)
	if e != nil {
		ip = r.RemoteAddr
	}
	if s.TrustCloudFront { // CloudFront appends the viewer address; do not trust client-supplied leading entries.
		xs := strings.Split(r.Header.Get("X-Forwarded-For"), ",")
		if len(xs) > 0 {
			v := strings.TrimSpace(xs[len(xs)-1])
			if net.ParseIP(v) != nil {
				ip = v
			}
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
	g, e := s.Engine.Generate(ctx, reading, s.Now())
	if e != nil {
		problem(w, 503, "generation_failed", nil)
		return
	}
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
func (s *Server) load(w http.ResponseWriter, r *http.Request, id string) (model.Glyph, []byte, bool) {
	var g model.Glyph
	if !glyphid.Valid(id) {
		problem(w, 400, "invalid_id", nil)
		return g, nil, false
	}
	b, e := s.Store.Get(r.Context(), "glyphs/"+id+".json")
	if e != nil {
		if errors.Is(e, store.ErrNotFound) {
			problem(w, 404, "not_found", nil)
		} else {
			problem(w, 503, "storage_unavailable", nil)
		}
		return g, nil, false
	}
	if json.Unmarshal(b, &g) != nil || g.ID != id || g.SchemaVersion != 1 {
		problem(w, 503, "invalid_record", nil)
		return g, nil, false
	}
	return g, b, true
}
func (s *Server) get(w http.ResponseWriter, r *http.Request) {
	_, b, ok := s.load(w, r, r.PathValue("id"))
	if !ok {
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Write(b)
}
func (s *Server) share(w http.ResponseWriter, r *http.Request) {
	g, _, ok := s.load(w, r, r.PathValue("id"))
	if !ok {
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
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	r = r.WithContext(ctx)
	key := "og/" + id + ".png"
	b, e := s.Store.Get(ctx, key)
	if errors.Is(e, store.ErrNotFound) {
		g, _, ok := s.load(w, r, id)
		if !ok {
			return
		}
		b, e = render.PNG(ctx, g)
		if e == nil {
			e = s.Store.PutIfAbsent(ctx, key, b, "image/png")
			if errors.Is(e, store.ErrExists) {
				b, e = s.Store.Get(ctx, key)
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
