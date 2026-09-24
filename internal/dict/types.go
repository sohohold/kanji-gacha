package dict

import (
	"bytes"
	"compress/gzip"
	"encoding/gob"
	"io"
	"kanji-gacha/internal/model"
	"kanji-gacha/internal/vector"
	"maps"
	"slices"
)

type Reading struct {
	Text      string `json:"text"`
	Okurigana string `json:"okurigana,omitempty"`
}

// Part is a component the generator places: a GlyphWiki glyph and what it
// stands for.
type Part struct {
	// ID is unique; it is the glyph name, with "#frame" appended for a whole
	// glyph that parents also use as 構・垂・繞.
	ID string
	// Glyph is the GlyphWiki name of the drawing, a key of Dictionary.Glyphs.
	Glyph string
	Char  string
	// Position is whole, left, right, top, bottom or enclosure.
	Position string
	// Bounds is the box around the control points in the 200×200 KAGE frame.
	Bounds model.Box
	// Strokes is the handwritten stroke count, from KANJIDIC2 where it lists
	// the character.
	Strokes   int
	On        []Reading
	Kun       []Reading
	OnVector  vector.Vector
	KunVector vector.Vector
	Frequency float64
	// Inner is where a 構・垂・繞 leaves room for the rest, relative to
	// Bounds, and Operator the IDS operator it forms.
	Inner    *model.Box `json:",omitempty"`
	Operator string     `json:",omitempty"`
	// FrameUses is how many parents draw other components inside this part.
	FrameUses int
}
type Dictionary struct {
	Version string
	Parts   []Part
	// Glyphs is the KAGE data of every glyph the parts draw with, by name
	// ("name@n" for a pinned version).
	Glyphs  map[string]string
	Sources map[string]string
	Report  map[string]int
}

// Lookup returns the KAGE data of a glyph, or "" if the dictionary lacks it.
func (d *Dictionary) Lookup(name string) string { return d.Glyphs[name] }

// file is the stored form of a Dictionary. gob writes map entries in
// iteration order, so the maps are stored as key-sorted slices to keep the
// same build input producing the same bytes.
type file struct {
	Version string
	Parts   []Part
	Glyphs  []glyph
	Sources []source
	Report  []count
}
type glyph struct{ Name, Data string }
type source struct{ Name, SHA256 string }
type count struct {
	Name string
	N    int
}

// Encode stores the dictionary as gzip-compressed gob. Decoding it takes about
// a quarter of the time JSON took, which is most of the Lambda cold start.
func Encode(d Dictionary) ([]byte, error) {
	f := file{Version: d.Version, Parts: d.Parts}
	for _, k := range slices.Sorted(maps.Keys(d.Glyphs)) {
		f.Glyphs = append(f.Glyphs, glyph{k, d.Glyphs[k]})
	}
	for _, k := range slices.Sorted(maps.Keys(d.Sources)) {
		f.Sources = append(f.Sources, source{k, d.Sources[k]})
	}
	for _, k := range slices.Sorted(maps.Keys(d.Report)) {
		f.Report = append(f.Report, count{k, d.Report[k]})
	}
	var b bytes.Buffer
	z := gzip.NewWriter(&b)
	if e := gob.NewEncoder(z).Encode(f); e != nil {
		return nil, e
	}
	if e := z.Close(); e != nil {
		return nil, e
	}
	return b.Bytes(), nil
}
func Decode(b []byte) (Dictionary, error) {
	var f file
	z, e := gzip.NewReader(bytes.NewReader(b))
	if e != nil {
		return Dictionary{}, e
	}
	defer z.Close()
	if e = gob.NewDecoder(io.LimitReader(z, 128<<20)).Decode(&f); e != nil {
		return Dictionary{}, e
	}
	d := Dictionary{Version: f.Version, Parts: f.Parts, Glyphs: make(map[string]string, len(f.Glyphs)), Sources: map[string]string{}, Report: map[string]int{}}
	for _, g := range f.Glyphs {
		d.Glyphs[g.Name] = g.Data
	}
	for _, s := range f.Sources {
		d.Sources[s.Name] = s.SHA256
	}
	for _, c := range f.Report {
		d.Report[c.Name] = c.N
	}
	return d, nil
}
