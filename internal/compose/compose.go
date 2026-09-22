package compose

import (
	"context"
	crand "crypto/rand"
	"fmt"
	"kanji-gacha/config"
	"kanji-gacha/internal/dict"
	"kanji-gacha/internal/kana"
	"kanji-gacha/internal/model"
	"kanji-gacha/internal/render"
	"kanji-gacha/internal/vector"
	"math"
	"math/rand/v2"
	"sort"
	"strings"
	"sync"
	"time"
)

type Engine struct {
	Config config.Config
	Dict   dict.Dictionary
	byID   map[string]*dict.Part
	whole  map[string]*dict.Part
	frames []*dict.Part
}

func New(c config.Config, d dict.Dictionary) *Engine {
	e := &Engine{Config: c, Dict: d, byID: map[string]*dict.Part{}, whole: map[string]*dict.Part{}}
	for i := range d.Parts {
		p := &d.Parts[i]
		e.byID[p.ID] = p
		if p.Position == "whole" {
			e.whole[p.Char] = p
		}
		if p.Inner != nil && p.Operator != "" {
			e.frames = append(e.frames, p)
		}
	}
	return e
}

type candidate struct {
	part  *dict.Part
	score float64
}

func (e *Engine) rank(morae []string, mode, okuri string) []candidate {
	v := vector.Encode(morae)
	s := strings.Join(morae, "")
	best := map[string]candidate{}
	for i := range e.Dict.Parts {
		p := &e.Dict.Parts[i]
		if len(p.Paths) > e.Config.MaxStrokes {
			continue
		}
		vec, readings := p.OnVector, p.On
		if mode == "kun" {
			vec, readings = p.KunVector, p.Kun
		}
		if vec == (vector.Vector{}) {
			continue
		}
		score := vector.Cos(v, vec) - e.Config.Gamma*p.Frequency
		for _, r := range readings {
			if r.Text == s {
				score += e.Config.Alpha
				if mode == "kun" && r.Okurigana == okuri {
					score += e.Config.Beta
				}
				break
			}
		}
		// Avoid letting characters with many source variants dominate the top-k pool.
		key := p.Char + ":" + p.Position
		old, ok := best[key]
		if !ok || score > old.score {
			best[key] = candidate{p, score}
		}
	}
	out := make([]candidate, 0, len(best))
	for _, c := range best {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].score == out[j].score {
			return out[i].part.ID < out[j].part.ID
		}
		return out[i].score > out[j].score
	})
	if len(out) > e.Config.TopK {
		out = out[:e.Config.TopK]
	}
	return out
}

