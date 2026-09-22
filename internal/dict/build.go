package dict

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"kanji-gacha/internal/kana"
	"kanji-gacha/internal/model"
	"kanji-gacha/internal/render"
	"kanji-gacha/internal/vector"
	"sort"
	"strconv"
	"strings"
)

type Group struct {
	ID       string  `xml:"id,attr"`
	Element  string  `xml:"element,attr"`
	Original string  `xml:"original,attr"`
	Position string  `xml:"position,attr"`
	Part     string  `xml:"part,attr"`
	Number   string  `xml:"number,attr"`
	Partial  string  `xml:"partial,attr"`
	Phon     string  `xml:"phon,attr"`
	Groups   []Group `xml:"g"`
	Paths    []Path  `xml:"path"`
}
type Path struct {
	ID string `xml:"id,attr"`
	D  string `xml:"d,attr"`
}
type Override struct {
	Exclude  bool       `json:"exclude"`
	Position string     `json:"position"`
	Operator string     `json:"operator"`
	Inner    *model.Box `json:"inner"`
	Alias    string     `json:"alias"`
}

func allPaths(g Group) []Path {
	m := map[string]Path{}
	var walk func(Group)
	walk = func(g Group) {
		for _, p := range g.Paths {
			m[p.ID] = p
		}
		for _, c := range g.Groups {
			walk(c)
		}
	}
	walk(g)
	out := make([]Path, 0, len(m))
	for _, p := range m {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool {
		a, _ := strconv.Atoi(out[i].ID[strings.LastIndex(out[i].ID, "-s")+2:])
		b, _ := strconv.Atoi(out[j].ID[strings.LastIndex(out[j].ID, "-s")+2:])
		return a < b
	})
	return out
}
func Build(kvg, kdic, overrides []byte) (Dictionary, error) {
	d := Dictionary{Sources: map[string]string{}, Report: map[string]int{}}
	for n, b := range map[string][]byte{"kanjivg": kvg, "kanjidic2": kdic, "overrides": overrides} {
		h := sha256.Sum256(b)
		d.Sources[n] = hex.EncodeToString(h[:])
	}
	d.Version = d.Sources["kanjivg"][:12] + "-" + d.Sources["kanjidic2"][:12] + "-" + d.Sources["overrides"][:8]
	var ov map[string]Override
	if e := json.Unmarshal(overrides, &ov); e != nil {
		return d, e
	}
	gz, e := gzip.NewReader(bytes.NewReader(kdic))
	if e != nil {
		return d, e
	}
	defer gz.Close()
	var kd struct {
		Characters []struct {
			Literal  string `xml:"literal"`
			Readings []struct {
				Type  string `xml:"r_type,attr"`
				Value string `xml:",chardata"`
			} `xml:"reading_meaning>rmgroup>reading"`
		} `xml:"character"`
	}
	if e = xml.NewDecoder(gz).Decode(&kd); e != nil {
		return d, e
	}
	on, kun := map[string][]Reading{}, map[string][]Reading{}
	for _, c := range kd.Characters {
		for _, r := range c.Readings {
			if r.Type != "ja_on" && r.Type != "ja_kun" {
				continue
			}
			ps := strings.SplitN(strings.Trim(r.Value, "-"), ".", 2)
			rd := Reading{Text: kana.Normalize(ps[0])}
			if len(ps) == 2 {
				rd.Okurigana = kana.Normalize(ps[1])
			}
			if _, err := kana.Morae(rd.Text); err != nil {
				continue
			}
			if r.Type == "ja_on" {
				on[c.Literal] = append(on[c.Literal], rd)
			} else {
				kun[c.Literal] = append(kun[c.Literal], rd)
			}
		}
	}
	archive, e := zip.NewReader(bytes.NewReader(kvg), int64(len(kvg)))
	if e != nil {
		return d, e
	}
	type acc struct {
		On, Kun vector.Vector
		Sources map[string]bool
	}
	stats := map[string]*acc{}
	counts := map[string]int{}
	total := 0
	sort.Slice(archive.File, func(i, j int) bool { return archive.File[i].Name < archive.File[j].Name })
	for _, f := range archive.File {
		if !strings.Contains(f.Name, "/kanji/") || !strings.HasSuffix(f.Name, ".svg") || strings.Contains(strings.TrimSuffix(f.Name[strings.LastIndex(f.Name, "/")+1:], ".svg"), "-") {
			continue
		}
		r, e := f.Open()
		if e != nil {
			return d, e
		}
		var svg struct {
			Groups []Group `xml:"g"`
		}
		e = xml.NewDecoder(r).Decode(&svg)
		r.Close()
		if e != nil {
			return d, fmt.Errorf("%s: %w", f.Name, e)
		}
		var root Group
		found := false
		for _, g := range svg.Groups {
			if strings.Contains(g.ID, "StrokePaths") && len(g.Groups) > 0 {
				root = g.Groups[0]
				found = true
				break
			}
		}
		if !found {
			continue
		}
		total++
		var visit func(Group)
		visit = func(g Group) {
			if g.Element != "" && g.Part == "" && g.Partial != "true" {
				o := ov[g.ID]
				if !o.Exclude {
					ps := allPaths(g)
					var paths []model.Stroke
					valid := len(ps) > 0
					for _, p := range ps {
						stroke, err := render.ParsePath(p.D)
						if err != nil {
							valid = false
							break
						}
						paths = append(paths, stroke)
					}
					if valid {
						alias := g.Element
						if g.Original != "" {
							alias = g.Original
						}
						if o.Alias != "" {
							alias = o.Alias
						}
						pos := g.Position
						if o.Position != "" {
							pos = o.Position
						}
						if pos == "" {
							pos = "unknown"
						}
						if g.ID == root.ID {
							pos = "whole"
						}
						a := stats[alias]
						if a == nil {
							a = &acc{Sources: map[string]bool{}}
							stats[alias] = a
						}
						if !a.Sources[root.Element] {
							weight := float32(1)
							if g.Phon != "" {
								weight = 2
							}
							for _, rd := range on[root.Element] {
								a.On.Add(vector.Reading(rd.Text), weight)
							}
							for _, rd := range kun[root.Element] {
								a.Kun.Add(vector.Reading(rd.Text), 1)
							}
							a.Sources[root.Element] = true
						}
						key := g.Element + ":" + pos
						if counts[key] < 3 || pos == "whole" || o.Inner != nil {
							counts[key]++
							d.Parts = append(d.Parts, Part{ID: g.ID, Char: g.Element, Original: alias, Source: root.Element, Position: pos, Paths: paths, Bounds: render.Bounds(paths), On: on[alias], Kun: kun[alias], Inner: o.Inner, Operator: o.Operator})
						}
					} else {
						d.Report["invalidPaths"]++
					}
				}
			} else if g.Partial == "true" {
				d.Report["partial"]++
			}
			merged := map[string][]Group{}
			for _, child := range g.Groups {
				if child.Part != "" {
					key := child.Element + ":" + child.Number
					merged[key] = append(merged[key], child)
				} else {
					visit(child)
				}
			}
			keys := make([]string, 0, len(merged))
			for k := range merged {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				gs := merged[k]
				valid := len(gs) > 1
				seen := map[string]bool{}
				for _, x := range gs {
					if seen[x.Part] || x.Partial == "true" {
						valid = false
					}
					seen[x.Part] = true
				}
				if !valid {
					d.Report["ambiguousParts"] += len(gs)
					continue
				}
				m := gs[0]
				m.Part = ""
				m.Paths = nil
				m.Groups = nil
				for _, x := range gs {
					m.Paths = append(m.Paths, allPaths(x)...)
				}
				visit(m)
				d.Report["mergedParts"]++
			}
		}
		visit(root)
	}
	for i := range d.Parts {
		p := &d.Parts[i]
		a := stats[p.Original]
		p.OnVector = a.On
		p.KunVector = a.Kun
		for _, r := range p.On {
			p.OnVector.Add(vector.Reading(r.Text), 3)
		}
		for _, r := range p.Kun {
			p.KunVector.Add(vector.Reading(r.Text), 3)
		}
		p.OnVector.Normalize()
		p.KunVector.Normalize()
		p.Frequency = float64(len(a.Sources)) / float64(total)
		if p.Position == "unknown" {
			d.Report["unknownPosition"]++
		}
		if p.OnVector != (vector.Vector{}) || p.KunVector != (vector.Vector{}) {
			d.Report["withReadings"]++
		}
	}
	d.Report["sourceCharacters"] = total
	d.Report["parts"] = len(d.Parts)
	if len(d.Parts) == 0 {
		return d, fmt.Errorf("empty dictionary")
	}
	if e := verifySpecials(&d, ov); e != nil {
		return d, e
	}
	return d, nil
}

