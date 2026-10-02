package compose

import (
	"bytes"
	"context"
	"kanji-gacha/config"
	"kanji-gacha/internal/dict"
	"kanji-gacha/internal/kage"
	"kanji-gacha/internal/kana"
	"kanji-gacha/internal/model"
	"log"
	"math"
	"math/rand/v2"
	"os"
	"strings"
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

// Test ID: GEN-01
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
			if res.strokes > e.Config.MaxStrokes {
				t.Fatal("stroke limit")
			}
			// The engine floors each part's rectangle to whole KAGE units, so a
			// part may shift by up to one unit.
			b, ok := kage.Extent(kage.Expand(e.glyph(res.parts), e.Dict.Lookup))
			m := e.Config.Margin * kageSize / e.Config.Canvas
			finite := !math.IsNaN(b.MinX+b.MinY+b.MaxX+b.MaxY) && !math.IsInf(b.MinX+b.MinY+b.MaxX+b.MaxY, 0)
			if !ok || !finite || b.MinX < m-1.01 || b.MinY < m-1.01 || b.MaxX > kageSize-m+1.01 || b.MaxY > kageSize-m+1.01 {
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

// Test ID: GEN-02
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

// Test ID: GEN-03
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

// Test ID: GEN-04
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

// Test ID: GEN-05
// The fallback must stay within the configured limits: a one-part rarity gets a
// bare phonetic, and a stroke limit the radical would break drops the radical.
func TestFallbackRespectsLimits(t *testing.T) {
	r, _ := kana.Validate(kana.Input{Mode: "on", Reading: "セイ"})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, tc := range []struct {
		name  string
		tweak func(*Engine)
	}{
		{"one part", func(e *Engine) {
			for i := range e.Config.Rarities {
				e.Config.Rarities[i].MinParts, e.Config.Rarities[i].MaxParts = 1, 1
			}
		}},
		{"tight strokes", func(e *Engine) { e.Config.MaxStrokes = 12 }},
	} {
		e := testEngine(t)
		tc.tweak(e)
		for i := 0; i < 20; i++ {
			g, err := e.Generate(ctx, r, time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC))
			if err != nil {
				t.Fatalf("%s: %v", tc.name, err)
			}
			if g.Legend != nil {
				continue
			}
			if g.Strokes > e.Config.MaxStrokes {
				t.Fatalf("%s: %d strokes over the limit", tc.name, g.Strokes)
			}
			if tc.name == "one part" && len(g.Components) != 1 {
				t.Fatalf("%s: %d components", tc.name, len(g.Components))
			}
		}
	}
}

// buildable draws radicals until attempt succeeds, since a single draw can
// fail on an unlucky radical, and returns what it built. A count that never
// builds is a failure, not a skipped case.
func buildable(e *Engine, ph candidate, n, depth int, rng *rand.Rand, cores int) ([]model.Component, bool) {
	for range 100 {
		if res, err := e.attempt(ph, n, depth, rng, cores); err == nil {
			return res.components, true
		}
	}
	return nil, false
}

// Test ID: GEN-06
// Every part count a rarity may draw must be reachable within its depth limit
// for every core size coreSize can pick, or the advertised range is skewed.
func TestRarityPartCountsReachable(t *testing.T) {
	e := testEngine(t)
	r, _ := kana.Validate(kana.Input{Mode: "on", Reading: "セイ"})
	ph := e.rank(r.Morae, r.Mode, r.Okurigana)[0]
	rng := rand.New(rand.NewChaCha8([32]byte{4}))
	cores := map[string][]int{"SSR": {1, 2, 3}, "UR": {2, 3, 4}}
	for _, rarity := range e.Config.Rarities {
		cs, ok := cores[rarity.Name]
		if !ok {
			cs = []int{1}
		}
		for n := rarity.MinParts; n <= rarity.MaxParts; n++ {
			for _, c := range cs {
				got, ok := buildable(e, ph, n, rarity.MaxDepth, rng, c)
				if !ok {
					t.Errorf("%s n=%d cores=%d: never built", rarity.Name, n, c)
					continue
				}
				if len(got) != n {
					t.Errorf("%s n=%d cores=%d: got %d components", rarity.Name, n, c, len(got))
				}
				// A repeated phonetic is the same character every time. When the
				// depth leaves too few radical slots, it repeats up to 4 times.
				readings := 0
				for _, x := range got {
					if x.Role == "reading" {
						readings++
						if x.Char != ph.part.Char {
							t.Errorf("%s n=%d cores=%d: phonetic %s, want %s", rarity.Name, n, c, x.Char, ph.part.Char)
						}
					}
				}
				if readings < c || readings > 4 {
					t.Errorf("%s n=%d cores=%d: %d phonetics", rarity.Name, n, c, readings)
				}
			}
		}
	}
}

// Test ID: GEN-07
// config.MaxReachableParts must match what attempt can actually build, so a
// configuration that passes validation never truncates its part counts.
func TestMaxReachablePartsMatchesGenerator(t *testing.T) {
	e := testEngine(t)
	r, _ := kana.Validate(kana.Input{Mode: "on", Reading: "セイ"})
	ph := e.rank(r.Morae, r.Mode, r.Okurigana)[0]
	e.Config.MaxStrokes = 512
	rng := rand.New(rand.NewChaCha8([32]byte{5}))
	for depth := 1; depth <= 10; depth++ {
		n := config.MaxReachableParts(depth)
		if want := max(2, depth+2); depth == 1 && n != 2 || depth > 1 && n != want {
			t.Errorf("MaxReachableParts(%d) = %d, want %d", depth, n, want)
		}
		for _, cores := range []int{1, 2, 3, 4} {
			got, ok := buildable(e, ph, n, depth, rng, cores)
			if !ok {
				t.Errorf("depth=%d cores=%d: %d parts never built", depth, cores, n)
			} else if len(got) != n {
				t.Errorf("depth=%d cores=%d: got %d of %d parts", depth, cores, len(got), n)
			}
		}
		// One more part than the limit must not be buildable.
		if res, err := e.attempt(ph, n+1, depth, rng, 4); err == nil && len(res.components) > n {
			t.Errorf("depth=%d: built %d parts beyond the limit %d", depth, len(res.components), n)
		}
	}
}

// Test ID: GEN-08
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

// Test ID: GEN-09
// A special glyph whose parts are missing falls back to a normal draw, and
// leaves a log line saying so rather than passing silently.
func TestMissingSpecialIsLogged(t *testing.T) {
	e := testEngine(t)
	delete(e.whole, "龍")
	var buf bytes.Buffer
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	r, _ := kana.Validate(kana.Input{Mode: "kun", Reading: "タイト"})
	g, err := e.Generate(context.Background(), r, time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if g.Legend != nil {
		t.Fatalf("drew legend %s without its parts", *g.Legend)
	}
	if !strings.Contains(buf.String(), "special glyph taito could not be drawn") {
		t.Fatalf("no log line: %q", buf.String())
	}
}
