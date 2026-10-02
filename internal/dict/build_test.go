package dict

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"kanji-gacha/internal/kage"
	"kanji-gacha/internal/model"
	"kanji-gacha/internal/vector"
	"math"
	"strings"
	"testing"
)

// archive makes a dump.tar.gz with the two files GlyphWiki ships.
func archive(t *testing.T, newest, all map[string]string) []byte {
	t.Helper()
	file := func(m map[string]string, escape bool) []byte {
		var b bytes.Buffer
		b.WriteString("\ufeff name | related | data\n------+------+------\n")
		for n, d := range m {
			if escape {
				n = strings.ReplaceAll(n, "@", `\@`)
			}
			fmt.Fprintf(&b, " %-20s | u3013 | %s\n", n, d)
		}
		fmt.Fprintf(&b, "(%d 行)\n", len(m))
		return b.Bytes()
	}
	var out bytes.Buffer
	z := gzip.NewWriter(&out)
	tw := tar.NewWriter(z)
	for _, f := range []struct {
		name string
		body []byte
	}{{newestFile, file(newest, false)}, {versionsFile, file(all, true)}} {
		tw.WriteHeader(&tar.Header{Name: f.name, Mode: 0644, Size: int64(len(f.body))})
		tw.Write(f.body)
	}
	tw.Close()
	z.Close()
	return out.Bytes()
}

// entry is a KANJIDIC2 character: its on reading, whether it is in JIS X 0208,
// and its grade (常用・人名用 have one). A reading of "" lists only a pinyin one.
type entry struct {
	on    string
	jis   bool
	grade string
}

func kanjidic(t *testing.T, chars map[string]string) []byte {
	t.Helper()
	es := map[string]entry{}
	for c, r := range chars {
		es[c] = entry{on: r, jis: true}
	}
	return kanjidicOf(t, es)
}

func kanjidicOf(t *testing.T, chars map[string]entry) []byte {
	t.Helper()
	var b bytes.Buffer
	b.WriteString("<kanjidic2>")
	for c, e := range chars {
		cp, grade, reading := "ucs", "", `<reading r_type="pinyin">jia3</reading>`
		if e.jis {
			cp = "jis208"
		}
		if e.grade != "" {
			grade = "<grade>" + e.grade + "</grade>"
		}
		if e.on != "" {
			reading = `<reading r_type="ja_on">` + e.on + `</reading>`
		}
		fmt.Fprintf(&b, `<character><literal>%s</literal><codepoint><cp_value cp_type="%s">1-1-1</cp_value></codepoint><misc>%s<stroke_count>5</stroke_count></misc><reading_meaning><rmgroup>%s</rmgroup></reading_meaning></character>`, c, cp, grade, reading)
	}
	b.WriteString("</kanjidic2>")
	var out bytes.Buffer
	z := gzip.NewWriter(&out)
	z.Write(b.Bytes())
	z.Close()
	return out.Bytes()
}

const (
	box   = "1:12:13:30:30:30:170$1:2:2:30:30:170:30$1:22:23:170:30:170:170$1:2:2:30:170:170:170"
	cross = "1:0:0:60:100:140:100$1:0:0:100:60:100:140"
	water = "2:7:8:42:12:99:23:124:35$2:7:8:20:62:75:71:97:85$2:2:7:63:144:109:118:188:51"
)

// fixture is a small dump and KANJIDIC2 that build a complete dictionary:
// a ⿴ frame, a 偏 and a 旁, a pinned version, and the legend characters.
func fixture() (newest, all, chars map[string]string) {
	newest = map[string]string{
		// 囗 encloses the rest of 国・困・因, so it becomes a ⿴ frame.
		"u56d7": "99:0:0:0:0:200:200:u56d7-j", "u56d7-j": box,
		"u56fd-j": "99:0:0:0:0:200:200:u56d7-j$99:0:0:40:40:160:160:u7389-j",
		"u56f0-j": "99:0:0:0:0:200:200:u56d7-j$99:0:0:40:40:160:160:u6728-j",
		"u56e0-j": "99:0:0:0:0:200:200:u56d7-j@2$99:0:0:40:40:160:160:u5927-j",
		"u7389-j": cross, "u6728-j": cross, "u5927-j": cross,
		// 氵 as 偏: the Japanese form wins over the plain one; ⺡ aliases it.
		"u6c35-01": water, "u6c35-j01": water + "$1:0:0:10:10:20:20", "u2ea1": "99:0:0:0:0:200:200:u6c35-j01",
		"u6c60-j":  "99:0:0:0:0:70:200:u6c35-j01$99:0:0:60:0:200:200:u4e5f-02",
		"u4e5f-02": cross, "u4e5f": cross,
		// Legend characters.
		"u5f41": box, "u599b": box, "u96f2": box, "u9f8d": cross,
	}
	all = map[string]string{"u56d7-j@1": cross, "u56d7-j@2": box + "$" + cross}
	chars = map[string]string{"国": "コク", "困": "コン", "因": "イン", "玉": "ギョク", "木": "モク", "大": "ダイ", "池": "チ", "也": "ヤ", "彁": "カ", "妛": "シ", "雲": "ウン", "龍": "リュウ"}
	return newest, all, chars
}

