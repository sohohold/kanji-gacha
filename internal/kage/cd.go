// This file is part of a Go port of the KAGE engine (GPL v3); see kage.go for
// the origin, the changes made and the license.

package kage

import "math"

// undefined stands for the original's undefined coordinates, which it sets
// when a head or tail shape is unknown and then skips drawing with them.
var undefined = math.NaN()

func cdDrawCurveU(ps *polygons,
	x1_, y1_, sx1, sy1, sx2, sy2, x2_, y2_ float64,
	a1, a2 float64,
	opt1, haneAdjustment, opt3, opt4 float64) {

	kMinWidthT := font.kMinWidthT - opt1/2

	x1, y1 := x1_, y1_
	delta1 := undefined
	switch math.Mod(a1, 100) {
	case 0, 7, 27:
		delta1 = -1 * font.kMinWidthY * 0.5
	case 1, 2, 6, 22, 32: // 2 must be 32; 32 changed
		delta1 = 0
	case 12:
		delta1 = font.kMinWidthY
	}
	has1 := !math.IsNaN(delta1)
	if !has1 {
		x1, y1 = undefined, undefined // ????? (was NaN in original code)
	} else if delta1 != 0 {
		var dx1, dy1 float64
		if x1 == sx1 && y1 == sy1 {
			dx1, dy1 = 0, delta1 // ?????
		} else {
			dx1, dy1 = normalize(x1-sx1, y1-sy1, delta1)
		}
		x1 += dx1
		y1 += dy1
	}

	cornerOffset := 0.0
	if has1 && (a1 == 22 || a1 == 27) && a2 == 7 && kMinWidthT > 6 {
		contourLength := math.Hypot(sx1-x1, sy1-y1) + math.Hypot(sx2-sx1, sy2-sy1) + math.Hypot(x2_-sx2, y2_-sy2)
		if contourLength < 100 {
			cornerOffset = (kMinWidthT - 6) * ((100 - contourLength) / 100)
			x1 += cornerOffset
		}
	}

	x2, y2 := x2_, y2_
	var delta2 float64
	switch math.Mod(a2, 100) {
	case 0, 1, 7, 9, 15, 14, 17, 5: // 15 can change to 15->5, 14 to 14->4; 17 no need
		delta2 = 0
	case 8: // get shorten for tail's circle
		delta2 = -1 * kMinWidthT * 0.5
	default:
		delta2 = delta1 // ?????
	}
	has2 := !math.IsNaN(delta2)
	if !has2 {
		x2, y2 = undefined, undefined // ????? (was NaN in original code)
	} else if delta2 != 0 {
		var dx2, dy2 float64
		if sx2 == x2 && sy2 == y2 {
			dx2, dy2 = 0, -delta2 // ?????
		} else {
			dx2, dy2 = normalize(x2-sx2, y2-sy2, delta2)
		}
		x2 += dx2
		y2 += dy2
	}

	if has1 && has2 {
		drawCurveBody(ps, x1, y1, sx1, sy1, sx2, sy2, x2, y2, a1, a2, kMinWidthT, opt3, opt4)
	}
	if has1 {
		isUpToBottom := has2 && y1 <= y2
		drawCurveHead(ps, x1, y1, sx1, sy1, a1, kMinWidthT, isUpToBottom, cornerOffset)
	}
	if has2 {
		isBottomToUp := has1 && y2 <= y1
		drawCurveTail(ps, sx2, sy2, x2, y2, a1, a2, kMinWidthT, haneAdjustment, opt4, isBottomToUp)
	}
}

