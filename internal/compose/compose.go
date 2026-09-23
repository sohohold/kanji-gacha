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
	byPos  map[[2]string][]*dict.Part
	rads   []*radical
}

func New(c config.Config, d dict.Dictionary) *Engine {
	e := &Engine{Config: c, Dict: d, byID: map[string]*dict.Part{}, whole: map[string]*dict.Part{}, byPos: map[[2]string][]*dict.Part{}}
	for i := range d.Parts {
		p := &d.Parts[i]
		e.byID[p.ID] = p
		if p.Position == "whole" {
			e.whole[p.Char] = p
		}
		if p.Position == "right" || p.Position == "bottom" {
			k := [2]string{p.Char, p.Position}
			e.byPos[k] = append(e.byPos[k], p)
		}
	}
	for _, ps := range e.byPos {
		pos := ps[0].Position
		sort.SliceStable(ps, func(i, j int) bool { return span(ps[i], pos) > span(ps[j], pos) })
	}
	e.rads = buildRadicals(d.Parts)
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
		// 音符は実在字から選び、置き場所に合う位置別字形は後で引き当てる。
		if p.Position != "whole" || len(p.Paths) > e.Config.MaxStrokes {
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
		// 他の字の旁・脚として使われる字（青・干・生など）は音符らしく自然に収まるので優先する。
		if len(e.byPos[[2]string{p.Char, "right"}])+len(e.byPos[[2]string{p.Char, "bottom"}]) > 0 {
			score += 0.3
		}
		key := p.Char
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

// plan carries per-node layout hints: which leaves are radicals and the inner
// box of each frame node.
type plan struct {
	radical map[*model.Node]bool
	inner   map[*model.Node]model.Box
}

// fitAspect maps a part into slot, stretching one axis at most maxStretch times
// the other and centring the rest, so borrowed shapes keep their proportions.
func fitAspect(p *dict.Part, slot model.Box, maxStretch float64) []model.Stroke {
	sx, sy := slot.W/p.Bounds.W, slot.H/p.Bounds.H
	to := slot
	if sx > sy*maxStretch {
		to.W = p.Bounds.W * sy * maxStretch
		to.X += (slot.W - to.W) / 2
	} else if sy > sx*maxStretch {
		to.H = p.Bounds.H * sx * maxStretch
		to.Y += (slot.H - to.H) / 2
	}
	return render.Fit(p.Paths, p.Bounds, to)
}

// sideShare is the fraction of the slot a radical leaf takes along the split
// axis, derived from its natural proportions (偏は細く、冠は低く).
func (e *Engine) sideShare(p *dict.Part, horizontal bool, b model.Box) float64 {
	full := e.Config.Canvas - 2*e.Config.Margin
	if horizontal {
		hf := math.Min(1, math.Max(0.55, p.Bounds.H/full*1.4))
		w := b.H * hf * p.Bounds.W / p.Bounds.H
		return math.Min(0.45, math.Max(0.2, w/(b.W-e.Config.Gap)))
	}
	wf := math.Min(1, math.Max(0.6, p.Bounds.W/full*1.3))
	h := b.W * wf * p.Bounds.H / p.Bounds.W
	return math.Min(0.42, math.Max(0.18, h/(b.H-e.Config.Gap)))
}

func (e *Engine) layout(n *model.Node, b model.Box, rng *rand.Rand, pl plan) ([]model.Stroke, float64, error) {
	if b.W <= 0 || b.H <= 0 {
		return nil, 0, fmt.Errorf("empty slot")
	}
	penalty := math.Max(0, e.Config.MinSlot-b.W) + math.Max(0, e.Config.MinSlot-b.H)
	if n.Operator == "" {
		p := e.byID[n.ComponentID]
		if p == nil {
			return nil, 0, fmt.Errorf("unknown component")
		}
		if pl.radical[n] {
			return fitAspect(p, b, 1.15), penalty, nil
		}
		return fitAspect(p, b, 1.6), penalty, nil
	}
	if len(n.Children) != 2 {
		return nil, 0, fmt.Errorf("invalid tree")
	}
	a, z := b, b
	if n.Operator == "⿰" || n.Operator == "⿱" {
		horizontal := n.Operator == "⿰"
		ratio := e.Config.MinRatio + rng.Float64()*(e.Config.MaxRatio-e.Config.MinRatio)
		if c := n.Children[0]; pl.radical[c] {
			ratio = e.sideShare(e.byID[c.ComponentID], horizontal, b)
		} else if c := n.Children[1]; pl.radical[c] {
			ratio = 1 - e.sideShare(e.byID[c.ComponentID], horizontal, b)
		}
		if horizontal {
			a.W = (b.W - e.Config.Gap) * ratio
			z.X = b.X + a.W + e.Config.Gap
			z.W = b.W - a.W - e.Config.Gap
		} else {
			a.H = (b.H - e.Config.Gap) * ratio
			z.Y = b.Y + a.H + e.Config.Gap
			z.H = b.H - a.H - e.Config.Gap
		}
	} else {
		in, ok := pl.inner[n]
		if !ok {
			p := e.byID[n.Children[0].ComponentID]
			if p == nil || p.Inner == nil {
				return nil, 0, fmt.Errorf("missing frame")
			}
			in = *p.Inner
		}
		z = model.Box{X: b.X + in.X*b.W, Y: b.Y + in.Y*b.H, W: in.W * b.W, H: in.H * b.H}
		p := e.byID[n.Children[0].ComponentID]
		if p == nil {
			return nil, 0, fmt.Errorf("unknown component")
		}
		inner, pi, err := e.layout(n.Children[1], z, rng, pl)
		if err != nil {
			return nil, 0, err
		}
		return append(render.Fit(p.Paths, p.Bounds, b), inner...), penalty + pi, nil
	}
	left, pa, err := e.layout(n.Children[0], a, rng, pl)
	if err != nil {
		return nil, 0, err
	}
	right, pz, err := e.layout(n.Children[1], z, rng, pl)
	return append(left, right...), penalty + pa + pz, err
}

type result struct {
	tree       *model.Node
	paths      []model.Stroke
	components []model.Component
	score      float64
}

// phonetic returns the variant of the chosen character that suits where it
// sits: a 旁 form beside a 偏, a 脚 form under a 冠, otherwise the whole glyph.
func (e *Engine) phonetic(char, position string, rng *rand.Rand) *dict.Part {
	if vs := e.byPos[[2]string{char, position}]; len(vs) > 0 && rng.IntN(4) != 0 {
		return vs[rng.IntN(min(2, len(vs)))]
	}
	return e.whole[char]
}

// coreSize is how many copies of the phonetic form the core (同字重ね).
func coreSize(r config.Rarity, rng *rand.Rand) int {
	switch r.Name {
	case "SSR":
		return 1 + rng.IntN(3)
	case "UR":
		return 2 + rng.IntN(3)
	}
	return 1
}

// attempt builds one 形声 style glyph: the phonetic core, wrapped from the
// inside out by radicals drawn independently of the reading.
func (e *Engine) attempt(ph candidate, count, depth int, rng *rand.Rand, cores int) (result, error) {
	cores = max(1, min(cores, count, 4))
	coreDepth := map[int]int{1: 0, 2: 1, 3: 2, 4: 2}
	// 意符1つごとに深さが1増えるので、深さが足りないときは音符の重ねを増やして選んだ部品数を保つ。
	for cores < min(count, 4) && cores+depth-coreDepth[cores] < count {
		cores++
	}
	wraps := min(count-cores, depth-coreDepth[cores])
	if len(e.rads) == 0 {
		wraps = 0
	}
	used := map[string]bool{ph.part.Char: true}
	var rads []*radical
	frame := false
	for len(rads) < wraps {
		var r *radical
		for try := 0; try < 8; try++ {
			r = pickRadical(e.rads, used, rng)
			isFrame := r != nil && (r.position == "tare" || r.position == "nyo" || r.position == "kamae")
			// 構・垂・繞は内側の1〜2層に1つだけにする。
			if r == nil || !isFrame || !frame && len(rads) < 2 {
				frame = frame || isFrame
				break
			}
			r = nil
		}
		if r == nil {
			break
		}
		used[r.char] = true
		rads = append(rads, r)
	}
	pl := plan{radical: map[*model.Node]bool{}, inner: map[*model.Node]model.Box{}}
	cs := []model.Component{}
	position := "whole"
	if cores == 1 && len(rads) > 0 {
		switch rads[0].position {
		case "left":
			position = "right"
		case "top":
			position = "bottom"
		}
	}
	core := func() *model.Node {
		p := e.phonetic(ph.part.Char, position, rng)
		if p == nil {
			p = ph.part
		}
		cs = append(cs, model.Component{ID: p.ID, Char: p.Char, Role: "reading", Score: ph.score})
		return leaf(p)
	}
	var tree *model.Node
	pair := func(op string, a, b *model.Node) *model.Node {
		return &model.Node{Operator: op, Children: []*model.Node{a, b}}
	}
	switch cores {
	case 1:
		tree = core()
	case 2:
		tree = pair([]string{"⿰", "⿱"}[rng.IntN(2)], core(), core())
	case 3:
		tree = pair("⿱", core(), pair("⿰", core(), core()))
	default:
		tree = pair("⿰", pair("⿱", core(), core()), pair("⿱", core(), core()))
	}
	for _, r := range rads {
		p := r.variant(rng)
		l := leaf(p)
		cs = append(cs, model.Component{ID: p.ID, Char: p.Char, Role: "decor"})
		switch r.position {
		case "left":
			pl.radical[l] = true
			tree = pair("⿰", l, tree)
		case "right":
			pl.radical[l] = true
			tree = pair("⿰", tree, l)
		case "top":
			pl.radical[l] = true
			tree = pair("⿱", l, tree)
		case "bottom":
			pl.radical[l] = true
			tree = pair("⿱", tree, l)
		default:
			f, _ := r.frame(p)
			tree = pair(f.op, l, tree)
			pl.inner[tree] = f.inner
		}
	}
	m, c := e.Config.Margin, e.Config.Canvas
	paths, penalty, err := e.layout(tree, model.Box{X: m, Y: m, W: c - 2*m, H: c - 2*m}, rng, pl)
	if err != nil {
		return result{}, err
	}
	if len(paths) > e.Config.MaxStrokes {
		return result{}, fmt.Errorf("stroke limit")
	}
	return result{tree: tree, paths: paths, components: cs, score: -penalty}, nil
}
func (e *Engine) Generate(ctx context.Context, r kana.Reading, now time.Time) (model.Glyph, error) {
	var seed [32]byte
	if _, err := crand.Read(seed[:]); err != nil {
		return model.Glyph{}, err
	}
	rng := rand.New(rand.NewChaCha8(seed))
	rarity := e.Rarity(rng)
	g := model.Glyph{SchemaVersion: 1, GeneratorVersion: 2, ConfigVersion: e.Config.Version, DictionaryVersion: e.Dict.Version, Key: r.Key(), Reading: r, CreatedAt: now.UTC(), Rarity: rarity.Name, License: render.License, OGPReading: r.Display}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(e.Config.TimeoutMs)*time.Millisecond)
	defer cancel()
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
			paths, _, err := e.layout(tree, model.Box{X: m, Y: m, W: c - 2*m, H: c - 2*m}, rng, plan{})
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
		// The build verifies these assets, so this only happens if a dictionary
		// slipped through; draw a normal glyph rather than failing the request.
	}
	cands := e.rank(r.Morae, r.Mode, r.Okurigana)
	if len(cands) == 0 {
		return g, fmt.Errorf("no reading candidates")
	}
	// 音符は1回だけ温度付きで抽選し、候補間では形の組み方だけを競わせる。
	ph := choose(cands, rarity.Temperature, rng)
	// 退避候補は意符1つ＋音符（N相当）を基本に、レアリティの部品数や総画数の上限を
	// 超えるときは音符だけにする（音符は rank の段階で画数上限内に絞ってある）。
	best, err := e.attempt(ph, min(2, rarity.MaxParts), 1, rng, 1)
	if err != nil {
		best, err = e.attempt(ph, 1, 1, rng, 1)
	}
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
				v, err := e.attempt(ph, n, rarity.MaxDepth, rr, coreSize(rarity, rr))
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