// Test ID: DICT-01
func TestBuild(t *testing.T) {
	newest, all, chars := fixture()
	gw, kd := archive(t, newest, all), kanjidic(t, chars)
	d, e := Build(gw, kd, []byte("{}"))
	if e != nil {
		t.Fatal(e)
	}
	by := map[string]Part{}
	for _, p := range d.Parts {
		by[p.ID] = p
	}
	again, e := Build(gw, kd, []byte("{}"))
	if e != nil {
		t.Fatal(e)
	}
	b1, _ := Encode(d)
	b2, _ := Encode(again)
	if !bytes.Equal(b1, b2) {
		t.Error("the same input built different bytes")
	}
	// Every parent is a whole part, drawn with its Japanese form.
	for c := range chars {
		found := false
		for _, p := range d.Parts {
			found = found || p.Position == "whole" && p.Char == c
		}
		if !found {
			t.Errorf("parent %s has no whole part", c)
		}
	}
	// The build report counts what went in.
	r := d.Report
	if r["parts"] != len(d.Parts) || r["glyphs"] != len(d.Glyphs) || r["position:left"] == 0 || r["position:right"] == 0 || r["frameParts"] == 0 || r["withReadings"] == 0 || r["strokesEstimated"] == 0 {
		t.Errorf("report %v", r)
	}
	f, ok := by["u56d7-j#frame"]
	if !ok || f.Char != "囗" || f.Operator != "⿴" || f.FrameUses != 3 || f.Inner == nil {
		t.Fatalf("frame %+v", f)
	}
	if in := *f.Inner; in.X < 0.32 || in.X > 0.34 || in.W < 0.33 || in.W > 0.35 {
		t.Errorf("inner %+v", in)
	}
	if p := by["u6c35-j01"]; p.Char != "氵" || p.Position != "left" {
		t.Errorf("氵 %+v", p)
	}
	if _, ok := by["u6c35-01"]; ok {
		t.Error("the plain 偏 form kept beside the Japanese one")
	}
	if p := by["u2ea1"]; p.Char != "⺡" || p.Position != "left" {
		t.Errorf("⺡ %+v", p)
	}
	if p := by["u4e5f-02"]; p.Position != "right" || p.Frequency != 2.0/float64(d.Report["parents"]) {
		t.Errorf("也 as 旁 %+v (used by 池 and 也)", p)
	}
	if d.Glyphs["u56d7-j@2"] == "" || d.Report["pinnedVersions"] != 1 {
		t.Errorf("pinned version not read: %v", d.Report)
	}
	if p := by["u56e0-j"]; p.Strokes != 5 || p.Bounds != (model.Box{X: 30, Y: 30, W: 140, H: 140}) {
		t.Errorf("因 %+v", p)
	}
	// A pinned version the archive lacks is reported, not fatal.
	newest["u4e5f"] = "99:0:0:0:0:200:200:u5927-j@9"
	d, e = Build(archive(t, newest, all), kanjidic(t, chars), []byte("{}"))
	if e != nil || d.Report["unresolvedRefs"] == 0 || d.Report["partsUnresolved"] != 1 {
		t.Fatalf("%v %v", e, d.Report)
	}
}

// Test ID: DICT-02
func TestCharOf(t *testing.T) {
	for n, want := range map[string]string{"u9752-02": "青", "ufa66-j": "辶", "u26c29-07@3": "𦰩", "cdp-8bd0": "", "u9752x": ""} {
		if got := charOf(n); got != want {
			t.Errorf("charOf(%q) = %q, want %q", n, got, want)
		}
	}
}

// Test ID: DICT-03
func TestHandStrokes(t *testing.T) {
	for data, want := range map[string]int{
		box: 3, // 口: ㇑ ㇕ ㇐
		"1:0:32:100:18:100:165$1:0:13:34:58:34:165$1:2:2:34:165:166:165$1:0:23:166:58:166:165": 3, // 山
		"1:0:2:64:39:123:39$2:27:0:123:39:131:137:180:173$2:0:7:74:68:56:154:12:183":           2, // 八
	} {
		if got := handStrokes(kage.Expand(data, nil)); got != want {
			t.Errorf("%s: %d, want %d", data, got, want)
		}
	}
}