func drawCurveBody(ps *polygons,
	x1, y1, sx1, sy1, sx2, sy2, x2, y2 float64,
	a1, a2 float64,
	kMinWidthT, opt3, opt4 float64) {
	isQuadratic := sx1 == sx2 && sy1 == sy2

	hosomi := 0.5
	if math.Hypot(x2-x1, y2-y1) < 50 {
		hosomi += 0.4 * (1 - math.Hypot(x2-x1, y2-y1)/50)
	}

	var deltadFunc func(t float64) float64
	switch {
	case (a1 == 7 || a1 == 27) && a2 == 0: // L2RD: fatten
		deltadFunc = func(t float64) float64 { return math.Pow(t, hosomi) * font.kL2RDfatten }
	case a1 == 7 || a1 == 27:
		if isQuadratic { // ?????
			deltadFunc = func(t float64) float64 { return math.Pow(t, hosomi) }
		} else {
			deltadFunc = func(t float64) float64 { return math.Pow(math.Pow(t, hosomi), 0.7) } // make fatten
		}
	case a2 == 7:
		deltadFunc = func(t float64) float64 { return math.Pow(1-t, hosomi) }
	case isQuadratic && (opt3 > 0 || opt4 > 0): // ?????
		deltadFunc = func(t float64) float64 {
			return ((font.kMinWidthT - opt3/2) - (opt4-opt3)/2*t) / font.kMinWidthT
		}
	default:
		deltadFunc = func(float64) float64 { return 1 }
	}

	left, right := fattenCurve(x1, y1, sx1, sy1, sx2, sy2, x2, y2, font.kRate, func(t float64) float64 {
		deltad := deltadFunc(t)
		if deltad < 0.15 {
			deltad = 0.15
		}
		return kMinWidthT * deltad
	})

	poly := &polygon{}
	poly2 := &polygon{}
	// copy to polygon structure
	for _, p := range left {
		poly.push(p[0], p[1])
	}
	for _, p := range right {
		poly2.push(p[0], p[1])
	}

	// suiheisen ni setsuzoku
	horizontalStart := false
	if isQuadratic {
		horizontalStart = y1 > y2
	} else {
		horizontalStart = x1 > sx1
	}
	if a1 == 132 || a1 == 22 && horizontalStart { // ?????
		poly.floor()
		poly2.floor()
		for index, length := 0, len(poly2.pts); index+1 < length; index++ {
			point1 := poly2.get(index)
			point2 := poly2.get(index + 1)
			if point1.Y <= y1 && y1 <= point2.Y {
				newx1 := point2.X + (point1.X-point2.X)*(y1-point2.Y)/(point1.Y-point2.Y)
				newy1 := y1
				point3 := poly.get(0)
				point4 := poly.get(1)
				var newx2, newy2 float64
				if a1 == 132 { // ?????
					newx2 = point3.X + (point4.X-point3.X)*(y1-point3.Y)/(point4.Y-point3.Y)
					newy2 = y1
				} else {
					newx2 = point3.X + (point4.X-point3.X+1)*(y1-point3.Y)/(point4.Y-point3.Y) // "+ 1"?????
					newy2 = y1 + 1                                                             // "+ 1"?????
				}
				for i := 0; i < index; i++ {
					poly2.shift()
				}
				poly2.set(0, newx1, newy1)
				poly.unshift(newx2, newy2)
				break
			}
		}
	}

	poly2.reverse()
	poly.concat(poly2)
	ps.push(poly)
}

