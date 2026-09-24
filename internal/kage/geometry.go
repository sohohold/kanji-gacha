// This file is part of a Go port of the KAGE engine (GPL v3); see kage.go for
// the origin, the changes made and the license.

package kage

import "math"

// Point is a vertex of an outline in the 200×200 KAGE space.
type Point struct{ X, Y float64 }

// Polygon is one closed outline. Its winding is as the engine drew it; see
// NormalizeWinding.
type Polygon []Point

// precision is the fixed-point scale of the original's Polygon: points are
// stored multiplied by it and floored when the polygon is added to a glyph,
// so outlines land on a 0.1 grid.
const precision = 10

// polygon is the original's Polygon with its internal fixed-point storage.
type polygon struct{ pts []Point }

func newPolygon(n int) *polygon { return &polygon{pts: make([]Point, n)} }

func polygonOf(ps ...Point) *polygon {
	p := &polygon{}
	for _, q := range ps {
		p.push(q.X, q.Y)
	}
	return p
}

func (p *polygon) push(x, y float64)       { p.pts = append(p.pts, Point{x * precision, y * precision}) }
func (p *polygon) set(i int, x, y float64) { p.pts[i] = Point{x * precision, y * precision} }
func (p *polygon) setPoint(i int, q Point) { p.set(i, q.X, q.Y) }
func (p *polygon) get(i int) Point {
	return Point{p.pts[i].X / precision, p.pts[i].Y / precision}
}
func (p *polygon) reverse() {
	for i, j := 0, len(p.pts)-1; i < j; i, j = i+1, j-1 {
		p.pts[i], p.pts[j] = p.pts[j], p.pts[i]
	}
}
func (p *polygon) concat(o *polygon) { p.pts = append(p.pts, o.pts...) }
func (p *polygon) shift() {
	if len(p.pts) > 0 {
		p.pts = p.pts[1:]
	}
}
func (p *polygon) unshift(x, y float64) {
	p.pts = append([]Point{{x * precision, y * precision}}, p.pts...)
}
func (p *polygon) translate(dx, dy float64) *polygon {
	dx *= precision
	dy *= precision
	for i := range p.pts {
		p.pts[i].X += dx
		p.pts[i].Y += dy
	}
	return p
}
func (p *polygon) reflectX() *polygon {
	for i := range p.pts {
		p.pts[i].X *= -1
	}
	return p
}
func (p *polygon) reflectY() *polygon {
	for i := range p.pts {
		p.pts[i].Y *= -1
	}
	return p
}
func (p *polygon) rotate90() *polygon {
	for i, q := range p.pts {
		p.pts[i] = Point{-q.Y, q.X}
	}
	return p
}
func (p *polygon) rotate180() *polygon {
	for i := range p.pts {
		p.pts[i].X *= -1
		p.pts[i].Y *= -1
	}
	return p
}
func (p *polygon) rotate270() *polygon {
	for i, q := range p.pts {
		p.pts[i] = Point{q.Y, -q.X}
	}
	return p
}
func (p *polygon) floor() *polygon {
	for i, q := range p.pts {
		p.pts[i] = Point{math.Floor(q.X), math.Floor(q.Y)}
	}
	return p
}
func (p *polygon) out() Polygon {
	out := make(Polygon, len(p.pts))
	for i := range p.pts {
		out[i] = p.get(i)
	}
	return out
}

// polygons is the original's Polygons: the outlines drawn so far.
type polygons struct{ array []*polygon }

// push adds a polygon unless it has fewer than three points, a NaN, or no
// extent. The extent check starts from min 200 and max 0, as the original's.
func (ps *polygons) push(p *polygon) {
	if len(p.pts) < 3 {
		return
	}
	p.floor()
	minx, maxx, miny, maxy := 200.0, 0.0, 200.0, 0.0
	for i := range p.pts {
		q := p.get(i)
		if q.X < minx {
			minx = q.X
		}
		if q.X > maxx {
			maxx = q.X
		}
		if q.Y < miny {
			miny = q.Y
		}
		if q.Y > maxy {
			maxy = q.Y
		}
		if math.IsNaN(q.X) || math.IsNaN(q.Y) {
			return
		}
	}
	if minx != maxx && miny != maxy {
		ps.array = append(ps.array, p)
	}
}

// signedArea is twice the signed area of the polygon, positive when it runs
// clockwise in the y-down KAGE space.
func signedArea(p Polygon) float64 {
	s := 0.0
	for i, a := range p {
		b := p[(i+1)%len(p)]
		s += (b.X - a.X) * (b.Y + a.Y)
	}
	return s
}

// NormalizeWinding reverses the polygons that do not run clockwise (in the
// y-down space), so overlapping outlines fill as one shape under the nonzero
// rule, the default for SVG paths. It is Polygons.normalizeWinding("cw").
func NormalizeWinding(ps []Polygon) {
	for _, p := range ps {
		if len(p) < 3 {
			continue
		}
		if signedArea(p) < 0 {
			for i, j := 0, len(p)-1; i < j; i, j = i+1, j-1 {
				p[i], p[j] = p[j], p[i]
			}
		}
	}
}

// pen converts coordinates local to a pen position and direction into
// global ones.
type pen struct{ x, y, cos, sin float64 }

func newPen(x, y float64) *pen { return &pen{x: x, y: y, cos: 1} }

