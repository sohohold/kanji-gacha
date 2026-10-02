package dict

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"kanji-gacha/internal/kage"
	"kanji-gacha/internal/kana"
	"kanji-gacha/internal/model"
	"kanji-gacha/internal/vector"
	"maps"
	"math"
	"regexp"
	"slices"
	"sort"
	"strings"
	"unicode"
)

// Override corrects the part with the given ID.
type Override struct {
	Exclude  bool       `json:"exclude"`
	Position string     `json:"position"`
	Operator string     `json:"operator"`
	Inner    *model.Box `json:"inner"`
}

type kanji struct {
	on, kun []Reading
	strokes int
	// parent marks the characters glyphs are built from and counted over:
	// those with a Japanese reading that are in JIS X 0208 or are 常用・人名用.
	parent bool
}

func parseKanjidic(b []byte) (map[string]*kanji, error) {
	gz, e := gzip.NewReader(bytes.NewReader(b))
	if e != nil {
		return nil, e
	}
	defer gz.Close()
	var kd struct {
		Characters []struct {
			Literal   string `xml:"literal"`
			Codepoint []struct {
				Type string `xml:"cp_type,attr"`
			} `xml:"codepoint>cp_value"`
			Grade    string `xml:"misc>grade"`
			Strokes  []int  `xml:"misc>stroke_count"`
			Readings []struct {
				Type  string `xml:"r_type,attr"`
				Value string `xml:",chardata"`
			} `xml:"reading_meaning>rmgroup>reading"`
		} `xml:"character"`
	}
	if e = xml.NewDecoder(gz).Decode(&kd); e != nil {
		return nil, e
	}
	out := map[string]*kanji{}
	for _, c := range kd.Characters {
		k := &kanji{}
		if len(c.Strokes) > 0 {
			k.strokes = c.Strokes[0]
		}
		for _, r := range c.Readings {
			if r.Type != "ja_on" && r.Type != "ja_kun" {
				continue
			}
			ps := strings.SplitN(strings.Trim(r.Value, "-"), ".", 2)
			rd := Reading{Text: kana.Normalize(ps[0])}
			if len(ps) == 2 {
				rd.Okurigana = kana.Normalize(ps[1])
			}
			if _, err := kana.Morae(rd.Text); err != nil {
				continue
			}
			if r.Type == "ja_on" {
				k.on = append(k.on, rd)
			} else {
				k.kun = append(k.kun, rd)
			}
		}
		jis := c.Grade != ""
		for _, cp := range c.Codepoint {
			jis = jis || cp.Type == "jis208"
		}
		k.parent = jis && len(k.on)+len(k.kun) > 0
		out[c.Literal] = k
	}
	return out, nil
}

// positions maps the suffix of a position variant to the position it is
// drawn for (GlyphWiki's 偏化変形: -01 偏, -02 旁, -03 冠, -04 脚, -05 囲いの外).
var positions = map[string]string{"01": "left", "02": "right", "03": "top", "04": "bottom", "05": "enclosure"}

var (
	variantName = regexp.MustCompile(`^u([0-9a-f]{4,6})-(j?)0([1-5])$`)
	plainName   = regexp.MustCompile(`^u[0-9a-f]{4,6}$`)
)