func drawCurveHead(ps *polygons,
	x1, y1, sx1, sy1 float64,
	a1, kMinWidthT float64, isUpToBottom bool, cornerOffset float64) {
	// process for head of stroke
	switch a1 {
	case 12:
		pen1 := newPen(x1, y1)
		if x1 != sx1 { // ?????
			pen1.setDown(sx1, sy1)
		}
		ps.push(pen1.getPolygon(
			Point{-kMinWidthT, 0},
			Point{+kMinWidthT, 0},
			Point{-kMinWidthT, -kMinWidthT},
		))
	case 0:
		if isUpToBottom { // from up to bottom
			pen1 := newPen(x1, y1)
			if x1 != sx1 { // ?????
				pen1.setDown(sx1, sy1)
			}
			typ := math.Atan2(math.Abs(y1-sy1), math.Abs(x1-sx1))/math.Pi*2 - 0.4
			if typ > 0 {
				typ *= 2
			} else {
				typ *= 16
			}
			pm := 1.0
			if typ < 0 {
				pm = -1
			}
			ps.push(pen1.getPolygon(
				Point{-kMinWidthT, 1}, // 1 ???
				Point{+kMinWidthT, 0},
				Point{-pm * kMinWidthT, -font.kMinWidthY * math.Abs(typ)},
			))
			// beginning of the stroke
			move := 0.0
			if typ < 0 {
				move = -typ * font.kMinWidthY
			}
			if x1 == sx1 && y1 == sy1 { // ?????
				ps.push(pen1.getPolygon(
					Point{kMinWidthT, -move},
					Point{kMinWidthT * 1.5, font.kMinWidthY - move},
					Point{kMinWidthT - 2, font.kMinWidthY*2 + 1},
				))
			} else {
				ps.push(pen1.getPolygon(
					Point{kMinWidthT, -move},
					Point{kMinWidthT * 1.5, font.kMinWidthY - move*1.2},
					Point{kMinWidthT - 2, font.kMinWidthY*2 - move*0.8 + 1},
				))
			}
		} else { // bottom to up
			pen1 := newPen(x1, y1)
			if x1 == sx1 {
				pen1.setMatrix2(0, 1) // ?????
			} else {
				pen1.setRight(sx1, sy1)
			}
			ps.push(pen1.getPolygon(
				Point{0, +kMinWidthT},
				Point{0, -kMinWidthT},
				Point{-font.kMinWidthY, -kMinWidthT},
			))
			// beginning of the stroke
			ps.push(pen1.getPolygon(
				Point{0, +kMinWidthT},
				Point{+font.kMinWidthY, +kMinWidthT * 1.5},
				Point{+font.kMinWidthY * 3, +kMinWidthT * 0.5},
			))
		}
	case 22, 27: // box's up-right corner, any time same degree
		pts := []Point{
			{-kMinWidthT, -font.kMinWidthY},
			{0, -font.kMinWidthY - font.kWidth},
			{+kMinWidthT + font.kWidth, +font.kMinWidthY},
			{+kMinWidthT, +kMinWidthT - 1},
		}
		if a1 == 27 {
			pts = append(pts, Point{0, +kMinWidthT + 2}, Point{0, 0.5})
		} else {
			pts = append(pts, Point{-kMinWidthT, +kMinWidthT + 4})
		}
		ps.push(newPen(x1-cornerOffset, y1).getPolygon(pts...))
	}
}