func parts(d Dictionary) map[string]Part {
	by := map[string]Part{}
	for _, p := range d.Parts {
		by[p.ID] = p
	}
	return by
}

// Test ID: DICT-08
// A parent is a character with a Japanese reading that is in JIS X 0208 or
// has a grade (常用・人名用), and it is drawn with its Japanese form.
func TestParentSelection(t *testing.T) {
	newest, all, chars := fixture()
	newest["u7532"], newest["u7532-j"] = cross, box // 甲: both forms
	newest["u4e59"], newest["u4e19"], newest["u4e01"] = cross, cross, cross
	es := map[string]entry{
		"甲": {on: "コウ", jis: true},
		"乙": {on: "オツ", grade: "9"}, // 人名用, outside JIS X 0208
		"丙": {on: "ヘイ"},             // neither
		"丁": {jis: true},            // no Japanese reading
	}
	for c, r := range chars {
		es[c] = entry{on: r, jis: true}
	}
	d, e := Build(archive(t, newest, all), kanjidicOf(t, es), []byte("{}"))
	if e != nil {
		t.Fatal(e)
	}
	whole := map[string]string{}
	for _, p := range d.Parts {
		if p.Position == "whole" {
			whole[p.Char] = p.ID
		}
	}
	if whole["甲"] != "u7532-j" {
		t.Errorf("甲 drawn with %q, want the Japanese form u7532-j", whole["甲"])
	}
	if whole["乙"] == "" {
		t.Error("乙 has a grade but is not a parent")
	}
	for _, c := range []string{"丙", "丁"} {
		if whole[c] != "" {
			t.Errorf("%s became a parent", c)
		}
	}
	if d.Report["parents"] != len(chars)+2 {
		t.Errorf("%d parents, want %d", d.Report["parents"], len(chars)+2)
	}
}

// Test ID: DICT-09
// -03 is a 冠, -04 a 脚 and -05 the outside of an enclosure; a variant of a
// character no parent is built from is left out; a part's sound vector sums
// the readings of the parents that contain it with its own at three times.
func TestVariantParts(t *testing.T) {
	newest, all, chars := fixture()
	newest["u82b1-j"] = "99:0:0:0:0:200:80:u8279-03$99:0:0:0:80:200:200:u5316-04" // 花 = 艹 over 化
	newest["u8279-03"], newest["u5316-04"] = cross, cross
	newest["u540c-j"] = "99:0:0:0:0:200:200:u5182-05$99:0:0:50:60:150:160:u4e00" // 同 = 冂 around 一
	newest["u5182-05"], newest["u4e00"] = box, cross
	newest["u4e59-02"] = cross                                                     // 乙 as 旁, which no parent uses
	newest["u5480-j"] = "99:0:0:0:0:100:200:u53e3-01$99:0:0:100:0:200:200:u6c60-j" // 咀 contains 池, so 也 two levels down
	newest["u53e3-01"] = cross
	chars["花"], chars["同"], chars["咀"] = "カ", "ドウ", "ソ"
	d, e := Build(archive(t, newest, all), kanjidic(t, chars), []byte("{}"))
	if e != nil {
		t.Fatal(e)
	}
	by := parts(d)
	for id, want := range map[string][2]string{"u8279-03": {"艹", "top"}, "u5316-04": {"化", "bottom"}, "u5182-05": {"冂", "enclosure"}} {
		if p := by[id]; p.Char != want[0] || p.Position != want[1] {
			t.Errorf("%s: %s %s, want %s %s", id, p.Char, p.Position, want[0], want[1])
		}
	}
	if _, ok := by["u4e59-02"]; ok {
		t.Error("a variant no parent uses became a part")
	}
	var want vector.Vector
	want.Add(vector.Reading("チ"), 1) // 池 contains 也
	want.Add(vector.Reading("ソ"), 1) // 咀 contains 池
	want.Add(vector.Reading("ヤ"), 1) // 也 itself
	want.Add(vector.Reading("ヤ"), 3) // its own reading
	want.Normalize()
	if c := vector.Cos(by["u4e5f-02"].OnVector, want); c < 0.9999 {
		t.Errorf("也 sound vector off by cos %g", c)
	}
}

