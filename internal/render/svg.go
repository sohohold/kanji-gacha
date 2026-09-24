package render

import (
	"encoding/xml"
	"fmt"
	"html"
	"kanji-gacha/internal/kage"
	"kanji-gacha/internal/model"
	"math"
	"strconv"
	"strings"
)

// License is recorded in each glyph and its SVG. Older glyphs carry the
// KanjiVG notice they were drawn under.
const License = "Glyph shapes: GlyphWiki https://glyphwiki.org/ (free to use), drawn with the KAGE engine (GPL v3); readings: KANJIDIC2 © EDRDG, CC BY-SA 4.0 https://www.edrdg.org/edrdg/licence.html"

// Canvas is the side of the square the outlines are drawn in (the KAGE frame).
const Canvas = 200

// SVG writes KAGE outlines as filled paths, one path per KAGE stroke, so the
// page can reveal a glyph stroke by stroke. The polygons of a stroke overlap,
// so they are wound one way and filled with the nonzero rule.
func SVG(strokes [][]kage.Polygon, label string) string {
	var b strings.Builder
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %d %d" role="img" aria-label="%s"><title>%s</title><metadata>%s</metadata><g fill="#151a17">`, Canvas, Canvas, html.EscapeString(label), html.EscapeString(label), html.EscapeString(License))
	num := func(v float64) string { return strconv.FormatFloat(math.Round(v*10)/10, 'f', -1, 64) }
	for _, ps := range strokes {
		kage.NormalizeWinding(ps)
		var d strings.Builder
		for _, p := range ps {
			for i, q := range p {
				if i == 0 {
					d.WriteString("M")
				} else {
					d.WriteString(" ")
				}
				d.WriteString(num(q.X))
				d.WriteString(" ")
				d.WriteString(num(q.Y))
			}
			d.WriteString("Z")
		}
		if d.Len() > 0 {
			fmt.Fprintf(&b, `<path d="%s"/>`, d.String())
		}
	}
	b.WriteString(`</g></svg>`)
	return b.String()
}

// ReadSVG reads a stored glyph back for the OGP image: its paths, the side of
// its viewBox, and its stroke width, which is 0 for filled outlines. Glyphs
// drawn before the move to GlyphWiki are KanjiVG centre lines with a width.
func ReadSVG(s string) ([]model.Stroke, float64, float64, error) {
	var doc struct {
		ViewBox string `xml:"viewBox,attr"`
		Group   struct {
			Width float64 `xml:"stroke-width,attr"`
			Paths []struct {
				D string `xml:"d,attr"`
			} `xml:"path"`
		} `xml:"g"`
	}
	if e := xml.Unmarshal([]byte(s), &doc); e != nil {
		return nil, 0, 0, e
	}
	var x, y, w, h float64
	if _, e := fmt.Sscanf(doc.ViewBox, "%f %f %f %f", &x, &y, &w, &h); e != nil || w <= 0 || w != h {
		return nil, 0, 0, fmt.Errorf("invalid viewBox")
	}
	paths := []model.Stroke{}
	for _, p := range doc.Group.Paths {
		st, e := ParsePath(p.D)
		if e != nil {
			return nil, 0, 0, e
		}
		paths = append(paths, st)
	}
	if len(paths) == 0 {
		return nil, 0, 0, fmt.Errorf("empty SVG")
	}
	return paths, w, doc.Group.Width, nil
}
