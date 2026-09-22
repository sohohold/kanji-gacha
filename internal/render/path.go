package render

import (
	"fmt"
	"kanji-gacha/internal/model"
	"math"
	"regexp"
	"strconv"
	"strings"
)

var tokens = regexp.MustCompile(`[MmCcSsLlHhVvQqZz]|[-+]?(?:\d*\.\d+|\d+\.?\d*)(?:[eE][-+]?\d+)?`)

// ParsePath converts KanjiVG paths into absolute cubic Beziers, preserving each stroke.
func ParsePath(d string) (model.Stroke, error) {
	ts := tokens.FindAllString(d, -1)
	left := tokens.ReplaceAllString(d, "")
	if strings.Trim(left, " ,\t\n\r") != "" {
		return nil, fmt.Errorf("unsupported SVG command")
	}
	var out model.Stroke
	var cur, start, prev model.Point
	cmd := ""
	last := ""
	i := 0
	number := func() (float64, error) {
		if i >= len(ts) {
			return 0, fmt.Errorf("missing coordinate")
		}
		v, e := strconv.ParseFloat(ts[i], 64)
		i++
		if e != nil || math.IsInf(v, 0) || math.IsNaN(v) {
			return 0, fmt.Errorf("invalid coordinate")
		}
		return v, nil
	}
	for i < len(ts) {
		if len(ts[i]) == 1 && strings.ContainsAny(ts[i], "MmCcSsLlHhVvQqZz") {
			cmd = ts[i]
			i++
		}
		if cmd == "" {
			return nil, fmt.Errorf("missing command")
		}
		rel := cmd == strings.ToLower(cmd)
		upper := strings.ToUpper(cmd)
		pt := func() (model.Point, error) {
			x, e := number()
			if e != nil {
				return model.Point{}, e
			}
			y, e := number()
			p := model.Point{X: x, Y: y}
			if rel {
				p.X += cur.X
				p.Y += cur.Y
			}
			return p, e
		}
		c := model.Curve{Start: cur}
		switch upper {
		case "M":
			p, e := pt()
			if e != nil {
				return nil, e
			}
			cur = p
			start = p
			if rel {
				cmd = "l"
			} else {
				cmd = "L"
			}
			last = "M"
			continue
		case "C":
			var e error
			c.C1, e = pt()
			if e != nil {
				return nil, e
			}
			c.C2, e = pt()
			if e != nil {
				return nil, e
			}
			c.End, e = pt()
			if e != nil {
				return nil, e
			}
		case "S":
			c.C1 = cur
			if last == "C" || last == "S" {
				c.C1 = model.Point{X: 2*cur.X - prev.X, Y: 2*cur.Y - prev.Y}
			}
			var e error
			c.C2, e = pt()
			if e != nil {
				return nil, e
			}
			c.End, e = pt()
			if e != nil {
				return nil, e
			}
		case "L":
			p, e := pt()
			if e != nil {
				return nil, e
			}
			c.C1 = cur
			c.C2 = p
			c.End = p
		case "H", "V":
			v, e := number()
			if e != nil {
				return nil, e
			}
			p := cur
			if upper == "H" {
				if rel {
					v += cur.X
				}
				p.X = v
			} else {
				if rel {
					v += cur.Y
				}
				p.Y = v
			}
			c.C1 = cur
			c.C2 = p
			c.End = p
		case "Q":
			p, e := pt()
			if e != nil {
				return nil, e
			}
			end, e := pt()
			if e != nil {
				return nil, e
			}
			c.C1 = model.Point{X: cur.X + 2*(p.X-cur.X)/3, Y: cur.Y + 2*(p.Y-cur.Y)/3}
			c.C2 = model.Point{X: end.X + 2*(p.X-end.X)/3, Y: end.Y + 2*(p.Y-end.Y)/3}
			c.End = end
		case "Z":
			c.C1 = cur
			c.C2 = start
			c.End = start
			cmd = ""
		default:
			return nil, fmt.Errorf("unsupported command")
		}
		out = append(out, c)
		cur = c.End
		prev = c.C2
		last = upper
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("empty stroke")
	}
	return out, nil
}
func Bounds(strokes []model.Stroke) model.Box {
	minX, minY, maxX, maxY := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	for _, s := range strokes {
		for _, c := range s {
			for _, p := range []model.Point{c.Start, c.C1, c.C2, c.End} {
				minX = math.Min(minX, p.X)
				minY = math.Min(minY, p.Y)
				maxX = math.Max(maxX, p.X)
				maxY = math.Max(maxY, p.Y)
			}
		}
	}
	return model.Box{X: minX, Y: minY, W: math.Max(maxX-minX, 1), H: math.Max(maxY-minY, 1)}
}
func Fit(strokes []model.Stroke, from, to model.Box) []model.Stroke {
	f := func(p model.Point) model.Point {
		return model.Point{X: to.X + (p.X-from.X)*to.W/from.W, Y: to.Y + (p.Y-from.Y)*to.H/from.H}
	}
	out := make([]model.Stroke, len(strokes))
	for i, s := range strokes {
		out[i] = make(model.Stroke, len(s))
		for j, c := range s {
			out[i][j] = model.Curve{Start: f(c.Start), C1: f(c.C1), C2: f(c.C2), End: f(c.End)}
		}
	}
	return out
}
