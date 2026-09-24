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
type Part struct {
	ID        string
	Char      string
	Original  string
	Source    string
	Position  string
	Paths     []model.Stroke
	Bounds    model.Box
	On        []Reading
	Kun       []Reading
	OnVector  vector.Vector
	KunVector vector.Vector
	Frequency float64
	Inner     *model.Box `json:",omitempty"`
	Operator  string     `json:",omitempty"`
}
type Dictionary struct {
	Version string
	Parts   []Part
	Sources map[string]string
	Report  map[string]int
}

// file is the stored form of a Dictionary. gob writes map entries in
// iteration order, so the maps are stored as key-sorted slices to keep the
// same build input producing the same bytes.
type file struct {
	Version string
	Parts   []Part
	Sources []source
	Report  []count
}
type source struct{ Name, SHA256 string }
type count struct {
	Name string
	N    int
}

// Encode stores the dictionary as gzip-compressed gob. Decoding it takes about
// a quarter of the time JSON took, which is most of the Lambda cold start.
func Encode(d Dictionary) ([]byte, error) {
	f := file{Version: d.Version, Parts: d.Parts}
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
	d := Dictionary{Version: f.Version, Parts: f.Parts, Sources: map[string]string{}, Report: map[string]int{}}
	for _, s := range f.Sources {
		d.Sources[s.Name] = s.SHA256
	}
	for _, c := range f.Report {
		d.Report[c.Name] = c.N
	}
	return d, nil
}
