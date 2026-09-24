// This file is part of a Go port of the KAGE engine (GPL v3); see kage.go for
// the origin, the changes made and the license.

package kage

import "math"

// mincho holds the parameters of the Mincho style at its default size.
type mincho struct {
	kRate       float64
	kMinWidthY  float64
	kMinWidthU  float64
	kMinWidthT  float64
	kWidth      float64
	kKakato     float64
	kL2RDfatten float64
	kMage       float64

	kAdjustKakatoL      []float64
	kAdjustKakatoR      []float64
	kAdjustKakatoRangeX float64
	kAdjustKakatoRangeY []float64
	kAdjustKakatoStep   int

	kAdjustUrokoX          []float64
	kAdjustUrokoY          []float64
	kAdjustUrokoLength     []float64
	kAdjustUrokoLengthStep int
	kAdjustUrokoLine       []float64

	kAdjustUroko2Step   float64
	kAdjustUroko2Length float64
	kAdjustTateStep     float64
	kAdjustMageStep     float64
}

var font = mincho{
	kRate:       100,
	kMinWidthY:  2,
	kMinWidthU:  2,
	kMinWidthT:  6,
	kWidth:      5,
	kKakato:     3,
	kL2RDfatten: 1.1,
	kMage:       10,

	kAdjustKakatoL:      []float64{14, 9, 5, 2, 0},
	kAdjustKakatoR:      []float64{8, 6, 4, 2},
	kAdjustKakatoRangeX: 20,
	kAdjustKakatoRangeY: []float64{1, 19, 24, 30},
	kAdjustKakatoStep:   3,

	kAdjustUrokoX:          []float64{24, 20, 16, 12},
	kAdjustUrokoY:          []float64{12, 11, 9, 8},
	kAdjustUrokoLength:     []float64{22, 36, 50},
	kAdjustUrokoLengthStep: 3,
	kAdjustUrokoLine:       []float64{22, 26, 30},

	kAdjustUroko2Step:   3,
	kAdjustUroko2Length: 40,
	kAdjustTateStep:     4,
	kAdjustMageStep:     5,
}

// Render draws the strokes as Mincho outlines and returns the polygons each
// stroke produced, in stroke order. Stroke shapes are adjusted against the
// other strokes (thinner verticals when crowded, smaller ウロコ, ...), so a
// glyph must be rendered in one call, not stroke by stroke. A transform line
// (0:97, 0:98, 0:99) moves the polygons drawn before it, whichever stroke
// drew them.
func Render(strokes []Stroke) [][]Polygon {
	adj := adjustStrokes(strokes)
	var ps polygons
	ends := make([]int, len(adj))
	for i := range adj {
		dfDrawFont(&ps, &adj[i])
		ends[i] = len(ps.array)
	}
	out := make([][]Polygon, len(adj))
	start := 0
	for i, end := range ends {
		for _, p := range ps.array[start:end] {
			out[i] = append(out[i], p.out())
		}
		start = end
	}
	return out
}

type adjusted struct {
	stroke Stroke

	// kirikuchi is set by adjustKirikuchi when a1 = 2, a2 = 32, x1 > x2 and
	// y1 < y2; otherwise it is a2_opt_1.
	kirikuchi float64
	// tate is set by adjustTate when a1 is 1, 3 or 7 and x1 = x2; otherwise it
	// is a2_opt_2 + a2_opt_3 * 10.
	tate float64
	// hane is set by adjustHane when a1 is 1, 2 or 6 and a3 = 4; otherwise it
	// is a3_opt_1.
	hane float64
	// uroko is set by adjustUroko or adjustUroko2 when a1 = 1 and a3 = 0;
	// otherwise it is a3_opt.
	uroko float64
	// kakato is set by adjustKakato when a1 = 1 and a3 is 13 or 23; otherwise
	// it is a3_opt.
	kakato float64
	// mage is set by adjustMage when a1 = 3 and y2 = y3; otherwise it is
	// a3_opt_2.
	mage float64
}

func adjustStrokes(strokes []Stroke) []adjusted {
	adj := make([]adjusted, len(strokes))
	for i, s := range strokes {
		adj[i] = adjusted{
			stroke:    s,
			kirikuchi: s.a2_opt_1,
			tate:      s.a2_opt_2 + s.a2_opt_3*10,
			hane:      s.a3_opt_1,
			uroko:     s.a3_opt,
			kakato:    s.a3_opt,
			mage:      s.a3_opt_2,
		}
	}
	adjustHane(adj)
	adjustMage(adj)
	adjustTate(adj)
	adjustKakato(adj)
	adjustUroko(adj)
	adjustUroko2(adj)
	adjustKirikuchi(adj)
	return adj
}

