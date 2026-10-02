package dict

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"kanji-gacha/internal/kage"
	"kanji-gacha/internal/model"
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

func kanjidic(t *testing.T, chars map[string]string) []byte {
	t.Helper()
	var b bytes.Buffer
	b.WriteString("<kanjidic2>")
	for c, r := range chars {
		fmt.Fprintf(&b, `<character><literal>%s</literal><codepoint><cp_value cp_type="jis208">1-1-1</cp_value></codepoint><misc><stroke_count>5</stroke_count></misc><reading_meaning><rmgroup><reading r_type="ja_on">%s</reading></rmgroup></reading_meaning></character>`, c, r)
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

// Test ID: DICT-01
func TestBuild(t *testing.T) {
	newest := map[string]string{
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
	all := map[string]string{"u56d7-j@1": cross, "u56d7-j@2": box + "$" + cross}
	chars := map[string]string{"国": "コク", "困": "コン", "因": "イン", "玉": "ギョク", "木": "モク", "大": "ダイ", "池": "チ", "也": "ヤ", "彁": "カ", "妛": "シ", "雲": "ウン", "龍": "リュウ"}
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
