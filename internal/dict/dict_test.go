package dict

import "testing"

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
func TestVerifySpecials(t *testing.T) {
	d, e := Load()
	if e != nil {
		t.Fatal(e)
	}
	if e = verifySpecials(&d, map[string]Override{"kvg:056fd-g1": {Position: "enclosure", Operator: "⿴"}}); e != nil {
		t.Fatal(e)
	}
	if e = verifySpecials(&d, map[string]Override{"kvg:ffffff-g9": {Position: "enclosure"}}); e == nil {
		t.Error("an override matching no part must fail the build")
	}
	if e = verifySpecials(&d, map[string]Override{"kvg:ffffff-g9": {Exclude: true}}); e != nil {
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