func adjustHane(adj []adjusted) {
	type seg struct {
		i         int
		x, y1, y2 float64
	}
	var vert []seg
	for i := range adj {
		s := &adj[i].stroke
		if s.a1_100 == 1 && s.a1_opt == 0 && s.x1 == s.x2 {
			vert = append(vert, seg{i, s.x1, s.y1, s.y2})
		}
	}
	for i := range adj {
		s := &adj[i].stroke
		if (s.a1_100 == 1 || s.a1_100 == 2 || s.a1_100 == 6) && s.a1_opt == 0 && s.a3_100 == 4 && s.a3_opt == 0 {
			var lpx, lpy float64
			switch s.a1_100 {
			case 1:
				lpx, lpy = s.x2, s.y2
			case 2:
				lpx, lpy = s.x3, s.y3
			default:
				lpx, lpy = s.x4, s.y4
			}
			mn := math.Inf(1)
			if lpx+18 < 100 {
				mn = lpx + 18
			}
			for _, v := range vert {
				if i != v.i && lpx-v.x < 100 && v.x < lpx && v.y1 <= lpy && v.y2 >= lpy {
					mn = jsMin(mn, lpx-v.x)
				}
			}
			if !math.IsInf(mn, 1) {
				adj[i].hane += 7 - math.Floor(mn/15)
			}
		}
	}
}

type horiSeg struct {
	i         int
	target    bool
	y, x1, x2 float64
}

func adjustMage(adj []adjusted) {
	var hori []horiSeg
	for i := range adj {
		s := &adj[i].stroke
		if s.a1_100 == 1 && s.a1_opt == 0 && s.y1 == s.y2 {
			hori = append(hori, horiSeg{i, false, s.y2, s.x1, s.x2})
		} else if s.a1_100 == 3 && s.a1_opt == 0 && s.y2 == s.y3 {
			hori = append(hori, horiSeg{i, true, s.y2, s.x2, s.x3})
		}
	}
	for _, h := range hori {
		if !h.target {
			continue
		}
		a := &adj[h.i]
		for _, o := range hori {
			if h.i != o.i && !(h.x1+1 > o.x2 || h.x2-1 < o.x1) && round(math.Abs(h.y-o.y)) < font.kMinWidthT*font.kAdjustMageStep {
				a.mage += font.kAdjustMageStep - math.Floor(math.Abs(h.y-o.y)/font.kMinWidthT)
				if a.mage > font.kAdjustMageStep {
					a.mage = font.kAdjustMageStep
				}
			}
		}
	}
}

func adjustTate(adj []adjusted) {
	type seg struct {
		i         int
		x, y1, y2 float64
	}
	var vert []seg
	for i := range adj {
		s := &adj[i].stroke
		if (s.a1_100 == 1 || s.a1_100 == 3 || s.a1_100 == 7) && s.a1_opt == 0 && s.x1 == s.x2 {
			vert = append(vert, seg{i, s.x1, s.y1, s.y2})
		}
	}
	for _, v := range vert {
		a := &adj[v.i]
		s := &a.stroke
		for _, o := range vert {
			if v.i != o.i && !(v.y1+1 > o.y2 || v.y2-1 < o.y1) && round(math.Abs(v.x-o.x)) < font.kMinWidthT*font.kAdjustTateStep {
				a.tate += font.kAdjustTateStep - math.Floor(math.Abs(v.x-o.x)/font.kMinWidthT)
				if a.tate > font.kAdjustTateStep || a.tate == font.kAdjustTateStep && (s.a2_opt_1 != 0 || s.a2_100 != 0) {
					a.tate = font.kAdjustTateStep
				}
			}
		}
	}
}

func adjustKakato(adj []adjusted) {
	for i := range adj {
		s := &adj[i].stroke
		if !(s.a1_100 == 1 && s.a1_opt == 0 && (s.a3_100 == 13 || s.a3_100 == 23) && s.a3_opt == 0) {
			continue
		}
		for k := 0; k < font.kAdjustKakatoStep; k++ {
			crossed := false
			for j := range adj {
				if i != j && adj[j].stroke.isCrossBox(
					s.x2-font.kAdjustKakatoRangeX/2,
					s.y2+font.kAdjustKakatoRangeY[k],
					s.x2+font.kAdjustKakatoRangeX/2,
					s.y2+font.kAdjustKakatoRangeY[k+1]) {
					crossed = true
					break
				}
			}
			if crossed ||
				round(s.y2+font.kAdjustKakatoRangeY[k+1]) > 200 || // adjust for baseline
				round(s.y2-s.y1) < font.kAdjustKakatoRangeY[k+1] { // for thin box
				adj[i].kakato = float64(3 - k)
				break
			}
		}
	}
}

