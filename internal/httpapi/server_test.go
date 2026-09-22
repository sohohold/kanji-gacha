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
