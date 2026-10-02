package compose

import (
	"bytes"
	"context"
	"kanji-gacha/config"
	"kanji-gacha/internal/dict"
	"kanji-gacha/internal/kage"
	"kanji-gacha/internal/kana"
	"kanji-gacha/internal/model"
	"kanji-gacha/internal/vector"
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

// Test ID: GEN-10
// Each term of the phonetic score moves a candidate by its configured amount:
// α for holding the reading, β for a matching okurigana, γ per unit of
// frequency, and 0.3 for having a 旁 or 脚 form. The candidates share one
// sound vector, so only those terms separate them.
func TestRankScoreTerms(t *testing.T) {
	c, err := config.Load("")
	if err != nil {
		t.Fatal(err)
	}
	sound := vector.Reading("セイ")
	whole := func(char string, on, kun []dict.Reading, freq float64) dict.Part {
		return dict.Part{ID: char, Glyph: char, Char: char, Position: "whole", Strokes: 5, Bounds: model.Box{W: 100, H: 100},
			On: on, Kun: kun, OnVector: sound, KunVector: sound, Frequency: freq}
	}
	d := dict.Dictionary{Parts: []dict.Part{
		whole("甲", []dict.Reading{{Text: "セイ"}}, []dict.Reading{{Text: "セイ", Okurigana: "イ"}}, 0),
		whole("乙", []dict.Reading{{Text: "ゼイ"}}, []dict.Reading{{Text: "セイ"}}, 0),
		whole("丙", []dict.Reading{{Text: "セイ"}}, nil, 0.5),
		whole("丁", []dict.Reading{{Text: "ゼイ"}}, nil, 0),
		{ID: "丁-02", Glyph: "丁-02", Char: "丁", Position: "right", Strokes: 2, Bounds: model.Box{W: 50, H: 100}},
	}}
	e := New(c, d)
	score := func(mode, okuri string) map[string]float64 {
		r, _ := kana.Validate(kana.Input{Mode: mode, Reading: "セイ", Okurigana: okuri})
		out := map[string]float64{}
		for _, cand := range e.rank(r.Morae, r.Mode, r.Okurigana) {
			out[cand.part.Char] = cand.score
		}
		return out
	}
	near := func(got, want float64) bool { return math.Abs(got-want) < 1e-6 }
	on := score("on", "")
	if !near(on["甲"]-on["乙"], c.Alpha) {
		t.Errorf("reading match adds %g, want α=%g", on["甲"]-on["乙"], c.Alpha)
	}
	if !near(on["甲"]-on["丙"], c.Gamma*0.5) {
		t.Errorf("frequency 0.5 costs %g, want γ/2=%g", on["甲"]-on["丙"], c.Gamma*0.5)
	}
	if !near(on["丁"]-on["乙"], 0.3) {
		t.Errorf("a 旁 form adds %g, want δ=0.3", on["丁"]-on["乙"])
	}
	kun := score("kun", "い")
	if !near(kun["甲"]-kun["乙"], c.Beta) {
		t.Errorf("matching okurigana adds %g, want β=%g", kun["甲"]-kun["乙"], c.Beta)
	}
	if got := e.rank([]string{"セ", "イ"}, "on", ""); got[0].part.Char != "甲" {
		t.Errorf("top candidate %s, want 甲", got[0].part.Char)
	}
	// Only the top k candidates go on to the draw.
	e.Config.TopK = 2
	got := e.rank([]string{"セ", "イ"}, "on", "")
	if len(got) != 2 || got[0].part.Char != "甲" || got[1].part.Char != "丙" {
		t.Errorf("top 2: %v", got)
	}
}

// Test ID: GEN-11
// The phonetic is drawn by softmax over the scores at temperature T, and the
// configured temperature rises with rarity so rarer draws stray further from
// the reading.
func TestChooseFollowsSoftmax(t *testing.T) {
	cs := []candidate{{&dict.Part{Char: "a"}, 0}, {&dict.Part{Char: "b"}, -0.3}, {&dict.Part{Char: "c"}, -0.6}}
	rng := rand.New(rand.NewChaCha8([32]byte{6}))
	for _, temp := range []float64{0.3, 1.5} {
		const n = 100000
		counts := map[string]float64{}
		for range n {
			counts[choose(cs, temp, rng).part.Char]++
		}
		sum := 0.0
		for _, c := range cs {
			sum += math.Exp(c.score / temp)
		}
		stat := 0.0
		for _, c := range cs {
			want := n * math.Exp(c.score/temp) / sum
			stat += math.Pow(counts[c.part.Char]-want, 2) / want
		}
		if stat > 13.816 { // χ² with 2 degrees of freedom at 0.001
			t.Errorf("T=%g: chi-square %.2f for %v", temp, stat, counts)
		}
	}
	e := testEngine(t)
	for i := 1; i < len(e.Config.Rarities); i++ {
		if a, b := e.Config.Rarities[i-1], e.Config.Rarities[i]; b.Temperature <= a.Temperature {
			t.Errorf("%s temperature %g is not above %s %g", b.Name, b.Temperature, a.Name, a.Temperature)
		}
	}
}

// Test ID: GEN-12
// A radical's position kind is drawn at the spec's rates, and within a kind a
// radical is drawn in proportion to the square root of its frequency.
func TestRadicalDrawRates(t *testing.T) {
	e := testEngine(t)
	rng := rand.New(rand.NewChaCha8([32]byte{7}))
	spec := map[string]float64{"left": 0.5, "top": 0.18, "right": 0.1, "bottom": 0.1, "tare": 0.05, "nyo": 0.04, "kamae": 0.03}
	const n = 100000
	counts := map[string]float64{}
	for range n {
		counts[pickRadical(e.rads, map[string]bool{}, rng).position]++
	}
	stat := 0.0
	for k, p := range spec {
		stat += math.Pow(counts[k]-n*p, 2) / (n * p)
	}
	if stat > 22.458 { // χ² with 6 degrees of freedom at 0.001
		t.Errorf("position kinds drawn at %v, chi-square %.2f", counts, stat)
	}
	for _, r := range e.rads {
		if f := r.variants[0].Frequency; f >= 0.002 && math.Abs(r.weight-math.Sqrt(f)) > 1e-12 {
			t.Errorf("%s weight %g, want √%g", r.char, r.weight, f)
		}
	}
	rs := []*radical{{char: "a", position: "left", weight: 1}, {char: "b", position: "left", weight: 2}, {char: "c", position: "left", weight: 3}}
	byChar := map[string]float64{}
	for range n {
		byChar[pickIn(rs, "left", map[string]bool{}, rng).char]++
	}
	stat = 0
	for _, r := range rs {
		want := n * r.weight / 6
		stat += math.Pow(byChar[r.char]-want, 2) / want
	}
	if stat > 13.816 {
		t.Errorf("radicals drawn at %v, chi-square %.2f", byChar, stat)
	}
}

// Test ID: GEN-13
// No radical repeats within a glyph or repeats the phonetic, and a 垂・繞・構
// appears at most once, in the innermost two layers.
func TestRadicalsDoNotRepeat(t *testing.T) {
	e := testEngine(t)
	r, _ := kana.Validate(kana.Input{Mode: "on", Reading: "セイ"})
	ph := e.rank(r.Morae, r.Mode, r.Okurigana)[0]
	frames := map[string]bool{}
	for _, rad := range e.rads {
		if isFrame(rad.position) {
			frames[rad.char] = true
		}
	}
	rng := rand.New(rand.NewChaCha8([32]byte{8}))
	built := 0
	for i := 0; i < 2000; i++ {
		res, err := e.attempt(ph, 2+i%9, 8, rng, 1)
		if err != nil {
			continue
		}
		built++
		seen := map[string]bool{ph.part.Char: true}
		layer := 0
		for _, c := range res.components {
			if c.Role != "decor" {
				continue
			}
			if seen[c.Char] {
				t.Fatalf("%s repeats in %v", c.Char, res.components)
			}
			seen[c.Char] = true
			if frames[c.Char] && layer >= 2 {
				t.Fatalf("frame %s at layer %d in %v", c.Char, layer, res.components)
			}
			layer++
		}
		n := 0
		for c := range seen {
			if frames[c] {
				n++
			}
		}
		if n > 1 {
			t.Fatalf("%d frames in %v", n, res.components)
		}
	}
	if built < 1000 {
		t.Fatalf("only %d of 2000 attempts built", built)
	}
}

// Test ID: GEN-14
// Radicals are drawn without regard to the reading: two different phonetics
// given the same random stream get the same radicals.
func TestRadicalsIgnoreReading(t *testing.T) {
	e := testEngine(t)
	pick := func(reading string) candidate {
		r, _ := kana.Validate(kana.Input{Mode: "on", Reading: reading})
		return e.rank(r.Morae, r.Mode, r.Okurigana)[0]
	}
	a, b := pick("セイ"), pick("ハク")
	for _, rad := range e.rads {
		if rad.char == a.part.Char || rad.char == b.part.Char {
			t.Fatalf("phonetic %s is also a radical; pick other readings", rad.char)
		}
	}
	if a.part.Char == b.part.Char {
		t.Fatal("both readings chose the same phonetic")
	}
	decor := func(ph candidate, seed byte) []string {
		res, err := e.attempt(ph, 4, 4, rand.New(rand.NewChaCha8([32]byte{seed})), 1)
		if err != nil {
			return nil
		}
		var out []string
		for _, c := range res.components {
			if c.Role == "decor" {
				out = append(out, c.Char)
			}
		}
		return out
	}
	compared := 0
	for seed := byte(0); seed < 50; seed++ {
		x, y := decor(a, seed), decor(b, seed)
		if x == nil || y == nil {
			continue
		}
		compared++
		if strings.Join(x, "") != strings.Join(y, "") {
			t.Errorf("seed %d: radicals %v for %s but %v for %s", seed, x, a.part.Char, y, b.part.Char)
		}
	}
	if compared < 25 {
		t.Fatalf("only %d seeds built for both phonetics", compared)
	}
}

// Test ID: GEN-15
// A frame's measured inner box may reach past the frame (辶 lets the inside
// rise above its first stroke), but the inner part is still placed inside the
// frame's own rectangle.
func TestFrameInnerStaysInside(t *testing.T) {
	e := testEngine(t)
	r, _ := kana.Validate(kana.Input{Mode: "on", Reading: "セイ"})
	ph := e.rank(r.Morae, r.Mode, r.Okurigana)[0]
	m, c := e.Config.Margin, e.Config.Canvas
	b := model.Box{X: m, Y: m, W: c - 2*m, H: c - 2*m}
	outside := 0
	for _, rad := range e.rads {
		if !isFrame(rad.position) {
			continue
		}
		for _, v := range rad.variants {
			f, ok := rad.frame(v)
			if !ok {
				continue
			}
			if in := f.inner; in.X < 0 || in.Y < 0 || in.X+in.W > 1 || in.Y+in.H > 1 {
				outside++
			}
			tree := &model.Node{Operator: f.op, Children: []*model.Node{leaf(v), leaf(ph.part)}}
			parts, _, err := e.layout(tree, b, rand.New(rand.NewChaCha8([32]byte{9})), plan{inner: map[*model.Node]model.Box{tree: f.inner}})
			if err != nil {
				t.Fatalf("%s: %v", v.ID, err)
			}
			for _, p := range parts[1:] {
				if to := p.to; to.X < b.X-1e-9 || to.Y < b.Y-1e-9 || to.X+to.W > b.X+b.W+1e-9 || to.Y+to.H > b.Y+b.H+1e-9 {
					t.Errorf("%s: inner part at %+v outside the frame %+v", v.ID, to, b)
				}
			}
		}
	}
	if outside == 0 {
		t.Fatal("no frame reaches past its rectangle; the test checks nothing")
	}
}
