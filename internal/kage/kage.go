// Package kage draws KAGE data, the glyph format of GlyphWiki, as Mincho
// (明朝体) outlines.
//
// This file is part of a Go port of the KAGE engine.
// Original JavaScript: KAGE engine by Koichi Kamichi
// <https://github.com/kamichikoichi/kage-engine>.
// Ported from the TypeScript version @kurgm/kage-engine v0.7.0 by kurgm
// <https://github.com/kurgm/kage-engine>.
// Modified 2026-09-24: translated to Go; only the Mincho style with polygon
// output (kUseCurve off) is kept, and component references are resolved
// through a lookup function.
//
// This program is free software: you can redistribute it and/or modify it
// under the terms of the GNU General Public License as published by the Free
// Software Foundation, version 3.
//
// This program is distributed in the hope that it will be useful, but WITHOUT
// ANY WARRANTY; without even the implied warranty of MERCHANTABILITY or
// FITNESS FOR A PARTICULAR PURPOSE. See the GNU General Public License for
// more details. You should have received a copy of the GNU General Public
// License along with this program. If not, see <https://www.gnu.org/licenses/>.
package kage

import (
	"math"
	"regexp"
	"strconv"
	"strings"
)

// maxDepth bounds component nesting. The original recurses without a limit, so
// a reference cycle would overflow its stack; real glyphs nest a few levels.
const maxDepth = 32

// Stroke is one stroke line of KAGE data with its component references
// resolved. Coordinates are in the 200×200 KAGE space, y pointing down.
type Stroke struct {
	a1_100, a1_opt                               float64
	a2_100, a2_opt, a2_opt_1, a2_opt_2, a2_opt_3 float64
	a3_100, a3_opt, a3_opt_1, a3_opt_2           float64
	x1, y1, x2, y2, x3, y3, x4, y4               float64
}

func newStroke(d [11]float64) Stroke {
	s := Stroke{a1_100: d[0], a2_100: d[1], a3_100: d[2], x1: d[3], y1: d[4], x2: d[5], y2: d[6], x3: d[7], y3: d[8], x4: d[9], y4: d[10]}
	s.a1_opt = math.Floor(s.a1_100 / 100)
	s.a1_100 = math.Mod(s.a1_100, 100)

	s.a2_opt = math.Floor(s.a2_100 / 100)
	s.a2_100 = math.Mod(s.a2_100, 100)
	s.a2_opt_1 = math.Mod(s.a2_opt, 10)
	s.a2_opt_2 = math.Mod(math.Floor(s.a2_opt/10), 10)
	s.a2_opt_3 = math.Floor(s.a2_opt / 100)

	s.a3_opt = math.Floor(s.a3_100 / 100)
	s.a3_100 = math.Mod(s.a3_100, 100)
	s.a3_opt_1 = math.Mod(s.a3_opt, 10)
	s.a3_opt_2 = math.Floor(s.a3_opt / 10)
	return s
}

// Type is the stroke type in the first column (1 straight, 2 curve, 3 bend,
// 4 otsu, 6 complex curve, 7 vertical sweep, 0 special), or -1 when that
// column has an option above 100 or is not a number.
func (s Stroke) Type() int {
	if s.a1_opt != 0 || math.IsNaN(s.a1_100) {
		return -1
	}
	return int(s.a1_100)
}

// Points returns the control points the stroke's type uses.
func (s Stroke) Points() [][2]float64 {
	ps := [][2]float64{{s.x1, s.y1}, {s.x2, s.y2}, {s.x3, s.y3}, {s.x4, s.y4}}
	switch s.Type() {
	case 1:
		return ps[:2]
	case 2, 3, 4:
		return ps[:3]
	case 6, 7:
		return ps
	}
	return nil
}

// Box is an axis-aligned rectangle given by its minimum and maximum corners.
type Box struct{ MinX, MinY, MaxX, MaxY float64 }