func adjustUroko(adj []adjusted) {
	for i := range adj {
		s := &adj[i].stroke
		if !(s.a1_100 == 1 && s.a1_opt == 0 && s.a3_100 == 0 && s.a3_opt == 0) { // no operation for TATE
			continue
		}
		for k := 0; k < font.kAdjustUrokoLengthStep; k++ {
			var cosrad, sinrad float64
			if s.y1 == s.y2 { // YOKO
				cosrad, sinrad = 1, 0
			} else if s.x2-s.x1 < 0 {
				cosrad, sinrad = normalize(s.x1-s.x2, s.y1-s.y2, 1) // for backward compatibility...
			} else {
				cosrad, sinrad = normalize(s.x2-s.x1, s.y2-s.y1, 1)
			}
			tx := s.x2 - font.kAdjustUrokoLine[k]*cosrad - 0.5*sinrad // typo? (sinrad should be -sinrad ?)
			ty := s.y2 - font.kAdjustUrokoLine[k]*sinrad - 0.5*cosrad

			var tlen float64
			if s.y1 == s.y2 { // YOKO
				tlen = s.x2 - s.x1 // should be Math.abs(...)?
			} else {
				tlen = math.Hypot(s.y2-s.y1, s.x2-s.x1)
			}
			crossed := false
			if !(round(tlen) < font.kAdjustUrokoLength[k]) {
				for j := range adj {
					if i != j && adj[j].stroke.isCross(tx, ty, s.x2, s.y2) {
						crossed = true
						break
					}
				}
			}
			if round(tlen) < font.kAdjustUrokoLength[k] || crossed {
				adj[i].uroko = float64(font.kAdjustUrokoLengthStep - k)
				break
			}
		}
	}
}

func adjustUroko2(adj []adjusted) {
	var hori []horiSeg
	for i := range adj {
		a := &adj[i]
		s := &a.stroke
		if s.a1_100 == 1 && s.a1_opt == 0 && s.y1 == s.y2 {
			hori = append(hori, horiSeg{i, s.a3_100 == 0 && s.a3_opt == 0 && a.uroko == 0, s.y1, s.x1, s.x2})
		} else if s.a1_100 == 3 && s.a1_opt == 0 && s.y2 == s.y3 {
			hori = append(hori, horiSeg{i, false, s.y2, s.x2, s.x3})
		}
	}
	for _, h := range hori {
		if !h.target {
			continue
		}
		pressure := 0.0
		for _, o := range hori {
			if h.i != o.i && !(h.x1+1 > o.x2 || h.x2-1 < o.x1) && round(math.Abs(h.y-o.y)) < font.kAdjustUroko2Length {
				pressure += math.Pow(font.kAdjustUroko2Length-math.Abs(h.y-o.y), 1.1)
			}
		}
		adj[h.i].uroko = math.Min(math.Floor(pressure/font.kAdjustUroko2Length), font.kAdjustUroko2Step)
	}
}

func adjustKirikuchi(adj []adjusted) {
	type seg struct{ y, x1, x2 float64 }
	var hori []seg
	for i := range adj {
		s := &adj[i].stroke
		if s.a1_100 == 1 && s.a1_opt == 0 && s.y1 == s.y2 {
			hori = append(hori, seg{s.y1, s.x1, s.x2})
		}
	}
	for i := range adj {
		s := &adj[i].stroke
		if !(s.a1_100 == 2 && s.a1_opt == 0 && s.a2_100 == 32 && s.a2_opt == 0 && s.x1 > s.x2 && s.y1 < s.y2) {
			continue
		}
		for _, h := range hori { // no need to skip when i == j
			if h.x1 < s.x1 && h.x2 > s.x1 && h.y == s.y1 {
				adj[i].kirikuchi = 1
				break
			}
		}
	}
}

// selectPolygonsRect returns the polygons lying entirely inside the rectangle.
func selectPolygonsRect(ps *polygons, x1, y1, x2, y2 float64) []*polygon {
	var out []*polygon
	for _, p := range ps.array {
		inside := true
		for i := range p.pts {
			q := p.get(i)
			if !(x1 <= q.X && q.X <= x2 && y1 <= q.Y && q.Y <= y2) {
				inside = false
				break
			}
		}
		if inside {
			out = append(out, p)
		}
	}
	return out
}

