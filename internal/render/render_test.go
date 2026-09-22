package render

import (
	"bytes"
	"context"
	"image/png"
	"kanji-gacha/internal/model"
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
func TestBiangPNG(t *testing.T) {
	b, e := PNG(context.Background(), model.Glyph{SVG: Biang, OGPReading: "ビャン", Rarity: "UR", Strokes: 58})
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
func FuzzPath(f *testing.F) {
	f.Add("M1 2c3 4 5 6 7 8")
	f.Add("M1 2Q3 4 5 6Z")
	f.Fuzz(func(t *testing.T, s string) {
		if len(s) > 4096 {
			return
		}
		p, e := ParsePath(s)
		if e == nil {
			out := SVG([]model.Stroke{p}, 109, 2.5, "test")
			if _, _, _, e = ReadSVG(out); e != nil {
				t.Fatal(e)
			}
		}
	})
}
