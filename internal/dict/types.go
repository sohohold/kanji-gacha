package dict

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"kanji-gacha/internal/model"
	"kanji-gacha/internal/vector"
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

func Encode(d Dictionary) ([]byte, error) {
	var b bytes.Buffer
	z := gzip.NewWriter(&b)
	if e := json.NewEncoder(z).Encode(d); e != nil {
		return nil, e
	}
	if e := z.Close(); e != nil {
		return nil, e
	}
	return b.Bytes(), nil
}
func Decode(b []byte) (Dictionary, error) {
	var d Dictionary
	z, e := gzip.NewReader(bytes.NewReader(b))
	if e != nil {
		return d, e
	}
	defer z.Close()
	e = json.NewDecoder(io.LimitReader(z, 128<<20)).Decode(&d)
	return d, e
}
