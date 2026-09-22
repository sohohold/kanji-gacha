package kana

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"unicode/utf8"
)

// Decode rejects encodings that encoding/json would silently repair, and duplicate keys.
func Decode(b []byte) (Input, int, error) {
	if !utf8.Valid(b) || !validSurrogates(b) {
		return Input{}, 400, fmt.Errorf("invalid Unicode encoding")
	}
	d := json.NewDecoder(bytes.NewReader(b))
	t, e := d.Token()
	if e != nil || t != json.Delim('{') {
		return Input{}, 400, fmt.Errorf("expected JSON object")
	}
	raw := map[string]json.RawMessage{}
	for d.More() {
		t, e = d.Token()
		if e != nil {
			return Input{}, 400, e
		}
		k, ok := t.(string)
		if !ok {
			return Input{}, 400, fmt.Errorf("invalid key")
		}
		if _, ok = raw[k]; ok {
			return Input{}, 400, fmt.Errorf("duplicate key")
		}
		var v json.RawMessage
		if e = d.Decode(&v); e != nil {
			return Input{}, 400, e
		}
		raw[k] = v
	}
	if _, e = d.Token(); e != nil {
		return Input{}, 400, e
	}
	if _, e = d.Token(); e != io.EOF {
		return Input{}, 400, fmt.Errorf("trailing JSON")
	}
	in := Input{}
	fields := map[string]*string{"mode": &in.Mode, "reading": &in.Reading, "okurigana": &in.Okurigana}
	for k, v := range raw {
		dest, ok := fields[k]
		if !ok {
			return in, 422, &Issue{Field: k, Code: "unknown"}
		}
		if bytes.Equal(bytes.TrimSpace(v), []byte("null")) || json.Unmarshal(v, dest) != nil {
			return in, 422, &Issue{Field: k, Code: "type"}
		}
	}
	for _, k := range []string{"mode", "reading"} {
		if _, ok := raw[k]; !ok {
			return in, 422, &Issue{Field: k, Code: "required"}
		}
	}
	return in, 0, nil
}
func validSurrogates(b []byte) bool {
	for i := 0; i < len(b); i++ {
		if b[i] != '\\' {
			continue
		}
		i++
		if i >= len(b) {
			return false
		}
		if b[i] != 'u' {
			continue
		}
		if i+4 >= len(b) {
			return false
		}
		n, e := strconv.ParseUint(string(b[i+1:i+5]), 16, 16)
		if e != nil {
			return false
		}
		i += 4
		if n >= 0xDC00 && n <= 0xDFFF {
			return false
		}
		if n >= 0xD800 && n <= 0xDBFF {
			if i+6 >= len(b) || b[i+1] != '\\' || b[i+2] != 'u' {
				return false
			}
			m, e := strconv.ParseUint(string(b[i+3:i+7]), 16, 16)
			if e != nil || m < 0xDC00 || m > 0xDFFF {
				return false
			}
			i += 6
		}
	}
	return true
}