// Build makes the dictionary from the GlyphWiki dump (dump.tar.gz as
// distributed), KANJIDIC2 (kanjidic2.xml.gz) and the part overrides.
func Build(glyphwiki, kdic, overrides []byte) (Dictionary, error) {
	d := Dictionary{Sources: map[string]string{}, Report: map[string]int{}, Glyphs: map[string]string{}}
	for n, b := range map[string][]byte{"glyphwiki": glyphwiki, "kanjidic2": kdic, "overrides": overrides} {
		h := sha256.Sum256(b)
		d.Sources[n] = hex.EncodeToString(h[:])
	}
	d.Version = d.Sources["glyphwiki"][:12] + "-" + d.Sources["kanjidic2"][:12] + "-" + d.Sources["overrides"][:8]
	var ov map[string]Override
	if e := json.Unmarshal(overrides, &ov); e != nil {
		return d, e
	}
	kd, e := parseKanjidic(kdic)
	if e != nil {
		return d, e
	}
	dp, e := readDump(glyphwiki)
	if e != nil {
		return d, e
	}

	// 親字: the whole glyph of each parent character, Japanese form first.
	var parents []string
	for c, k := range kd {
		if k.parent {
			parents = append(parents, c)
		}
	}
	sort.Strings(parents)
	whole := func(c string) string {
		for _, n := range []string{nameOf(c) + "-j", nameOf(c)} {
			if _, ok := dp.newest[n]; ok {
				return n
			}
		}
		return ""
	}
	// Pinned versions are read from the file of all versions, which takes a
	// pass over the archive; references inside them may pin more.
	tried := map[string]bool{}
	fetch := func(roots []string) error {
		for range 4 {
			want := map[string]bool{}
			seen := map[string]bool{}
			for _, r := range roots {
				for _, m := range dp.closure(r, seen) {
					if strings.Contains(m, "@") && !tried[m] {
						want[m] = true
						tried[m] = true
					}
				}
			}
			if len(want) == 0 {
				return nil
			}
			missing, e := dp.fetchVersions(want)
			if e != nil {
				return e
			}
			if len(missing) == len(want) {
				return nil
			}
		}
		return nil
	}
	var roots []string
	for _, c := range parents {
		if n := whole(c); n != "" {
			roots = append(roots, n)
		}
	}
	if e = fetch(roots); e != nil {
		return d, e
	}

	// Which characters each parent is built from, through every level of
	// component references, for the reading statistics.
	sources := map[string]map[string]bool{}
	count := func(c, parent string) {
		if sources[c] == nil {
			sources[c] = map[string]bool{}
		}
		sources[c][parent] = true
	}
	for _, p := range parents {
		n := whole(p)
		if n == "" {
			d.Report["parentsMissing"]++
			continue
		}
		d.Report["parents"]++
		seen := map[string]bool{}
		d.Report["unresolvedRefs"] += len(dp.closure(n, seen))
		count(p, p)
		for g := range seen {
			if c := charOf(g); c != "" {
				count(c, p)
			}
		}
	}
	total := float64(d.Report["parents"])

	var parts []Part
	used := map[string]bool{}
	add := func(p Part) {
		if !used[p.ID] {
			used[p.ID] = true
			parts = append(parts, p)
		}
	}
	for _, c := range append(slices.Clone(parents), LegendChars...) {
		if n := whole(c); n != "" {
			add(Part{ID: n, Glyph: n, Char: c, Position: "whole"})
		}
	}
	// Position variants of the characters the parents are built from and of
	// the radical forms, the Japanese form (uXXXX-j0N) before the plain one
	// (uXXXX-0N).
	var variants []string
	for n := range dp.newest {
		if variantName.MatchString(n) {
			variants = append(variants, n)
		}
	}
	sort.Slice(variants, func(i, j int) bool {
		a, b := variantName.FindStringSubmatch(variants[i]), variantName.FindStringSubmatch(variants[j])
		if a[1]+a[3] != b[1]+b[3] {
			return a[1]+a[3] < b[1]+b[3]
		}
		return a[2] > b[2]
	})
	fs := frames(dp, parents, whole)
	frameOf := map[string]frame{}
	for _, f := range fs {
		frameOf[f.ID] = f
	}
	taken := map[string]bool{}
	for _, n := range variants {
		m := variantName.FindStringSubmatch(n)
		c, pos := charOf(n), positions["0"+m[3]]
		if !isHan(c) || len(sources[c]) == 0 && !isRadical(c) || taken[c+":"+pos] {
			continue
		}
		taken[c+":"+pos] = true
		p := Part{ID: n, Glyph: n, Char: c, Position: pos}
		if f, ok := frameOf[n]; ok {
			p = f.Part
		}
		add(p)
	}
	// A glyph that only aliases a position variant of another character
	// (⺨ is 犭 as 偏, ⻏ is 阝 as 旁) is that position of its own character.
	var plain []string
	for n := range dp.newest {
		if plainName.MatchString(n) {
			plain = append(plain, n)
		}
	}
	sort.Strings(plain)
	for _, n := range plain {
		t := dp.resolve(n)
		if t == n {
			continue
		}
		c := charOf(n)
		if m := variantName.FindStringSubmatch(t); m != nil && isHan(c) && !taken[c+":"+positions["0"+m[3]]] {
			taken[c+":"+positions["0"+m[3]]] = true
			add(Part{ID: n, Glyph: n, Char: c, Position: positions["0"+m[3]]})
		}
	}
	for _, f := range fs {
		add(f.Part)
	}
	// ⻌ aliases 辶's frame glyph, so it is that frame too.
	for _, n := range plain {
		c := charOf(n)
		if c == "" || taken[c+":frame"] {
			continue
		}
		t := dp.resolve(n)
		for _, f := range fs {
			if f.Glyph == t && f.Char != c {
				taken[c+":frame"] = true
				f.Part.ID, f.Part.Glyph, f.Part.Char = n+"#frame", n, c
				add(f.Part)
				break
			}
		}
	}

	// Parts draw with glyphs the parents do not use; read their pinned
	// versions too.
	var glyphs []string
	for _, p := range parts {
		glyphs = append(glyphs, p.Glyph)
	}
	if e = fetch(glyphs); e != nil {
		return d, e
	}
	for _, p := range parts {
		seen := map[string]bool{}
		if missing := dp.closure(p.Glyph, seen); len(missing) > 0 {
			d.Report["partsUnresolved"]++
			continue
		}
		if o, ok := ov[p.ID]; ok {
			if o.Exclude {
				continue
			}
			if o.Position != "" {
				p.Position = o.Position
			}
			if o.Operator != "" {
				p.Operator = o.Operator
			}
			if o.Inner != nil {
				p.Inner = o.Inner
			}
		}
		strokes := kage.Expand(dp.lookup(p.Glyph), dp.lookup)
		b, ok := kage.Extent(strokes)
		if !ok {
			d.Report["emptyParts"]++
			continue
		}
		p.Bounds = model.Box{X: b.MinX, Y: b.MinY, W: math.Max(b.MaxX-b.MinX, 1), H: math.Max(b.MaxY-b.MinY, 1)}
		if k := kd[p.Char]; k != nil && k.strokes > 0 {
			p.Strokes = k.strokes
		} else {
			p.Strokes = handStrokes(strokes)
			d.Report["strokesEstimated"]++
		}
		if k := kd[p.Char]; k != nil {
			p.On, p.Kun = k.on, k.kun
		}
		// Summed in a fixed order: float addition is not associative, and the
		// same input must give the same bytes.
		for _, s := range slices.Sorted(maps.Keys(sources[p.Char])) {
			for _, r := range kd[s].on {
				p.OnVector.Add(vector.Reading(r.Text), 1)
			}
			for _, r := range kd[s].kun {
				p.KunVector.Add(vector.Reading(r.Text), 1)
			}
		}
		for _, r := range p.On {
			p.OnVector.Add(vector.Reading(r.Text), 3)
		}
		for _, r := range p.Kun {
			p.KunVector.Add(vector.Reading(r.Text), 3)
		}
		p.OnVector.Normalize()
		p.KunVector.Normalize()
		p.Frequency = float64(len(sources[p.Char])) / total
		if p.OnVector != (vector.Vector{}) || p.KunVector != (vector.Vector{}) {
			d.Report["withReadings"]++
		}
		d.Report["position:"+p.Position]++
		d.Parts = append(d.Parts, p)
		for g := range seen {
			if data, ok := dp.data(g); ok {
				d.Glyphs[g] = data
			}
		}
	}
	for g := range d.Glyphs {
		if strings.Contains(g, "@") {
			d.Report["pinnedVersions"]++
		}
	}
	d.Report["glyphs"] = len(d.Glyphs)
	d.Report["parts"] = len(d.Parts)
	if len(d.Parts) == 0 {
		return d, fmt.Errorf("empty dictionary")
	}
	// The generator draws its phonetic from the whole glyphs of the parents
	// that have a reading; the legend glyphs alone leave it nothing to draw.
	candidates := 0
	for _, p := range d.Parts {
		if p.Position == "whole" && kd[p.Char] != nil && kd[p.Char].parent && (p.OnVector != (vector.Vector{}) || p.KunVector != (vector.Vector{})) {
			candidates++
		}
	}
	if candidates == 0 {
		return d, fmt.Errorf("no phonetic candidates")
	}
	if e := verifySpecials(&d, ov); e != nil {
		return d, e
	}
	return d, nil
}