// LegendChars are the whole-glyph assets the special slots draw from; a build
// without them would fail only at request time.
var LegendChars = []string{"彁", "妛", "雲", "龍"}

// verifySpecials fails the build when an enabled special slot has no drawing
// asset, or when an override no longer matches any part.
func verifySpecials(d *Dictionary, ov map[string]Override) error {
	whole, used, frames := map[string]bool{}, map[string]bool{}, 0
	for i := range d.Parts {
		p := &d.Parts[i]
		if p.Position == "whole" {
			whole[p.Char] = true
		}
		used[p.ID] = true
		if p.Inner != nil && p.Operator != "" {
			frames++
		}
	}
	for _, c := range LegendChars {
		if !whole[c] {
			return fmt.Errorf("legend asset missing: no whole part for %q", c)
		}
	}
	ids := make([]string, 0, len(ov))
	for id := range ov {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		o := ov[id]
		if o.Inner != nil && o.Operator == "" {
			return fmt.Errorf("override %s sets inner without operator", id)
		}
		if !o.Exclude && !used[id] {
			return fmt.Errorf("override %s matched no part", id)
		}
	}
	if frames == 0 {
		return fmt.Errorf("no frame part with both inner and operator")
	}
	d.Report["frameParts"] = frames
	return nil
}
