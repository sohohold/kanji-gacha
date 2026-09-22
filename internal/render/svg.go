package render

import (
	"encoding/xml"
	"fmt"
	"html"
	"kanji-gacha/internal/model"
	"strings"
)

const License = "KanjiVG © Ulrich Apel — adapted, CC BY-SA 3.0 https://creativecommons.org/licenses/by-sa/3.0/; readings: KANJIDIC2 © EDRDG, CC BY-SA 4.0 https://www.edrdg.org/edrdg/licence.html"

func SVG(strokes []model.Stroke, canvas, width float64, label string) string {
	var b strings.Builder
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %g %g" role="img" aria-label="%s"><title>%s</title><metadata>%s</metadata><g fill="none" stroke="#151a17" stroke-width="%g" stroke-linecap="round" stroke-linejoin="round">`, canvas, canvas, html.EscapeString(label), html.EscapeString(label), html.EscapeString(License), width)
	for _, s := range strokes {
		if len(s) == 0 {
			continue
		}
		fmt.Fprintf(&b, `<path d="M%.3f %.3f`, s[0].Start.X, s[0].Start.Y)
		for _, c := range s {
			fmt.Fprintf(&b, ` C%.3f %.3f %.3f %.3f %.3f %.3f`, c.C1.X, c.C1.Y, c.C2.X, c.C2.Y, c.End.X, c.End.Y)
		}
		b.WriteString(`"/>`)
	}
	b.WriteString(`</g></svg>`)
	return b.String()
}
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