// segment uses DP to select up to three contiguous reading intervals; all spans are cached.
func (e *Engine) segment(r kana.Reading, slots int) [][]candidate {
	n := len(r.Morae)
	kmax := min(3, slots, n)
	cache := map[[2]int][]candidate{}
	get := func(i, j int) []candidate {
		key := [2]int{i, j}
		v, ok := cache[key]
		if !ok {
			okuri := ""
			if i == 0 && j == n {
				okuri = r.Okurigana
			}
			v = e.rank(r.Morae[i:j], r.Mode, okuri)
			cache[key] = v
		}
		return v
	}
	dp := make([][]float64, kmax+1)
	prev := make([][]int, kmax+1)
	for k := range dp {
		dp[k] = make([]float64, n+1)
		prev[k] = make([]int, n+1)
		for j := range dp[k] {
			dp[k][j] = math.Inf(-1)
		}
	}
	dp[0][0] = 0
	for k := 1; k <= kmax; k++ {
		for j := k; j <= n; j++ {
			for i := k - 1; i < j; i++ {
				cs := get(i, j)
				if len(cs) == 0 {
					continue
				}
				score := dp[k-1][i] + cs[0].score*float64(j-i) - 0.08
				if score > dp[k][j] {
					dp[k][j] = score
					prev[k][j] = i
				}
			}
		}
	}
	k := 1
	for a := 2; a <= kmax; a++ {
		if dp[a][n] > dp[k][n] {
			k = a
		}
	}
	var out [][]candidate
	for j := n; k > 0; k-- {
		i := prev[k][j]
		out = append(out, get(i, j))
		j = i
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}
func choose(cs []candidate, t float64, rng *rand.Rand) candidate {
	sum := 0.0
	for _, c := range cs {
		sum += math.Exp((c.score - cs[0].score) / t)
	}
	x := rng.Float64() * sum
	for _, c := range cs {
		x -= math.Exp((c.score - cs[0].score) / t)
		if x <= 0 {
			return c
		}
	}
	return cs[len(cs)-1]
}
func (e *Engine) Rarity(rng *rand.Rand) config.Rarity {
	x := rng.IntN(10000)
	for _, r := range e.Config.Rarities {
		x -= r.Weight
		if x < 0 {
			return r
		}
	}
	return e.Config.Rarities[0]
}
func leaf(p *dict.Part) *model.Node { return &model.Node{ComponentID: p.ID, Char: p.Char} }
func (e *Engine) tree(parts []*dict.Part, depth int, rng *rand.Rand) *model.Node {
	if len(parts) == 1 {
		return leaf(parts[0])
	}
	n := len(parts)
	minLeft := max(1, n-(1<<(depth-1)))
	maxLeft := min(n-1, 1<<(depth-1))
	cut := minLeft + rng.IntN(maxLeft-minLeft+1)
	op := "⿰"
	if rng.IntN(2) == 0 {
		op = "⿱"
	}
	if cut == 1 {
		switch parts[0].Position {
		case "left":
			op = "⿰"
		case "top":
			op = "⿱"
		}
	}
	return &model.Node{Operator: op, Children: []*model.Node{e.tree(parts[:cut], depth-1, rng), e.tree(parts[cut:], depth-1, rng)}}
}
func (e *Engine) layout(n *model.Node, b model.Box, rng *rand.Rand) ([]model.Stroke, float64, error) {
	if b.W <= 0 || b.H <= 0 {
		return nil, 0, fmt.Errorf("empty slot")
	}
	penalty := math.Max(0, e.Config.MinSlot-b.W) + math.Max(0, e.Config.MinSlot-b.H)
	if n.Operator == "" {
		p := e.byID[n.ComponentID]
		if p == nil {
			return nil, 0, fmt.Errorf("unknown component")
		}
		return render.Fit(p.Paths, p.Bounds, b), penalty, nil
	}
	if len(n.Children) != 2 {
		return nil, 0, fmt.Errorf("invalid tree")
	}
	a, z := b, b
	if n.Operator == "⿰" || n.Operator == "⿱" {
		ratio := e.Config.MinRatio + rng.Float64()*(e.Config.MaxRatio-e.Config.MinRatio)
		if n.Operator == "⿰" {
			a.W = (b.W - e.Config.Gap) * ratio
			z.X = b.X + a.W + e.Config.Gap
			z.W = b.W - a.W - e.Config.Gap
		} else {
			a.H = (b.H - e.Config.Gap) * ratio
			z.Y = b.Y + a.H + e.Config.Gap
			z.H = b.H - a.H - e.Config.Gap
		}
	} else {
		p := e.byID[n.Children[0].ComponentID]
		if p == nil || p.Inner == nil {
			return nil, 0, fmt.Errorf("missing frame")
		}
		z = model.Box{X: b.X + p.Inner.X*b.W, Y: b.Y + p.Inner.Y*b.H, W: p.Inner.W * b.W, H: p.Inner.H * b.H}
	}
	left, pl, err := e.layout(n.Children[0], a, rng)
	if err != nil {
		return nil, 0, err
	}
	right, pr, err := e.layout(n.Children[1], z, rng)
	return append(left, right...), penalty + pl + pr, err
}

type result struct {
	tree       *model.Node
	paths      []model.Stroke
	components []model.Component
	score      float64
}

func (e *Engine) attempt(groups [][]candidate, count, depth int, temp float64, rng *rand.Rand, frame bool) (result, error) {
	parts := make([]*dict.Part, 0, count)
	cs := make([]model.Component, 0, count)
	quality := 0.0
	for i := 0; i < count; i++ {
		pool := groups[i%len(groups)]
		if len(pool) == 0 {
			return result{}, fmt.Errorf("no reading candidates")
		}
		c := choose(pool, temp, rng)
		parts = append(parts, c.part)
		role := "reading"
		if i >= len(groups) {
			role = "decor"
		}
		cs = append(cs, model.Component{ID: c.part.ID, Char: c.part.Char, Role: role, Score: c.score})
		quality += c.score
	}
	var tree *model.Node
	if frame && len(e.frames) > 0 && count >= 3 {
		p := e.frames[rng.IntN(len(e.frames))]
		parts[0] = p
		cs[0] = model.Component{ID: p.ID, Char: p.Char, Role: "decor"}
		tree = &model.Node{Operator: p.Operator, Children: []*model.Node{leaf(p), e.tree(parts[1:], depth-1, rng)}}
	} else {
		tree = e.tree(parts, depth, rng)
	}
	m, c := e.Config.Margin, e.Config.Canvas
	paths, penalty, err := e.layout(tree, model.Box{X: m, Y: m, W: c - 2*m, H: c - 2*m}, rng)
	if err != nil {
		return result{}, err
	}
	if len(paths) > e.Config.MaxStrokes {
		return result{}, fmt.Errorf("stroke limit")
	}
	return result{tree: tree, paths: paths, components: cs, score: quality/float64(count) - penalty*0.02}, nil
}
func (e *Engine) Generate(ctx context.Context, r kana.Reading, now time.Time) (model.Glyph, error) {
	var seed [32]byte
	if _, err := crand.Read(seed[:]); err != nil {
		return model.Glyph{}, err
	}
	rng := rand.New(rand.NewChaCha8(seed))
	rarity := e.Rarity(rng)
	g := model.Glyph{SchemaVersion: 1, GeneratorVersion: 1, ConfigVersion: e.Config.Version, DictionaryVersion: e.Dict.Version, Key: r.Key(), Reading: r, CreatedAt: now.UTC(), Rarity: rarity.Name, License: render.License, OGPReading: r.Display}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(e.Config.TimeoutMs)*time.Millisecond)
	defer cancel()
	if r.Reading == "ビャン" {
		g.SVG = render.Biang
		g.IDS = "𰻞"
		g.Tree = &model.Node{Char: "𰻞", ComponentID: "noto:30ede"}
		g.Strokes = 58
		g.Rarity = "UR"
		legend := "biang"
		g.Legend = &legend
		g.Components = []model.Component{{ID: "noto:30ede", Char: "𰻞", Role: "decor"}}
		g.License = "Noto Sans CJK JP © Adobe, converted to outlines; SIL OFL 1.1"
		return g, nil
	}

	// Ghost glyphs come from the same pinned dictionary and follow the same storage path.
	jst := now.In(time.FixedZone("JST", 9*3600))
	ghost := 0.003
	if jst.Hour() == 2 && jst.Minute() < 30 {
		ghost = 0.03
	}
	special := ""
	if r.Reading == "タイト" {
		special = "taito"
	} else if rng.Float64() < ghost {
		special = "ghost"
	}
	if special != "" {
		var tree *model.Node
		if special == "ghost" {
			p := e.whole[[]string{"彁", "妛"}[rng.IntN(2)]]
			if p != nil {
				tree = leaf(p)
			}
		} else {
			cloud, dragon := e.whole["雲"], e.whole["龍"]
			if cloud != nil && dragon != nil {
				triple := func(p *dict.Part) *model.Node {
					return &model.Node{Operator: "⿱", Children: []*model.Node{leaf(p), {Operator: "⿰", Children: []*model.Node{leaf(p), leaf(p)}}}}
				}
				tree = &model.Node{Operator: "⿱", Children: []*model.Node{triple(cloud), triple(dragon)}}
			}
		}
		if tree != nil {
			m, c := e.Config.Margin, e.Config.Canvas
			paths, _, err := e.layout(tree, model.Box{X: m, Y: m, W: c - 2*m, H: c - 2*m}, rng)
			if err == nil {
				g.Tree = tree
				g.IDS = tree.IDS()
				g.Strokes = len(paths)
				g.SVG = render.SVG(paths, c, e.Config.StrokeWidth, r.Display+" "+g.IDS)
				g.Legend = &special
				g.Rarity = "UR"
				g.Components = []model.Component{}
				var walk func(*model.Node)
				walk = func(n *model.Node) {
					if n.Operator == "" {
						g.Components = append(g.Components, model.Component{ID: n.ComponentID, Char: n.Char, Role: "decor"})
					}
					for _, c := range n.Children {
						walk(c)
					}
				}
				walk(tree)
				return g, nil
			}
		}
		return g, fmt.Errorf("missing legend asset")
	}
	groups := e.segment(r, rarity.MaxParts)
	best, err := e.attempt(groups, 1, 1, rarity.Temperature, rng, false)
	if err != nil {
		return g, err
	}
	fallback := true
	// Each worker owns its PRNG; no shared rand.Rand or mutable dictionary state.
	results := make(chan result, e.Config.Candidates)
	var wg sync.WaitGroup
	for w := 0; w < e.Config.Workers; w++ {
		var s [32]byte
		if _, err = crand.Read(s[:]); err != nil {
			return g, err
		}
		wg.Add(1)
		go func(worker int, seed [32]byte) {
			defer wg.Done()
			rr := rand.New(rand.NewChaCha8(seed))
			for i := worker; i < e.Config.Candidates; i += e.Config.Workers {
				if ctx.Err() != nil {
					return
				}
				n := rarity.MinParts + rr.IntN(rarity.MaxParts-rarity.MinParts+1)
				frame := n >= 3 && n-1 <= 1<<(rarity.MaxDepth-1) && rr.IntN(3) == 0
				v, err := e.attempt(groups, n, rarity.MaxDepth, rarity.Temperature, rr, frame)
				if err == nil {
					select {
					case results <- v:
					case <-ctx.Done():
						return
					}
				}
			}
		}(w, s)
	}
	go func() { wg.Wait(); close(results) }()
collect:
	for {
		select {
		case v, ok := <-results:
			if !ok {
				break collect
			}
			if fallback || v.score > best.score {
				best = v
				fallback = false
			}
		case <-ctx.Done():
			break collect
		}
	}
	g.Fallback = fallback
	g.Tree = best.tree
	g.IDS = best.tree.IDS()
	g.Components = best.components
	g.Strokes = len(best.paths)
	g.SVG = render.SVG(best.paths, e.Config.Canvas, e.Config.StrokeWidth, r.Display+" "+g.IDS)
	return g, nil
}