func drawCurveTail(ps *polygons,
	sx2, sy2, x2, y2 float64,
	a1, a2 float64,
	kMinWidthT, haneAdjustment, opt4 float64, isBottomToUp bool) {
	// process for tail
	switch a2 {
	case 1, 8, 15: // the last filled circle ... it can change 15->5
		kMinWidthT2 := font.kMinWidthT - opt4/2
		pen2 := newPen(x2, y2)
		if sx2 == x2 {
			pen2.setMatrix2(0, 1) // ?????
		} else if sy2 != y2 { // ?????
			pen2.setLeft(sx2, sy2)
		}
		poly := pen2.getPolygon(
			Point{0, -kMinWidthT2},
			Point{+kMinWidthT2 * 0.7, -kMinWidthT2 * 0.7},
			Point{+kMinWidthT2, 0},
			Point{+kMinWidthT2 * 0.7, +kMinWidthT2 * 0.7},
			Point{0, +kMinWidthT2},
		)
		if sx2 == x2 {
			poly.reverse()
		}
		ps.push(poly)

		if a2 == 15 { // jump up ... it can change 15->5
			// anytime same degree
			pen2r := newPen(x2, y2)
			if isBottomToUp {
				pen2r.setMatrix2(-1, 0)
			}
			ps.push(pen2r.getPolygon(
				Point{0, -kMinWidthT + 1},
				Point{+2, -kMinWidthT - font.kWidth*5},
				Point{0, -kMinWidthT - font.kWidth*5},
				Point{-kMinWidthT, -kMinWidthT + 1},
			))
		}
	case 0, 9: // Math.sinnyu & L2RD Harai ... no need for a2=9
		if a2 == 0 && !(a1 == 7 || a1 == 27) {
			break
		}
		typ := math.Atan2(math.Abs(y2-sy2), math.Abs(x2-sx2))/math.Pi*2 - 0.6
		if typ > 0 {
			typ *= 8
		} else {
			typ *= 3
		}
		pm := 1.0
		if typ < 0 {
			pm = -1
		}
		pen2 := newPen(x2, y2)
		if sy2 == y2 {
			pen2.setMatrix2(1, 0) // ?????
		} else if sx2 == x2 {
			if y2 > sy2 { // for backward compatibility...
				pen2.setMatrix2(0, -1)
			} else {
				pen2.setMatrix2(0, 1)
			}
		} else {
			pen2.setLeft(sx2, sy2)
		}
		ps.push(pen2.getPolygon(
			Point{0, +kMinWidthT * font.kL2RDfatten},
			Point{0, -kMinWidthT * font.kL2RDfatten},
			Point{math.Abs(typ) * kMinWidthT * font.kL2RDfatten, pm * kMinWidthT * font.kL2RDfatten},
		))
	case 14: // jump to left, allways go left
		jumpFactor := 1.0
		if kMinWidthT > 6 {
			jumpFactor = 6.0 / kMinWidthT
		}
		haneLength := font.kWidth * 4 * math.Min(1-haneAdjustment/10, math.Pow(kMinWidthT/font.kMinWidthT, 3)) * jumpFactor
		ps.push(newPen(x2, y2).getPolygon(
			Point{0, 0},
			Point{0, -kMinWidthT},
			Point{-haneLength, -kMinWidthT},
			Point{-haneLength, -kMinWidthT * 0.5},
		))
	}
}

func cdDrawBezier(ps *polygons,
	x1, y1, x2, y2, x3, y3, x4, y4 float64,
	a1, a2 float64,
	opt1, haneAdjustment, opt3, opt4 float64) {
	cdDrawCurveU(ps, x1, y1, x2, y2, x3, y3, x4, y4, a1, a2, opt1, haneAdjustment, opt3, opt4)
}

func cdDrawCurve(ps *polygons,
	x1, y1, x2, y2, x3, y3 float64,
	a1, a2 float64,
	opt1, haneAdjustment, opt3, opt4 float64) {
	cdDrawCurveU(ps, x1, y1, x2, y2, x2, y2, x3, y3, a1, a2, opt1, haneAdjustment, opt3, opt4)
}

