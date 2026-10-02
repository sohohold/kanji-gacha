package dict

import (
	"bytes"
	"reflect"
	"testing"
)

// Test ID: DICT-04
// The bundled dictionary must carry every asset the special slots draw from;
// Build enforces this, and this guards the artifact that ships.
func TestBundledSpecialAssets(t *testing.T) {
	d, e := Load()
	if e != nil {
		t.Fatal(e)
	}
	whole, frames := map[string]bool{}, 0
	for i := range d.Parts {
		p := &d.Parts[i]
		if p.Position == "whole" {
			whole[p.Char] = true
		}
		if p.Inner != nil && p.Operator != "" {
			frames++
		}
	}
	for _, c := range LegendChars {
		if !whole[c] {
			t.Errorf("no whole part for legend char %q", c)
		}
	}
	if frames == 0 {
		t.Error("no frame part with both inner and operator")
	}
}

// Test ID: DICT-05
func TestVerifySpecials(t *testing.T) {
	d, e := Load()
	if e != nil {
		t.Fatal(e)
	}
	if e = verifySpecials(&d, map[string]Override{"u56d7-j#frame": {Operator: "⿴"}}); e != nil {
		t.Fatal(e)
	}
	if e = verifySpecials(&d, map[string]Override{"uffff-99": {Position: "enclosure"}}); e == nil {
		t.Error("an override matching no part must fail the build")
	}
	if e = verifySpecials(&d, map[string]Override{"uffff-99": {Exclude: true}}); e != nil {
		t.Errorf("an exclude override needs no matching part: %v", e)
	}
	missing := Dictionary{Report: map[string]int{}}
	for i := range d.Parts {
		if d.Parts[i].Char != LegendChars[0] {
			missing.Parts = append(missing.Parts, d.Parts[i])
		}
	}
	if e = verifySpecials(&missing, nil); e == nil {
		t.Error("a missing legend asset must fail the build")
	}
}

// Test ID: DICT-06
// gob writes maps in iteration order; Encode must still give the same bytes
// for the same dictionary, or every rebuild shows a spurious diff.
func TestEncodeRoundTrip(t *testing.T) {
	d, e := Load()
	if e != nil {
		t.Fatal(e)
	}
	small := Dictionary{Version: d.Version, Parts: d.Parts[:200], Glyphs: map[string]string{"u4e00": d.Glyphs["u4e00"], "u6c35-01": d.Glyphs["u6c35-01"]}, Sources: d.Sources, Report: d.Report}
	b, e := Encode(small)
	if e != nil {
		t.Fatal(e)
	}
	for range 5 {
		again, e := Encode(small)
		if e != nil {
			t.Fatal(e)
		}
		if !bytes.Equal(b, again) {
			t.Fatal("Encode is not deterministic")
		}
	}
	got, e := Decode(b)
	if e != nil {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(got, small) {
		t.Error("Decode(Encode(d)) differs from d")
	}
}

// Test ID: DICT-07
// Every glyph a part draws with, and every glyph those reference, ships in
// the dictionary; a missing one would silently drop strokes.
func TestBundledGlyphsComplete(t *testing.T) {
	d, e := Load()
	if e != nil {
		t.Fatal(e)
	}
	for _, p := range d.Parts {
		stack := []string{p.Glyph}
		for len(stack) > 0 {
			n := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			data, ok := d.Glyphs[n]
			if !ok {
				t.Fatalf("%s: glyph %s missing", p.ID, n)
			}
			refs, _ := lines(data)
			for _, r := range refs {
				stack = append(stack, r.name)
			}
		}
		if p.Strokes <= 0 || p.Bounds.W <= 0 || p.Bounds.H <= 0 {
			t.Fatalf("%s: %d strokes, bounds %+v", p.ID, p.Strokes, p.Bounds)
		}
	}
}

// The cold start is mostly this decode.
func BenchmarkDecode(b *testing.B) {
	for b.Loop() {
		if _, e := Decode(bundled); e != nil {
			b.Fatal(e)
		}
	}
}

// Test ID: DICT-13
// The shipped dictionary is built over the 6,458 parents spec 3.1 states;
// a rebuild that changes the count must update the spec with it.
func TestBundledParents(t *testing.T) {
	d, e := Load()
	if e != nil {
		t.Fatal(e)
	}
	if d.Report["parents"] != 6458 || d.Report["parentsMissing"] != 0 {
		t.Fatalf("%d parents, %d missing", d.Report["parents"], d.Report["parentsMissing"])
	}
}