func dfTransform(ps *polygons, x1, y1, x2, y2, a2_100, a3_100, a2_opt, a3_opt float64) {
	switch {
	case a2_100 == 98 && a2_opt == 0:
		for _, p := range selectPolygonsRect(ps, x1, y1, x2, y2) {
			p.reflectX().translate(x1+x2, 0).floor()
		}
	case a2_100 == 97 && a2_opt == 0:
		for _, p := range selectPolygonsRect(ps, x1, y1, x2, y2) {
			p.reflectY().translate(0, y1+y2).floor()
		}
	case a2_100 == 99 && a2_opt == 0:
		switch {
		case a3_100 == 1 && a3_opt == 0:
			for _, p := range selectPolygonsRect(ps, x1, y1, x2, y2) {
				p.rotate90().translate(x1+y2, y1-x1).floor()
			}
		case a3_100 == 2 && a3_opt == 0:
			for _, p := range selectPolygonsRect(ps, x1, y1, x2, y2) {
				p.rotate180().translate(x1+x2, y1+y2).floor()
			}
		case a3_100 == 3 && a3_opt == 0:
			for _, p := range selectPolygonsRect(ps, x1, y1, x2, y2) {
				p.rotate270().translate(x1-y1, y2+x1).floor()
			}
		}
	}
}