// Test ID: DICT-10
// A glyph becomes a frame once three parents draw the rest of themselves
// inside it, within 15% of its box and covering at least 3% of its area; the
// room it leaves is the median of those uses.
func TestFrameDetection(t *testing.T) {
	newest, all, chars := fixture()
	newest["zfull"] = "1:0:0:0:0:200:0$1:0:0:0:200:200:200" // fills exactly the box it is placed in
	for _, c := range []struct {
		frame   string
		parents []string
		inners  []string
	}{
		{"u5182", []string{"u4e00", "u4e8c"}, []string{"40:40:160:160", "40:40:160:160"}},                           // two uses
		{"u531a", []string{"u4e09", "u56db", "u4e94"}, []string{"90:90:110:110", "90:90:110:110", "90:90:110:110"}}, // under 3% of the area
		{"u5338", []string{"u516d", "u4e03", "u516b"}, []string{"5:40:160:160", "5:40:160:160", "5:40:160:160"}},    // 25 past a 21 tolerance
		{"u51f5", []string{"u4e5d", "u5341", "u5343"}, []string{"15:40:160:160", "40:40:160:160", "60:40:160:160"}}, // 15 past, median at 40
	} {
		newest[c.frame] = box
		for i, p := range c.parents {
			newest[p+"-j"] = "99:0:0:0:0:200:200:" + c.frame + "$99:0:0:" + c.inners[i] + ":zfull"
			chars[charOf(p)] = "イチ"
		}
	}
	d, e := Build(archive(t, newest, all), kanjidic(t, chars), []byte("{}"))
	if e != nil {
		t.Fatal(e)
	}
	by := parts(d)
	for _, n := range []string{"u5182", "u531a", "u5338"} {
		if _, ok := by[n+"#frame"]; ok {
			t.Errorf("%s became a frame", n)
		}
	}
	f, ok := by["u51f5#frame"]
	if !ok || f.Inner == nil || f.FrameUses != 3 {
		t.Fatalf("凵 frame %+v", f)
	}
	// The box is 30..170; the uses start at 15, 40 and 60, so the median is 40.
	if want := math.Round(10.0/140*1000) / 1000; f.Inner.X != want {
		t.Errorf("inner x %g, want the median %g", f.Inner.X, want)
	}
}

// Test ID: DICT-11
// Overrides exclude a part or set its position, operator and inner box, and
// the dictionary version changes with them.
func TestOverridesAndVersion(t *testing.T) {
	newest, all, chars := fixture()
	gw, kd := archive(t, newest, all), kanjidic(t, chars)
	ov := []byte(`{"u4e5f-02":{"position":"left"},"u56d7-j#frame":{"operator":"⿵","inner":{"X":0.1,"Y":0.2,"W":0.5,"H":0.6}},"u2ea1":{"exclude":true}}`)
	d, e := Build(gw, kd, ov)
	if e != nil {
		t.Fatal(e)
	}
	by := parts(d)
	if p := by["u4e5f-02"]; p.Position != "left" {
		t.Errorf("也 position %q", p.Position)
	}
	if f := by["u56d7-j#frame"]; f.Operator != "⿵" || f.Inner == nil || *f.Inner != (model.Box{X: 0.1, Y: 0.2, W: 0.5, H: 0.6}) {
		t.Errorf("囗 frame %s %+v", f.Operator, f.Inner)
	}
	if _, ok := by["u2ea1"]; ok {
		t.Error("excluded ⺡ is still a part")
	}
	again, e := Build(gw, kd, ov)
	if e != nil {
		t.Fatal(e)
	}
	plain, e := Build(gw, kd, []byte("{}"))
	if e != nil {
		t.Fatal(e)
	}
	if d.Version != again.Version || d.Version == plain.Version {
		t.Errorf("versions %s, %s, %s", d.Version, again.Version, plain.Version)
	}
	// A different dump or KANJIDIC2 is a different version too.
	newest["u4e5f"] = box
	otherDump, e := Build(archive(t, newest, all), kd, []byte("{}"))
	if e != nil {
		t.Fatal(e)
	}
	chars["本"] = "ホン"
	otherKanjidic, e := Build(gw, kanjidic(t, chars), []byte("{}"))
	if e != nil {
		t.Fatal(e)
	}
	if otherDump.Version == plain.Version || otherKanjidic.Version == plain.Version {
		t.Errorf("version ignores an input: %s, %s, %s", plain.Version, otherDump.Version, otherKanjidic.Version)
	}
}

// Test ID: DICT-12
// A dictionary with no parent to draw a phonetic from fails the build, even
// though the legend glyphs are there.
func TestNoPhoneticCandidates(t *testing.T) {
	newest, all, chars := fixture()
	es := map[string]entry{}
	for c, r := range chars {
		es[c] = entry{on: r} // none in JIS X 0208 or graded
	}
	_, e := Build(archive(t, newest, all), kanjidicOf(t, es), []byte("{}"))
	if e == nil || !strings.Contains(e.Error(), "no phonetic candidates") {
		t.Fatalf("got %v", e)
	}
}
