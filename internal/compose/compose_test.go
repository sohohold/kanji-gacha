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
	groups := e.plans(r, []int{3})[3]
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

// A frame must occupy a slot of its own: before this, it overwrote parts[0] and
// the reading part chosen for the first segment vanished from the glyph.
func TestFrameKeepsReadingParts(t *testing.T) {
	e := testEngine(t)
	if len(e.frames) == 0 {
		t.Skip("no frame parts in the bundled dictionary")
	}
	r, _ := kana.Validate(kana.Input{Mode: "on", Reading: "セイ"})
	rng := rand.New(rand.NewChaCha8([32]byte{3}))
	for _, count := range []int{3, 4, 5} {
		groups := e.plans(r, []int{slotKey(count, true)})[slotKey(count, true)]
		for i := 0; i < 20; i++ {
			res, err := e.attempt(groups, count, 4, 0.5, rng, true)
			if err != nil {
				continue
			}
			if len(res.components) != count {
				t.Fatalf("count=%d: got %d components", count, len(res.components))
			}
			readings := 0
			for _, c := range res.components {
				if c.Role == "reading" {
					readings++
				}
			}
			if readings < len(groups) {
				t.Fatalf("count=%d: %d reading components for %d segments", count, readings, len(groups))
			}
		}
	}
}

// The number of segments must never exceed the reading slots a candidate has,
// or the tail of the reading never reaches the glyph (spec 3.3).
func TestSegmentsFitAvailableSlots(t *testing.T) {
	e := testEngine(t)
	r, _ := kana.Validate(kana.Input{Mode: "on", Reading: "トウキョウ"})
	for _, rarity := range e.Config.Rarities {
		slots := []int{slotKey(1, false)}
		for n := rarity.MinParts; n <= rarity.MaxParts; n++ {
			slots = append(slots, slotKey(n, false), slotKey(n, true))
		}
		plans := e.plans(r, slots)
		for n := rarity.MinParts; n <= rarity.MaxParts; n++ {
			for _, frame := range []bool{false, true} {
				if frame && n < 3 { // Generate only frames candidates of three parts or more.
					continue
				}
				groups, ok := plans[slotKey(n, frame)]
				if !ok {
					t.Fatalf("%s n=%d frame=%v: no plan", rarity.Name, n, frame)
				}
				available := n
				if frame {
					available = n - 1
				}
				if len(groups) > available {
					t.Fatalf("%s n=%d frame=%v: %d segments for %d slots", rarity.Name, n, frame, len(groups), available)
				}
			}
		}
	}
	// The single-part fallback ranks the whole reading, not just its head.
	if got := len(e.plans(r, []int{1})[1]); got != 1 {
		t.Fatalf("fallback plan has %d segments", got)
	}
}