// handStrokes estimates the handwritten stroke count of a glyph that
// KANJIDIC2 does not list: a KAGE stroke that starts where the previous one
// ended with a connecting or corner head (口's ㇕ is a horizontal line and a
// vertical one) continues it. It matches KANJIDIC2 for about three in four
// characters and is only a fallback.
func handStrokes(strokes []kage.Stroke) int {
	n := 0
	var prev [2]float64
	has := false
	for _, s := range strokes {
		ps := s.Points()
		if ps == nil {
			continue
		}
		cont := false
		switch s.Head() {
		case 2, 7, 12, 22, 27, 32:
			cont = has && math.Abs(ps[0][0]-prev[0]) < 1 && math.Abs(ps[0][1]-prev[1]) < 1
		}
		if !cont {
			n++
		}
		prev, has = ps[len(ps)-1], true
	}
	return n
}

// frame is a part used as 構・垂・繞.
type frame struct{ Part }

// frames finds the glyphs that parents use as 構・垂・繞: a component whose
// box holds everything else of the parent. The box it leaves for the inside
// is the median of those uses, relative to the component's own box, and the
// sides it closes give the IDS operator. A glyph needs three such uses.
func frames(dp *dump, parents []string, whole func(string) string) []frame {
	uses := map[string][]model.Box{}
	extent := func(lines []string) (kage.Box, bool) {
		return kage.Extent(kage.Expand(strings.Join(lines, "$"), dp.lookup))
	}
	for _, p := range parents {
		n := whole(p)
		if n == "" {
			continue
		}
		data, _ := dp.data(dp.resolve(n))
		refs, other := lines(data)
		for i, r := range refs {
			f, ok := extent([]string{r.line})
			if !ok {
				continue
			}
			rest := slices.Clone(other)
			for j, o := range refs {
				if j != i {
					rest = append(rest, o.line)
				}
			}
			in, ok := extent(rest)
			if !ok {
				continue
			}
			fw, fh := f.MaxX-f.MinX, f.MaxY-f.MinY
			iw, ih := in.MaxX-in.MinX, in.MaxY-in.MinY
			tx, ty := 0.15*fw, 0.15*fh
			if fw < 60 || fh < 60 || in.MinX < f.MinX-tx || in.MaxX > f.MaxX+tx || in.MinY < f.MinY-ty || in.MaxY > f.MaxY+ty || iw*ih < 0.03*fw*fh {
				continue
			}
			name := r.name
			if i := strings.IndexByte(name, '@'); i >= 0 {
				name = name[:i]
			}
			uses[name] = append(uses[name], model.Box{X: (in.MinX - f.MinX) / fw, Y: (in.MinY - f.MinY) / fh, W: (in.MaxX - in.MinX) / fw, H: (in.MaxY - in.MinY) / fh})
		}
	}
	var out []frame
	for name, us := range uses {
		if len(us) < 3 || charOf(name) == "" {
			continue
		}
		median := func(f func(model.Box) float64) float64 {
			vs := make([]float64, len(us))
			for i, u := range us {
				vs[i] = f(u)
			}
			sort.Float64s(vs)
			return vs[len(vs)/2]
		}
		x0, y0 := median(func(b model.Box) float64 { return b.X }), median(func(b model.Box) float64 { return b.Y })
		x1, y1 := median(func(b model.Box) float64 { return b.X + b.W }), median(func(b model.Box) float64 { return b.Y + b.H })
		inner := model.Box{X: round3(x0), Y: round3(y0), W: round3(x1 - x0), H: round3(y1 - y0)}
		if inner.W <= 0 || inner.H <= 0 {
			continue
		}
		op := operator(kage.Expand(dp.lookup(name), dp.lookup), inner)
		if op == "" {
			continue
		}
		id := name
		if m := variantName.FindStringSubmatch(name); m == nil || m[3] != "5" {
			id = name + "#frame"
		}
		out = append(out, frame{Part{ID: id, Glyph: name, Char: charOf(name), Position: "enclosure", Inner: &inner, Operator: op, FrameUses: len(us)}})
	}
	// The most used frame of a character comes first.
	sort.Slice(out, func(i, j int) bool {
		if out[i].FrameUses != out[j].FrameUses {
			return out[i].FrameUses > out[j].FrameUses
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// isRadical reports a character of the radical blocks (⺌, ⻖, ...), which
// parents reach only through the forms they alias.
func isRadical(c string) bool {
	for _, r := range c {
		return r >= 0x2e80 && r <= 0x2fdf
	}
	return false
}

func isHan(c string) bool {
	for _, r := range c {
		return unicode.Is(unicode.Han, r)
	}
	return false
}

func round3(v float64) float64 { return math.Round(v*1000) / 1000 }

// operator is the IDS operator of a frame from the sides it closes around
// the inner box (relative to the frame's box). A side is closed when a probe
// from the inner box outwards, at a quarter, half or three quarters along the
// side, crosses a stroke: 門's top is closed although its middle is open.
func operator(strokes []kage.Stroke, rel model.Box) string {
	f, ok := kage.Extent(strokes)
	if !ok {
		return ""
	}
	fw, fh := f.MaxX-f.MinX, f.MaxY-f.MinY
	in := kage.Box{MinX: f.MinX + rel.X*fw, MinY: f.MinY + rel.Y*fh, MaxX: f.MinX + (rel.X+rel.W)*fw, MaxY: f.MinY + (rel.Y+rel.H)*fh}
	var segs [][4]float64
	for _, s := range strokes {
		ps := s.Points()
		for i := 1; i < len(ps); i++ {
			segs = append(segs, [4]float64{ps[i-1][0], ps[i-1][1], ps[i][0], ps[i][1]})
		}
	}
	closed := func(vertical bool, from, to float64) bool {
		for _, t := range []float64{0.25, 0.5, 0.75} {
			var probe [4]float64
			if vertical {
				x := in.MinX + t*(in.MaxX-in.MinX)
				probe = [4]float64{x, from, x, to}
			} else {
				y := in.MinY + t*(in.MaxY-in.MinY)
				probe = [4]float64{from, y, to, y}
			}
			for _, s := range segs {
				if crosses(probe, s) {
					return true
				}
			}
		}
		return false
	}
	t := in.MinY > f.MinY && closed(true, f.MinY-1, in.MinY)
	b := in.MaxY < f.MaxY && closed(true, in.MaxY, f.MaxY+1)
	l := in.MinX > f.MinX && closed(false, f.MinX-1, in.MinX)
	r := in.MaxX < f.MaxX && closed(false, in.MaxX, f.MaxX+1)
	switch {
	case l && t && r && b:
		return "⿴"
	case l && t && r:
		return "⿵"
	case l && b && r:
		return "⿶"
	case t && l && b:
		return "⿷"
	case t && l:
		return "⿸"
	case t && r:
		return "⿹"
	case l && b:
		return "⿺"
	}
	return ""
}

// crosses reports whether two segments intersect.
func crosses(a, b [4]float64) bool {
	side := func(x1, y1, x2, y2, px, py float64) float64 { return (x2-x1)*(py-y1) - (y2-y1)*(px-x1) }
	d1 := side(a[0], a[1], a[2], a[3], b[0], b[1])
	d2 := side(a[0], a[1], a[2], a[3], b[2], b[3])
	d3 := side(b[0], b[1], b[2], b[3], a[0], a[1])
	d4 := side(b[0], b[1], b[2], b[3], a[2], a[3])
	return d1*d2 <= 0 && d3*d4 <= 0 && !(d1 == 0 && d2 == 0)
}

// LegendChars are the whole-glyph assets the special slots draw from; a build
// without them would fail only at request time.
var LegendChars = []string{"彁", "妛", "雲", "龍"}

// verifySpecials fails the build when an enabled special slot has no drawing
// asset, or when an override no longer matches any part.
func verifySpecials(d *Dictionary, ov map[string]Override) error {
	whole, used, frames := map[string]bool{}, map[string]bool{}, 0
	for i := range d.Parts {
		p := &d.Parts[i]
		if p.Position == "whole" {
			whole[p.Char] = true
		}
		used[p.ID] = true
		if p.Inner != nil && p.Operator != "" {
			frames++
		}
	}
	for _, c := range LegendChars {
		if !whole[c] {
			return fmt.Errorf("legend asset missing: no whole part for %q", c)
		}
	}
	ids := make([]string, 0, len(ov))
	for id := range ov {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		o := ov[id]
		if o.Inner != nil && o.Operator == "" {
			return fmt.Errorf("override %s sets inner without operator", id)
		}
		if !o.Exclude && !used[id] {
			return fmt.Errorf("override %s matched no part", id)
		}
	}
	if frames == 0 {
		return fmt.Errorf("no frame part with both inner and operator")
	}
	d.Report["frameParts"] = frames
	return nil
}