// getBox is Stroke.getBox of the original, including its treatment of
// unknown types as four-point strokes.
func (s Stroke) getBox() Box {
	b := Box{math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)}
	a1 := s.a1_100
	if s.a1_opt != 0 {
		a1 = 6
	}
	add := func(x, y float64) {
		b.MinX = jsMin(b.MinX, x)
		b.MaxX = jsMax(b.MaxX, x)
		b.MinY = jsMin(b.MinY, y)
		b.MaxY = jsMax(b.MaxY, y)
	}
	switch a1 {
	case 0:
		return b
	case 1, 99:
	case 2, 3, 4:
		add(s.x3, s.y3)
	default:
		add(s.x4, s.y4)
		add(s.x3, s.y3)
	}
	add(s.x1, s.y1)
	add(s.x2, s.y2)
	return b
}

// Bounds is the box around the control points of the strokes, the box the
// original uses to stretch a component. It starts from the empty 200 frame
// (min 200, max 0), as the original does.
func Bounds(strokes []Stroke) Box {
	b := Box{200, 200, 0, 0}
	for _, s := range strokes {
		sb := s.getBox()
		b.MinX = jsMin(b.MinX, sb.MinX)
		b.MaxX = jsMax(b.MaxX, sb.MaxX)
		b.MinY = jsMin(b.MinY, sb.MinY)
		b.MaxY = jsMax(b.MaxY, sb.MaxY)
	}
	return b
}

func (s Stroke) controlSegments() [][4]float64 {
	a1 := s.a1_100
	if s.a1_opt != 0 {
		a1 = 1
	}
	var res [][4]float64
	switch a1 {
	case 0, 8, 9:
		return nil
	case 6, 7:
		res = append(res, [4]float64{s.x3, s.y3, s.x4, s.y4})
		fallthrough
	case 2, 12, 3, 4:
		res = append(res, [4]float64{s.x2, s.y2, s.x3, s.y3})
	}
	res = append(res, [4]float64{s.x1, s.y1, s.x2, s.y2})
	// The original unshifts, so the first segment comes first.
	for i, j := 0, len(res)-1; i < j; i, j = i+1, j-1 {
		res[i], res[j] = res[j], res[i]
	}
	return res
}

func (s Stroke) isCross(bx1, by1, bx2, by2 float64) bool {
	for _, c := range s.controlSegments() {
		if isCross(c[0], c[1], c[2], c[3], bx1, by1, bx2, by2) {
			return true
		}
	}
	return false
}

func (s Stroke) isCrossBox(bx1, by1, bx2, by2 float64) bool {
	for _, c := range s.controlSegments() {
		if isCrossBox(c[0], c[1], c[2], c[3], bx1, by1, bx2, by2) {
			return true
		}
	}
	return false
}

func stretch(dp, sp, p, min, max float64) float64 {
	var p1, p2, p3, p4 float64
	if p < sp+100 {
		p1 = min
		p3 = min
		p2 = sp + 100
		p4 = dp + 100
	} else {
		p1 = sp + 100
		p3 = dp + 100
		p2 = max
		p4 = max
	}
	return math.Floor(((p-p1)/(p2-p1))*(p4-p3) + p3)
}

func (s *Stroke) stretch(sx, sx2, sy, sy2 float64, b Box) {
	s.x1 = stretch(sx, sx2, s.x1, b.MinX, b.MaxX)
	s.y1 = stretch(sy, sy2, s.y1, b.MinY, b.MaxY)
	s.x2 = stretch(sx, sx2, s.x2, b.MinX, b.MaxX)
	s.y2 = stretch(sy, sy2, s.y2, b.MinY, b.MaxY)
	if !(s.a1_100 == 99 && s.a1_opt == 0) { // always true
		s.x3 = stretch(sx, sx2, s.x3, b.MinX, b.MaxX)
		s.y3 = stretch(sy, sy2, s.y3, b.MinY, b.MaxY)
		s.x4 = stretch(sx, sx2, s.x4, b.MinX, b.MaxX)
		s.y4 = stretch(sy, sy2, s.y4, b.MinY, b.MaxY)
	}
}

