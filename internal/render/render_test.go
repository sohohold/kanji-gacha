package render

import (
	"bytes"
	"context"
	"image/png"
	"kanji-gacha/internal/kage"
	"kanji-gacha/internal/model"
	"strings"
	"testing"
)

func TestRelativeSmooth(t *testing.T) {
	s, e := ParsePath("M10,20c1,2,3,4,5,6s7,8,9,10")
	if e != nil {
		t.Fatal(e)
	}
	if s[1].Start != (model.Point{X: 15, Y: 26}) || s[1].C1 != (model.Point{X: 17, Y: 28}) || s[1].End != (model.Point{X: 24, Y: 36}) {
		t.Fatalf("%+v", s[1])
	}
}

func outline(data string) string {
	return SVG(kage.Render(kage.Expand(data, func(string) string { return "" })), "test")
}

// Every KAGE stroke becomes one filled path, and the OGP renderer reads it back.
func TestSVGRoundTrip(t *testing.T) {
	svg := outline("1:0:0:20:100:180:100$1:0:0:100:20:100:180$2:7:8:40:40:80:80:120:160")
	if n := strings.Count(svg, "<path "); n != 3 {
		t.Fatalf("%d paths", n)
	}
	strokes, canvas, width, e := ReadSVG(svg)
	if e != nil {
		t.Fatal(e)
	}
	if len(strokes) != 3 || canvas != Canvas || width != 0 {
		t.Fatalf("%d strokes, canvas %g, width %g", len(strokes), canvas, width)
	}
}

// legacy is a glyph stored before GlyphWiki: KanjiVG centre lines stroked on
// a 109 canvas.
const legacy = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 109 109" role="img" aria-label="test"><title>test</title><metadata>KanjiVG</metadata><g fill="none" stroke="#151a17" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><path d="M20.000 20.000 C20.000 20.000 89.000 89.000 89.000 89.000"/><path d="M89.000 20.000 C89.000 20.000 20.000 89.000 20.000 89.000"/></g></svg>`

func TestPNGSize(t *testing.T) {
	for _, g := range []model.Glyph{
		{SVG: outline("1:0:0:20:100:180:100$1:0:0:100:20:100:180"), License: License},
		{SVG: legacy, License: "KanjiVG © Ulrich Apel"},
	} {
		g.OGPReading, g.Rarity, g.Strokes = "テスト", "N", 2
		b, e := PNG(context.Background(), g)
		if e != nil {
			t.Fatal(e)
		}
		im, e := png.Decode(bytes.NewReader(b))
		if e != nil {
			t.Fatal(e)
		}
		if im.Bounds().Dx() != 1200 || im.Bounds().Dy() != 630 {
			t.Fatal(im.Bounds())
		}
	}
	if _, _, width, _ := ReadSVG(legacy); width != 2.5 {
		t.Fatalf("legacy stroke width %g", width)
	}
}

func FuzzPath(f *testing.F) {
	f.Add("M1 2c3 4 5 6 7 8")
	f.Add("M1 2Q3 4 5 6Z")
	f.Add("M1 2 3 4 5 6ZM7 8 9 10 11 12Z")
	f.Fuzz(func(t *testing.T, s string) {
		if len(s) > 4096 {
			return
		}
		ParsePath(s)
	})
}
