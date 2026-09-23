package compose

import (
	"context"
	"kanji-gacha/config"
	"kanji-gacha/internal/dict"
	"kanji-gacha/internal/kana"
	"kanji-gacha/internal/model"
	"kanji-gacha/internal/render"
	"math"
	"math/rand/v2"
	"testing"
	"time"
)

func testEngine(t *testing.T) *Engine {
	t.Helper()
	c, e := config.Load("")
	if e != nil {
		t.Fatal(e)
	}
	d, e := dict.Load()
	if e != nil {
		t.Fatal(e)
	}
	return New(c, d)
}
func TestGenerationLimits(t *testing.T) {
	e := testEngine(t)
	r, _ := kana.Validate(kana.Input{Mode: "on", Reading: "セイ"})
	ph := e.rank(r.Morae, r.Mode, r.Okurigana)[0]
	rng := rand.New(rand.NewChaCha8([32]byte{1}))
	for _, rarity := range e.Config.Rarities {
		for i := 0; i < 20; i++ {
			res, err := e.attempt(ph, rarity.MaxParts, rarity.MaxDepth, rng, coreSize(rarity, rng))
			if err != nil {
				continue
			}
			if len(res.paths) > e.Config.MaxStrokes {
				t.Fatal("stroke limit")
			}
			b := render.Bounds(res.paths)
			if b.X < e.Config.Margin-0.001 || b.Y < e.Config.Margin-0.001 || b.X+b.W > 109-e.Config.Margin+.001 || b.Y+b.H > 109-e.Config.Margin+.001 {
				t.Fatalf("out of canvas: %+v", b)
			}
			var check func(*model.Node, int)
			check = func(n *model.Node, d int) {
				if d > rarity.MaxDepth {
					t.Fatalf("%s: depth %d", rarity.Name, d)
				}
				if n.Operator != "" && len(n.Children) != 2 {
					t.Fatal("arity")
				}
				for _, c := range n.Children {
					check(c, d+1)
				}
			}
			check(res.tree, 0)
		}
	}
	for _, in := range []kana.Input{{Mode: "on", Reading: "セイ"}, {Mode: "kun", Reading: "トウキョウ"}, {Mode: "kun", Reading: "タイト"}, {Mode: "kun", Reading: "きも", Okurigana: "い"}} {
		r, _ := kana.Validate(in)
		g, err := e.Generate(context.Background(), r, time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC))
		if err != nil {
			t.Fatal(err)
		}
		if g.SVG == "" || g.IDS == "" || g.Strokes <= 0 {
			t.Fatalf("empty result %s", in.Reading)
		}
	}
}
func TestRarityDistribution(t *testing.T) {
	e := testEngine(t)
	rng := rand.New(rand.NewChaCha8([32]byte{2}))
	counts := map[string]int{}
	for i := 0; i < 100000; i++ {
		counts[e.Rarity(rng).Name]++
	}
	stat := 0.0
	for _, r := range e.Config.Rarities {
		expected := float64(r.Weight) * 10
		stat += math.Pow(float64(counts[r.Name])-expected, 2) / expected
	}
	if stat > 18.467 {
		t.Fatalf("chi-square %.2f", stat)
	}
}

// Every glyph keeps exactly one reading character (the phonetic, possibly
// repeated) and wraps it in radicals chosen independently of the reading.
func TestPhoneticWithRadicals(t *testing.T) {
	e := testEngine(t)
	r, _ := kana.Validate(kana.Input{Mode: "kun", Reading: "キモ", Okurigana: "イ"})
	ph := e.rank(r.Morae, r.Mode, r.Okurigana)[0]
	rng := rand.New(rand.NewChaCha8([32]byte{3}))
	seen := map[string]bool{}
	for i := 0; i < 300; i++ {
		res, err := e.attempt(ph, 2, 1, rng, 1)
		if err != nil {
			t.Fatal(err)
		}
		readings := 0
		for _, c := range res.components {
			if c.Role == "reading" {
				readings++
				if c.Char != ph.part.Char {
					t.Fatalf("reading component %s, want %s", c.Char, ph.part.Char)
				}
			} else {
				seen[c.Char] = true
			}
		}
		if readings != 1 || len(res.components) != 2 {
			t.Fatalf("components %+v", res.components)
		}
	}
	// 音を持たない偏・部首が実際に出ること。
	for _, c := range []string{"氵", "亻", "扌", "艹"} {
		if !seen[c] {
			t.Errorf("radical %s never drawn in 300 attempts (%d kinds seen)", c, len(seen))
		}
	}
	if len(seen) < 40 {
		t.Errorf("only %d radicals seen", len(seen))
	}
}

// A generation that times out before any worker reports must still return a
// phonetic with a radical, not a bare existing character.
func TestFallbackHasRadical(t *testing.T) {
	e := testEngine(t)
	r, _ := kana.Validate(kana.Input{Mode: "on", Reading: "セイ"})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for i := 0; i < 20; i++ {
		g, err := e.Generate(ctx, r, time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC))
		if err != nil {
			t.Fatal(err)
		}
		if g.Legend != nil {
			continue
		}
		if !g.Fallback {
			t.Fatal("expected the fallback candidate with a cancelled context")
		}
		if len(g.Components) != 2 || g.Tree.Operator == "" {
			t.Fatalf("fallback without radical: %s %+v", g.IDS, g.Components)
		}
	}
}

func TestRadicalFramesHaveInnerBoxes(t *testing.T) {
	e := testEngine(t)
	frames := 0
	for _, r := range e.rads {
		switch r.position {
		case "tare", "nyo", "kamae":
			frames++
			if _, ok := r.frame(r.variants[0]); !ok {
				t.Errorf("%s has no inner box", r.char)
			}
		}
	}
	if frames == 0 {
		t.Fatal("no frame radicals")
	}
}