// Expand parses KAGE data (lines joined by "$") into strokes, replacing each
// component reference (99) by the strokes of the named glyph mapped into its
// rectangle. lookup returns the data of a glyph, or "" when there is none; a
// missing component draws nothing, as in the original.
func Expand(data string, lookup func(name string) string) []Stroke {
	if data == "" {
		return nil
	}
	return expand(data, lookup, 0)
}

func expand(data string, lookup func(string) string, depth int) []Stroke {
	var out []Stroke
	for _, line := range strings.Split(data, "$") {
		cols := strings.Split(line, ":")
		col := func(i int) float64 {
			if i >= len(cols) {
				return math.NaN() // +undefined
			}
			return math.Floor(jsNumber(cols[i]))
		}
		if col(0) != 99 {
			var d [11]float64
			for i := range d {
				d[i] = col(i)
			}
			out = append(out, newStroke(d))
			continue
		}
		name := "undefined"
		if len(cols) > 7 {
			name = cols[7]
		}
		if depth >= maxDepth {
			continue
		}
		buhin := lookup(name)
		if buhin == "" {
			continue
		}
		out = append(out, expandBuhin(buhin, lookup, depth+1, col(3), col(4), col(5), col(6), col(1), col(2), col(9), col(10))...)
	}
	return out
}

func expandBuhin(buhin string, lookup func(string) string, depth int, x1, y1, x2, y2, sx, sy, sx2, sy2 float64) []Stroke {
	strokes := expand(buhin, lookup, depth)
	box := Bounds(strokes)
	if sx != 0 || sy != 0 {
		if sx > 100 {
			sx -= 200
		} else {
			sx2 = 0
			sy2 = 0
		}
	}
	for i := range strokes {
		s := &strokes[i]
		if sx != 0 || sy != 0 {
			s.stretch(sx, sx2, sy, sy2, box)
		}
		s.x1 = x1 + s.x1*(x2-x1)/200
		s.y1 = y1 + s.y1*(y2-y1)/200
		s.x2 = x1 + s.x2*(x2-x1)/200
		s.y2 = y1 + s.y2*(y2-y1)/200
		s.x3 = x1 + s.x3*(x2-x1)/200
		s.y3 = y1 + s.y3*(y2-y1)/200
		s.x4 = x1 + s.x4*(x2-x1)/200
		s.y4 = y1 + s.y4*(y2-y1)/200
	}
	return strokes
}

var decimal = regexp.MustCompile(`^[+-]?(?:\d+\.?\d*|\.\d+)(?:[eE][+-]?\d+)?$`)

// jsNumber converts a column as JavaScript's unary + does: surrounding
// whitespace is ignored, an empty string is 0, and anything else that is not
// a number is NaN.
func jsNumber(s string) float64 {
	t := strings.TrimSpace(s)
	if t == "" {
		return 0
	}
	if len(t) > 2 && t[0] == '0' {
		base := 0
		switch t[1] {
		case 'x', 'X':
			base = 16
		case 'o', 'O':
			base = 8
		case 'b', 'B':
			base = 2
		}
		if base != 0 {
			n, e := strconv.ParseUint(t[2:], base, 64)
			if e != nil || strings.Contains(t, "_") {
				return math.NaN()
			}
			return float64(n)
		}
	}
	switch t {
	case "Infinity", "+Infinity":
		return math.Inf(1)
	case "-Infinity":
		return math.Inf(-1)
	}
	if !decimal.MatchString(t) {
		return math.NaN()
	}
	v, e := strconv.ParseFloat(t, 64)
	if e != nil && !math.IsInf(v, 0) {
		return math.NaN()
	}
	return v
}

// jsMin and jsMax are Math.min and Math.max for two arguments: NaN wins.
func jsMin(a, b float64) float64 {
	if math.IsNaN(a) || math.IsNaN(b) {
		return math.NaN()
	}
	return math.Min(a, b)
}
func jsMax(a, b float64) float64 {
	if math.IsNaN(a) || math.IsNaN(b) {
		return math.NaN()
	}
	return math.Max(a, b)
}
