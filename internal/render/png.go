package render

import (
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"github.com/fogleman/gg"
	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"image/png"
	"kanji-gacha/internal/model"
	"sync"
)

//go:embed assets/NotoSansJP.otf
var fontBytes []byte
var parsed = sync.OnceValues(func() (*opentype.Font, error) { return opentype.Parse(fontBytes) })

func PNG(ctx context.Context, g model.Glyph) ([]byte, error) {
	strokes, canvas, width, e := ReadSVG(g.SVG)
	if e != nil {
		return nil, e
	}
	f, e := parsed()
	if e != nil {
		return nil, e
	}
	dc := gg.NewContext(1200, 630)
	dc.SetHexColor("#101713")
	dc.Clear()
	dc.SetHexColor("#c4f3b1")
	dc.DrawRectangle(0, 0, 8, 630)
	dc.Fill()
	dc.SetHexColor("#f3f0e7")
	dc.DrawRoundedRectangle(70, 72, 486, 486, 20)
	dc.Fill()
	factor := 420 / canvas
	dc.SetHexColor("#151a17")
	dc.SetLineWidth(width * factor)
	dc.SetLineCap(gg.LineCapRound)
	dc.SetLineJoin(gg.LineJoinRound)
	for _, s := range strokes {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		dc.NewSubPath()
		dc.MoveTo(103+s[0].Start.X*factor, 105+s[0].Start.Y*factor)
		prev := s[0].Start
		for _, c := range s {
			if c.Start != prev {
				dc.NewSubPath()
				dc.MoveTo(103+c.Start.X*factor, 105+c.Start.Y*factor)
			}
			dc.CubicTo(103+c.C1.X*factor, 105+c.C1.Y*factor, 103+c.C2.X*factor, 105+c.C2.Y*factor, 103+c.End.X*factor, 105+c.End.Y*factor)
			prev = c.End
		}
		if width == 0 {
			dc.Fill()
		} else {
			dc.Stroke()
		}
	}
	text := func(s string, x, y, size float64, col string) error {
		face, e := opentype.NewFace(f, &opentype.FaceOptions{Size: size, DPI: 72, Hinting: font.HintingFull})
		if e != nil {
			return e
		}
		defer face.Close()
		dc.SetFontFace(face)
		dc.SetHexColor(col)
		dc.DrawString(s, x, y)
		return nil
	}
	for _, t := range []struct {
		s          string
		x, y, size float64
		c          string
	}{{"架空漢字ガチャ", 610, 145, 32, "#f3f0e7"}, {g.OGPReading, 610, 275, 42, "#c4f3b1"}, {g.Rarity, 610, 365, 64, "#f3f0e7"}, {fmt.Sprintf("%d 画", g.Strokes), 610, 423, 25, "#aab9ae"}, {"KanjiVG / CC BY-SA 3.0 · KANJIDIC2 / CC BY-SA 4.0 · Noto CJK / OFL 1.1", 70, 603, 13, "#aab9ae"}} {
		if e = text(t.s, t.x, t.y, t.size, t.c); e != nil {
			return nil, e
		}
	}
	var b bytes.Buffer
	if e = png.Encode(&b, dc.Image()); e != nil {
		return nil, e
	}
	return b.Bytes(), ctx.Err()
}