func dfDrawFont(ps *polygons, a *adjusted) {
	s := a.stroke
	x1, y1, x2, y2, x3, y3, x4, y4 := s.x1, s.y1, s.x2, s.y2, s.x3, s.y3, s.x4, s.y4
	switch s.a1_100 { // ... no need to divide
	case 0:
		dfTransform(ps, x1, y1, x2, y2, s.a2_100, s.a3_100, s.a2_opt, s.a3_opt)
	case 1:
		if s.a3_100 == 4 {
			var dx1, dy1 float64
			if x1 == x2 && y1 == y2 {
				dx1, dy1 = 0, font.kMage // ?????
			} else {
				dx1, dy1 = normalize(x1-x2, y1-y2, font.kMage)
			}
			tx1 := x2 + dx1
			ty1 := y2 + dy1
			cdDrawLine(ps, x1, y1, tx1, ty1, s.a2_100+s.a2_opt_1*100, 1, a.tate, 0, 0)
			cdDrawCurve(ps,
				tx1, ty1, x2, y2,
				x2-font.kMage*(((font.kAdjustTateStep+4)-a.tate)/(font.kAdjustTateStep+4)), y2,
				1, 14, math.Mod(a.tate, 10), a.hane, math.Floor(a.tate/10), s.a3_opt_2)
		} else {
			cdDrawLine(ps, x1, y1, x2, y2, s.a2_100+s.a2_opt_1*100, s.a3_100, a.tate, a.uroko, a.kakato)
		}
	case 2:
		// case 12: // ... no need
		if s.a3_100 == 4 {
			var dx1, dy1 float64
			if x2 == x3 {
				dx1, dy1 = 0, -font.kMage // ?????
			} else if y2 == y3 {
				dx1, dy1 = -font.kMage, 0 // ?????
			} else {
				dx1, dy1 = normalize(x2-x3, y2-y3, font.kMage)
			}
			tx1 := x3 + dx1
			ty1 := y3 + dy1
			cdDrawCurve(ps, x1, y1, x2, y2, tx1, ty1, s.a2_100+a.kirikuchi*100, 0, s.a2_opt_2, 0, s.a2_opt_3, 0)
			cdDrawCurve(ps, tx1, ty1, x3, y3, x3-font.kMage, y3, 2, 14, s.a2_opt_2, a.hane, 0, s.a3_opt_2)
		} else {
			a3 := s.a3_100
			if s.a3_100 == 5 && s.a3_opt == 0 {
				a3 = 15
			}
			cdDrawCurve(ps, x1, y1, x2, y2, x3, y3, s.a2_100+a.kirikuchi*100, a3, s.a2_opt_2, s.a3_opt_1, s.a2_opt_3, s.a3_opt_2)
		}
	case 3:
		var dx1, dy1 float64
		if x1 == x2 && y1 == y2 {
			dx1, dy1 = 0, font.kMage // ?????
		} else {
			dx1, dy1 = normalize(x1-x2, y1-y2, font.kMage)
		}
		tx1 := x2 + dx1
		ty1 := y2 + dy1
		var dx2, dy2 float64
		if x2 == x3 && y2 == y3 {
			dx2, dy2 = 0, -font.kMage // ?????
		} else {
			dx2, dy2 = normalize(x3-x2, y3-y2, font.kMage)
		}
		tx2 := x2 + dx2
		ty2 := y2 + dy2

		cdDrawLine(ps, x1, y1, tx1, ty1, s.a2_100+s.a2_opt_1*100, 1, a.tate, 0, 0)
		cdDrawCurve(ps, tx1, ty1, x2, y2, tx2, ty2, 1, 1, 0, 0, a.tate, a.mage)

		if !(s.a3_100 == 5 && s.a3_opt_1 == 0 && !((x2 < x3 && x3-tx2 > 0) || (x2 > x3 && tx2-x3 > 0))) { // for closer position
			opt2 := s.a3_opt_1 + a.mage*10
			if s.a3_100 == 5 && s.a3_opt_1 == 0 {
				opt2 = 0
			}
			cdDrawLine(ps, tx2, ty2, x3, y3, 6, s.a3_100, a.mage, opt2, opt2) // bolder by force
		}
	case 12:
		cdDrawCurve(ps, x1, y1, x2, y2, x3, y3, s.a2_100+s.a2_opt_1*100, 1, s.a2_opt_2, 0, s.a2_opt_3, 0)
		cdDrawLine(ps, x3, y3, x4, y4, 6, s.a3_100, 0, s.a3_opt, s.a3_opt)
	case 4:
		rate := math.Hypot(x3-x2, y3-y2) / 120 * 6
		if rate > 6 {
			rate = 6
		}
		var dx1, dy1 float64
		if x1 == x2 && y1 == y2 {
			dx1, dy1 = 0, font.kMage*rate // ?????
		} else {
			dx1, dy1 = normalize(x1-x2, y1-y2, font.kMage*rate)
		}
		tx1 := x2 + dx1
		ty1 := y2 + dy1
		var dx2, dy2 float64
		if x2 == x3 && y2 == y3 {
			dx2, dy2 = 0, -font.kMage*rate // ?????
		} else {
			dx2, dy2 = normalize(x3-x2, y3-y2, font.kMage*rate)
		}
		tx2 := x2 + dx2
		ty2 := y2 + dy2

		cdDrawLine(ps, x1, y1, tx1, ty1, s.a2_100+s.a2_opt_1*100, 1, s.a2_opt_2+s.a2_opt_3*10, 0, 0)
		cdDrawCurve(ps, tx1, ty1, x2, y2, tx2, ty2, 1, 1, 0, 0, 0, 0)

		if !(s.a3_100 == 5 && s.a3_opt == 0 && x3-tx2 <= 0) { // for closer position
			cdDrawLine(ps, tx2, ty2, x3, y3, 6, s.a3_100, 0, s.a3_opt, s.a3_opt) // bolder by force
		}
	case 6:
		if s.a3_100 == 4 {
			var dx1, dy1 float64
			if x3 == x4 {
				dx1, dy1 = 0, -font.kMage // ?????
			} else if y3 == y4 {
				dx1, dy1 = -font.kMage, 0 // ?????
			} else {
				dx1, dy1 = normalize(x3-x4, y3-y4, font.kMage)
			}
			tx1 := x4 + dx1
			ty1 := y4 + dy1
			cdDrawBezier(ps, x1, y1, x2, y2, x3, y3, tx1, ty1, s.a2_100+s.a2_opt_1*100, 1, s.a2_opt_2, 0, s.a2_opt_3, 0)
			cdDrawCurve(ps, tx1, ty1, x4, y4, x4-font.kMage, y4, 1, 14, 0, a.hane, 0, s.a3_opt_2)
		} else {
			a3 := s.a3_100
			if s.a3_100 == 5 && s.a3_opt == 0 {
				a3 = 15
			}
			cdDrawBezier(ps, x1, y1, x2, y2, x3, y3, x4, y4, s.a2_100+s.a2_opt_1*100, a3, s.a2_opt_2, s.a3_opt_1, s.a2_opt_3, s.a3_opt_2)
		}
	case 7:
		cdDrawLine(ps, x1, y1, x2, y2, s.a2_100+s.a2_opt_1*100, 1, a.tate, 0, 0)
		cdDrawCurve(ps, x2, y2, x3, y3, x4, y4, 1, s.a3_100, math.Mod(a.tate, 10), s.a3_opt_1, math.Floor(a.tate/10), s.a3_opt_2)
	}
}
