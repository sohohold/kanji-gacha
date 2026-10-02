package kage

import (
	"compress/gzip"
	"encoding/json"
	"math"
	"os"
	"strings"
	"testing"
)

type golden struct {
	Engine string
	Buhin  map[string]string
	Cases  []struct {
		ID        string
		Data      string
		Polygons  [][]float64
		PerStroke []int
	}
}

func loadGolden(t *testing.T) golden {
	t.Helper()
	f, e := os.Open("testdata/golden.json.gz")
	if e != nil {
		t.Fatal(e)
	}
	defer f.Close()
	z, e := gzip.NewReader(f)
	if e != nil {
		t.Fatal(e)
	}
	var g golden
	if e = json.NewDecoder(z).Decode(&g); e != nil {
		t.Fatal(e)
	}
	return g
}

// Test ID: KAGE-01
// The port must draw what the reference engine draws. Outlines are floored to
// a 0.1 grid, so a last-bit difference in a math function can move a point by
// one step; anything more is a porting error.
func TestMatchesReferenceEngine(t *testing.T) {
	g := loadGolden(t)
	lookup := func(name string) string { return g.Buhin[name] }
	inexact := 0
	for _, c := range g.Cases {
		groups := Render(Expand(c.Data, lookup))
		var got []Polygon
		for _, ps := range groups {
			got = append(got, ps...)
		}
		if len(got) != len(c.Polygons) {
			t.Errorf("%s: %d polygons, want %d", c.ID, len(got), len(c.Polygons))
			continue
		}
		for i, p := range got {
			want := c.Polygons[i]
			if len(p)*2 != len(want) {
				t.Errorf("%s: polygon %d has %d points, want %d", c.ID, i, len(p), len(want)/2)
				continue
			}
			for j, q := range p {
				dx, dy := math.Abs(q.X-want[2*j]), math.Abs(q.Y-want[2*j+1])
				if dx > 0.10001 || dy > 0.10001 {
					t.Errorf("%s: polygon %d point %d is (%g, %g), want (%g, %g)", c.ID, i, j, q.X, q.Y, want[2*j], want[2*j+1])
				} else if dx > 1e-9 || dy > 1e-9 {
					inexact++
				}
			}
		}
		// Grouping by stroke matches drawing each stroke on its own, except
		// where a transform line moves the polygons of earlier strokes.
		if !strings.Contains(c.Data, "$0:") && !strings.HasPrefix(c.Data, "0:") {
			if len(groups) != len(c.PerStroke) {
				t.Errorf("%s: %d stroke groups, want %d", c.ID, len(groups), len(c.PerStroke))
				continue
			}
			for i, ps := range groups {
				if len(ps) != c.PerStroke[i] {
					t.Errorf("%s: stroke %d has %d polygons, want %d", c.ID, i, len(ps), c.PerStroke[i])
				}
			}
		}
	}
	t.Logf("%d cases against %s, %d points one grid step off", len(g.Cases), g.Engine, inexact)
}

// Test ID: KAGE-02
func TestExpandPlacesComponents(t *testing.T) {
	parts := map[string]string{"a": "1:0:0:0:100:200:100", "b": "99:0:0:0:0:100:200:a"}
	s := Expand("99:0:0:100:0:200:100:b", func(n string) string { return parts[n] })
	if len(s) != 1 {
		t.Fatalf("%d strokes", len(s))
	}
	if s[0].Type() != 1 || s[0].Points()[0] != [2]float64{100, 50} || s[0].Points()[1] != [2]float64{150, 50} {
		t.Fatalf("%+v", s[0].Points())
	}
	b := Bounds(s)
	if b != (Box{100, 50, 150, 50}) {
		t.Fatalf("%+v", b)
	}
}

// Test ID: KAGE-03
// A reference cycle would overflow the original's stack; the port stops at a
// fixed depth instead.
func TestExpandCycle(t *testing.T) {
	parts := map[string]string{"a": "99:0:0:0:0:200:200:a$1:0:0:20:100:180:100"}
	s := Expand("99:0:0:0:0:200:200:a", func(n string) string { return parts[n] })
	if len(s) != maxDepth {
		t.Fatalf("%d strokes", len(s))
	}
}

// Test ID: KAGE-04
func TestNormalizeWinding(t *testing.T) {
	g := loadGolden(t)
	for _, c := range g.Cases {
		if !strings.HasPrefix(c.ID, "glyph:") {
			continue
		}
		for _, ps := range Render(Expand(c.Data, func(n string) string { return g.Buhin[n] })) {
			NormalizeWinding(ps)
			for _, p := range ps {
				if signedArea(p) < 0 {
					t.Fatalf("%s: counter-clockwise polygon after NormalizeWinding", c.ID)
				}
			}
		}
	}
}

// Test ID: KAGE-05
func TestJSNumber(t *testing.T) {
	for in, want := range map[string]float64{"": 0, " 12 ": 12, "-3.5": -3.5, "1e2": 100, "0x10": 16, ".5": 0.5, "5.": 5} {
		if got := jsNumber(in); got != want {
			t.Errorf("jsNumber(%q) = %g, want %g", in, got, want)
		}
	}
	for _, in := range []string{"x", "1a", "0x", "inf", "NaN", "1_0", "--1"} {
		if got := jsNumber(in); !math.IsNaN(got) {
			t.Errorf("jsNumber(%q) = %g, want NaN", in, got)
		}
	}
}

func BenchmarkRender(b *testing.B) {
	parts := map[string]string{
		"u6c35-07":  "2:7:8:42:12:99:23:124:35$2:7:8:20:62:75:71:97:85$2:7:8:12:123:90:151:81:188$2:2:7:63:144:109:118:188:51",
		"u26c29-07": "1:0:0:18:29:187:29$1:0:0:73:10:73:48$1:0:0:132:10:132:48$1:12:13:44:59:44:87$1:2:2:44:59:163:59$1:22:23:163:59:163:87$1:2:2:44:87:163:87$1:0:0:32:116:176:116$1:0:0:21:137:190:137$7:32:7:102:59:102:123:102:176:10:190$2:7:0:105:137:126:169:181:182",
	}
	lookup := func(n string) string { return parts[n] }
	for b.Loop() {
		Render(Expand("99:150:0:9:12:73:200:u6c35-07:0:-10:50$99:0:0:54:10:190:199:u26c29-07", lookup))
	}
}