func cdDrawLine(ps *polygons,
	x1, y1, x2, y2 float64,
	a1, a2 float64, opt1, urokoAdjustment, kakatoAdjustment float64) {

	kMinWidthT := font.kMinWidthT - opt1/2

	if x1 == x2 || y1 != y2 && (x1 > x2 || math.Abs(y2-y1) >= math.Abs(x2-x1) || a1 == 6 || a2 == 6) {
		// if TATE stroke, use y-axis
		// for others, use x-axis
		// KAKUDO GA FUKAI or KAGI NO YOKO BOU
		var cosrad, sinrad float64
		if x1 == x2 {
			cosrad, sinrad = 0, 1 // ?????
		} else {
			cosrad, sinrad = normalize(x2-x1, y2-y1, 1)
		}

		pen1 := newPen(x1, y1)
		pen2 := newPen(x2, y2)
		pen1.setMatrix2(sinrad, -cosrad)
		pen2.setMatrix2(sinrad, -cosrad)

		poly0 := newPolygon(4)
		switch a1 {
		case 0:
			poly0.setPoint(0, pen1.getPoint(kMinWidthT, font.kMinWidthY/2))
			poly0.setPoint(3, pen1.getPoint(-kMinWidthT, -font.kMinWidthY/2))
		case 1, 6: // 6 ... no need
			poly0.setPoint(0, pen1.getPoint(kMinWidthT, 0))
			poly0.setPoint(3, pen1.getPoint(-kMinWidthT, 0))
		case 12:
			poly0.setPoint(0, pen1.getPoint(kMinWidthT, -font.kMinWidthY))
			poly0.setPoint(3, pen1.getPoint(-kMinWidthT, -font.kMinWidthY-kMinWidthT))
		case 22:
			if x1 == x2 {
				poly0.set(0, x1+kMinWidthT, y1)
				poly0.set(3, x1-kMinWidthT, y1)
			} else {
				v := 1.0
				if x1 > x2 {
					v = -1
				}
				// TODO: why " + v", " + 1" ???
				poly0.set(0, x1+(kMinWidthT+v)/sinrad, y1+1)
				poly0.set(3, x1-kMinWidthT/sinrad, y1)
			}
		case 32:
			if x1 == x2 {
				poly0.set(0, x1+kMinWidthT, y1-font.kMinWidthY)
				poly0.set(3, x1-kMinWidthT, y1-font.kMinWidthY)
			} else {
				poly0.set(0, x1+kMinWidthT/sinrad, y1)
				poly0.set(3, x1-kMinWidthT/sinrad, y1)
			}
		}

		switch a2 {
		case 0:
			if a1 == 6 { // KAGI's tail ... no need
				poly0.setPoint(1, pen2.getPoint(kMinWidthT, 0))
				poly0.setPoint(2, pen2.getPoint(-kMinWidthT, 0))
			} else {
				poly0.setPoint(1, pen2.getPoint(kMinWidthT, -kMinWidthT/2))
				poly0.setPoint(2, pen2.getPoint(-kMinWidthT, kMinWidthT/2))
			}
		case 5, 1: // 1 is needed?
			if a2 == 5 && x1 == x2 {
				break
			}
			poly0.setPoint(1, pen2.getPoint(kMinWidthT, 0))
			poly0.setPoint(2, pen2.getPoint(-kMinWidthT, 0))
		case 13:
			poly0.setPoint(1, pen2.getPoint(kMinWidthT, kakatoAt(font.kAdjustKakatoL, kakatoAdjustment)))
			poly0.setPoint(2, pen2.getPoint(-kMinWidthT, kakatoAt(font.kAdjustKakatoL, kakatoAdjustment)+kMinWidthT))
		case 23:
			poly0.setPoint(1, pen2.getPoint(kMinWidthT, kakatoAt(font.kAdjustKakatoR, kakatoAdjustment)))
			poly0.setPoint(2, pen2.getPoint(-kMinWidthT, kakatoAt(font.kAdjustKakatoR, kakatoAdjustment)+kMinWidthT))
		case 24, 32: // 24 for T/H design
			if x1 == x2 {
				poly0.set(1, x2+kMinWidthT, y2+font.kMinWidthY)
				poly0.set(2, x2-kMinWidthT, y2+font.kMinWidthY)
			} else {
				poly0.set(1, x2+kMinWidthT/sinrad, y2)
				poly0.set(2, x2-kMinWidthT/sinrad, y2)
			}
		}

		ps.push(poly0)

		switch a2 {
		case 24: // for T design
			p1 := Point{+kMinWidthT * 0.5, -font.kMinWidthY * 4}
			if x1 == x2 { // ?????
				p1 = Point{+kMinWidthT, -font.kMinWidthY * 3}
			}
			ps.push(newPen(x2, y2).getPolygon(
				Point{0, +font.kMinWidthY},
				p1,
				Point{+kMinWidthT * 2, -font.kMinWidthY},
				Point{+kMinWidthT * 2, +font.kMinWidthY},
			))
		case 13:
			if kakatoAdjustment == 4 { // for new GTH box's left bottom corner
				if x1 == x2 {
					ps.push(newPen(x2, y2).getPolygon(
						Point{-kMinWidthT, -font.kMinWidthY * 3},
						Point{-kMinWidthT * 2, 0},
						Point{-font.kMinWidthY, +font.kMinWidthY * 5},
						Point{+kMinWidthT, +font.kMinWidthY},
					))
				} else { // MUKI KANKEINASHI
					m := 0.0
					if x1 > x2 && y1 != y2 {
						m = math.Floor((x1 - x2) / (y2 - y1) * 3)
					}
					ps.push(newPen(x2+m, y2).getPolygon(
						Point{0, -font.kMinWidthY * 5},
						Point{-kMinWidthT * 2, 0},
						Point{-font.kMinWidthY, +font.kMinWidthY * 5},
						Point{+kMinWidthT, +font.kMinWidthY},
						Point{0, 0},
					))
				}
			}
		}

		switch a1 {
		case 22, 27:
			// box's right top corner
			// SHIKAKU MIGIUE UROKO NANAME DEMO MASSUGU MUKI
			pts := []Point{
				{-kMinWidthT, -font.kMinWidthY},
				{0, -font.kMinWidthY - font.kWidth},
				{+kMinWidthT + font.kWidth, +font.kMinWidthY},
			}
			switch {
			case x1 == x2:
				pts = append(pts, Point{+kMinWidthT, +kMinWidthT}, Point{-kMinWidthT, 0})
			case a1 == 27:
				pts = append(pts, Point{+kMinWidthT, +kMinWidthT - 1}, Point{0, +kMinWidthT + 2}, Point{0, 0})
			default:
				pts = append(pts, Point{+kMinWidthT, +kMinWidthT - 1}, Point{-kMinWidthT, +kMinWidthT + 4})
			}
			ps.push(newPen(x1, y1).getPolygon(pts...))
		case 0: // beginning of the stroke
			poly := pen1.getPolygon(
				Point{kMinWidthT, font.kMinWidthY * 0.5},
				Point{kMinWidthT + kMinWidthT*0.5, font.kMinWidthY*0.5 + font.kMinWidthY},
				Point{kMinWidthT - 2, font.kMinWidthY*0.5 + font.kMinWidthY*2 + 1},
			)
			if x1 != x2 { // ?????
				poly.set(2,
					x1+(kMinWidthT-2)*sinrad+(font.kMinWidthY*0.5+font.kMinWidthY*2)*cosrad,
					y1+(kMinWidthT+1)*-cosrad+(font.kMinWidthY*0.5+font.kMinWidthY*2)*sinrad) // ?????
			}
			ps.push(poly)
		}

		if x1 == x2 && a2 == 1 || a1 == 6 && (a2 == 0 || x1 != x2 && a2 == 5) {
			// KAGI NO YOKO BOU NO SAIGO NO MARU ... no need only used at 1st=yoko
			r := 0.8 // ?????
			if x1 == x2 && (a1 == 6 && a2 == 0 || a2 == 1) {
				r = 0.6
			}
			poly := polygonOf(
				pen2.getPoint(kMinWidthT, 0),
				pen2.getPoint(kMinWidthT*0.6, kMinWidthT*r),
				pen2.getPoint(0, kMinWidthT),
				pen2.getPoint(-kMinWidthT*0.6, kMinWidthT*r),
				pen2.getPoint(-kMinWidthT, 0),
			)
			if x1 == x2 && (a1 == 6 && a2 == 0 || a2 == 1) {
				// for backward compatibility
				poly.reverse()
			}
			ps.push(poly)
			if x1 != x2 && a1 == 6 && a2 == 5 {
				// KAGI NO YOKO BOU NO HANE
				haneLength := font.kWidth * 5
				rv := -1.0
				if x1 < x2 {
					rv = 1
				}
				ps.push(pen2.getPolygon(
					Point{rv * (kMinWidthT - 1), 0},
					Point{rv * (kMinWidthT + haneLength), 2},
					Point{rv * (kMinWidthT + haneLength), 0},
					Point{kMinWidthT - 1, -kMinWidthT}, // rv ?????
				))
			}
		}
	} else if y1 == y2 && a1 == 6 {
		// if it is YOKO stroke, use x-axis
		// if it is KAGI's YOKO stroke, get bold
		pen1r := newPen(x1, y1)
		pen2r := newPen(x2, y2)
		ps.push(polygonOf(
			pen1r.getPoint(0, -kMinWidthT),
			pen2r.getPoint(0, -kMinWidthT),
			pen2r.getPoint(0, +kMinWidthT),
			pen1r.getPoint(0, +kMinWidthT),
		))

		switch a2 {
		case 1, 0, 5: // no need a2=1
			// KAGI NO YOKO BOU NO SAIGO NO MARU
			pen2 := newPen(x2, y2)
			if x1 > x2 {
				pen2.setMatrix2(-1, 0)
			}
			r := 0.6
			poly := pen2.getPolygon(
				Point{0, -kMinWidthT},
				Point{+kMinWidthT * r, -kMinWidthT * 0.6},
				Point{+kMinWidthT, 0},
				Point{+kMinWidthT * r, +kMinWidthT * 0.6},
				Point{0, +kMinWidthT},
			)
			if x1 >= x2 {
				poly.reverse()
			}
			ps.push(poly)

			if a2 == 5 {
				haneLength := font.kWidth * (4*(1-opt1/font.kAdjustMageStep) + 1)
				// KAGI NO YOKO BOU NO HANE
				rv := -1.0
				if x1 < x2 {
					rv = 1
				}
				ps.push(pen2.getPolygon(
					Point{0, rv * -kMinWidthT},
					Point{2, rv * (-kMinWidthT - haneLength)},
					Point{0, rv * (-kMinWidthT - haneLength)},
					Point{-kMinWidthT, rv * -kMinWidthT},
				))
			}
		}
	} else {
		// for others, use x-axis
		// ASAI KAUDO
		var cosrad, sinrad float64
		if y1 == y2 {
			cosrad, sinrad = 1, 0 // ?????
		} else {
			cosrad, sinrad = normalize(x2-x1, y2-y1, 1)
		}
		// always same
		pen1 := newPen(x1, y1)
		pen2 := newPen(x2, y2)
		pen1.setMatrix2(cosrad, sinrad)
		pen2.setMatrix2(cosrad, sinrad)
		ps.push(polygonOf(
			pen1.getPoint(0, -font.kMinWidthY),
			pen2.getPoint(0, -font.kMinWidthY),
			pen2.getPoint(0, font.kMinWidthY),
			pen1.getPoint(0, font.kMinWidthY),
		))

		if a2 == 0 { // UROKO
			urokoScale := (font.kMinWidthU/font.kMinWidthY-1.0)/4.0 + 1.0
			ux := kakatoAt(font.kAdjustUrokoX, urokoAdjustment)
			uy := kakatoAt(font.kAdjustUrokoY, urokoAdjustment)
			poly2 := pen2.getPolygon(
				Point{0, -font.kMinWidthY},
				Point{-ux * urokoScale, 0},
			)
			poly2.push(
				x2-(cosrad-sinrad)*ux*urokoScale/2,
				y2-(sinrad+cosrad)*uy*urokoScale,
			)
			ps.push(poly2)
		}
	}
}

// kakatoAt indexes a parameter table as JavaScript does: an index outside the
// table (or not an integer) reads undefined, which turns the arithmetic into
// NaN, and push then drops the polygon.
func kakatoAt(table []float64, i float64) float64 {
	if i != math.Trunc(i) || i < 0 || int(i) >= len(table) {
		return math.NaN()
	}
	return table[int(i)]
}