func (p *pen) setMatrix2(cos, sin float64) *pen {
	p.cos, p.sin = cos, sin
	return p
}
func (p *pen) setLeft(ox, oy float64) *pen {
	dx, dy := normalize(ox-p.x, oy-p.y, 1)
	return p.setMatrix2(-dx, -dy)
}
func (p *pen) setRight(ox, oy float64) *pen {
	dx, dy := normalize(ox-p.x, oy-p.y, 1)
	return p.setMatrix2(dx, dy)
}
func (p *pen) setUp(ox, oy float64) *pen {
	dx, dy := normalize(ox-p.x, oy-p.y, 1)
	return p.setMatrix2(-dy, dx)
}
func (p *pen) setDown(ox, oy float64) *pen {
	dx, dy := normalize(ox-p.x, oy-p.y, 1)
	return p.setMatrix2(dy, -dx)
}
func (p *pen) getPoint(lx, ly float64) Point {
	return Point{p.x + p.cos*lx + -p.sin*ly, p.y + p.sin*lx + p.cos*ly}
}
func (p *pen) getPolygon(local ...Point) *polygon {
	out := &polygon{}
	for _, q := range local {
		g := p.getPoint(q.X, q.Y)
		out.push(g.X, g.Y)
	}
	return out
}

// normalize returns the vector (x, y) scaled to the magnitude; the zero
// vector points along +x or -x by the sign of x, as Math.atan2 would.
func normalize(x, y, magnitude float64) (float64, float64) {
	if x == 0 && y == 0 {
		if math.Signbit(x) {
			return -magnitude, 0
		}
		return magnitude, 0
	}
	k := magnitude / math.Hypot(x, y)
	return x * k, y * k
}

func quadraticBezier(p1, p2, p3, t float64) float64 {
	s := 1 - t
	return (s*s)*p1 + 2*(s*t)*p2 + (t*t)*p3
}
func quadraticBezierDeriv(p1, p2, p3, t float64) float64 {
	return 2 * (t*(p1-2*p2+p3) - p1 + p2)
}
func cubicBezier(p1, p2, p3, p4, t float64) float64 {
	s := 1 - t
	return (s*s*s)*p1 + 3*(s*s*t)*p2 + 3*(s*t*t)*p3 + (t*t*t)*p4
}
func cubicBezierDeriv(p1, p2, p3, p4, t float64) float64 {
	return 3 * (t*(t*(-p1+3*p2-3*p3+p4)+2*(p1-2*p2+p3)) - p1 + p2)
}

// jsRound is Math.round: halves round towards +Infinity.
func jsRound(v float64) float64 {
	f := math.Floor(v)
	if v-f >= 0.5 {
		return f + 1
	}
	return f
}

// round is the original's round(v, rate) with its default rate 1e8.
func round(v float64) float64 { return roundRate(v, 1e8) }
func roundRate(v, rate float64) float64 {
	return jsRound(v*rate) / rate
}

func cross(x1, y1, x2, y2 float64) float64 { return x1*y2 - x2*y1 }

func isCross(x11, y11, x12, y12, x21, y21, x22, y22 float64) bool {
	c := cross(x12-x11, y12-y11, x22-x21, y22-y21)
	if math.IsNaN(c) {
		return true // for backward compatibility...
	}
	if c == 0 {
		return false // parallel
	}
	c1 := cross(x12-x11, y12-y11, x21-x11, y21-y11)
	c2 := cross(x12-x11, y12-y11, x22-x11, y22-y11)
	c3 := cross(x22-x21, y22-y21, x11-x21, y11-y21)
	c4 := cross(x22-x21, y22-y21, x12-x21, y12-y21)
	return roundRate(c1*c2, 1e5) <= 0 && roundRate(c3*c4, 1e5) <= 0
}

func isCrossBox(x1, y1, x2, y2, bx1, by1, bx2, by2 float64) bool {
	return isCross(x1, y1, x2, y2, bx1, by1, bx2, by1) ||
		isCross(x1, y1, x2, y2, bx2, by1, bx2, by2) ||
		isCross(x1, y1, x2, y2, bx1, by2, bx2, by2) ||
		isCross(x1, y1, x2, y2, bx1, by1, bx1, by2)
}

// fattenCurve samples both sides of a quadratic (sx1,sy1 == sx2,sy2) or cubic
// curve, offset by widthFunc(t), every rate/1000 of t.
func fattenCurve(x1, y1, sx1, sy1, sx2, sy2, x2, y2, rate float64, widthFunc func(t float64) float64) (left, right [][2]float64) {
	quadratic := sx1 == sx2 && sy1 == sy2
	for tt := 0.0; tt <= 1000; tt += rate {
		t := tt / 1000
		var x, y, ix, iy float64
		if quadratic {
			x = quadraticBezier(x1, sx1, x2, t)
			y = quadraticBezier(y1, sy1, y2, t)
			ix = quadraticBezierDeriv(x1, sx1, x2, t)
			iy = quadraticBezierDeriv(y1, sy1, y2, t)
		} else {
			x = cubicBezier(x1, sx1, sx2, x2, t)
			y = cubicBezier(y1, sy1, sy2, y2, t)
			ix = cubicBezierDeriv(x1, sx1, sx2, x2, t)
			iy = cubicBezierDeriv(y1, sy1, sy2, y2, t)
		}
		width := widthFunc(t)
		var ia, ib float64
		if round(ix) == 0 && round(iy) == 0 {
			ia, ib = -width, 0
		} else {
			ia, ib = normalize(-iy, ix, width)
		}
		left = append(left, [2]float64{x - ia, y - ib})
		right = append(right, [2]float64{x + ia, y + ib})
	}
	return left, right
}
