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
	groups := e.segment(r, 3)
	rng := rand.New(rand.NewChaCha8([32]byte{1}))
	for _, rarity := range e.Config.Rarities {
		for i := 0; i < 5; i++ {
			res, err := e.attempt(groups, rarity.MaxParts, rarity.MaxDepth, rarity.Temperature, rng, false)
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
				if d > e.Config.MaxDepth {
					t.Fatal("depth")
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
	for _, read := range []string{"セイ", "トウキョウ", "タイト", "ビャン"} {
		r, _ := kana.Validate(kana.Input{Mode: "on", Reading: read})
		g, err := e.Generate(context.Background(), r, time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC))
		if err != nil {
			t.Fatal(err)
		}
		if g.SVG == "" || g.IDS == "" || g.Strokes <= 0 {
			t.Fatalf("empty result %s", read)
		}
		if read == "ビャン" && (g.Legend == nil || *g.Legend != "biang") {
			t.Fatal("biang")
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
