// Package kana defines the public input grammar. Dictionary lookup is not validation.
package kana

import (
	"encoding/json"
	"fmt"
	"golang.org/x/text/unicode/norm"
	"strings"
	"unicode/utf8"
)

const Basic = "アイウエオカキクケコサシスセソタチツテトナニヌネノハヒフヘホマミムメモヤユヨラリルレロワヲンガギグゲゴザジズゼゾダヂヅデドバビブベボパピプペポヴヰヱ"
const Small = "ァィゥェォャュョ"

type Input struct {
	Mode      string `json:"mode"`
	Reading   string `json:"reading"`
	Okurigana string `json:"okurigana,omitempty"`
}
type Reading struct {
	Mode      string   `json:"mode"`
	Reading   string   `json:"reading"`
	Okurigana string   `json:"okurigana"`
	Display   string   `json:"display"`
	Morae     []string `json:"morae"`
}
type Issue struct {
	Field      string `json:"field"`
	Code       string `json:"code"`
	Position   *int   `json:"position,omitempty"`
	Normalized string `json:"normalized,omitempty"`
}

func (e *Issue) Error() string { return fmt.Sprintf("%s: %s", e.Field, e.Code) }
func Normalize(s string) string {
	return strings.Map(func(r rune) rune {
		if r >= 'ぁ' && r <= 'ゖ' {
			return r + 0x60
		}
		return r
	}, norm.NFKC.String(s))
}
func Hiragana(s string) string {
	return strings.Map(func(r rune) rune {
		if r >= 'ァ' && r <= 'ヶ' {
			return r - 0x60
		}
		return r
	}, s)
}
func pair(a, b rune) bool {
	return strings.ContainsRune("キギシジチヂニヒビピミリ", a) && strings.ContainsRune("ャュョ", b) || strings.ContainsRune("イウヴシジチツフテデトドクグ", a) && strings.ContainsRune("ァィゥェォ", b) || strings.ContainsRune("テデフヴ", a) && b == 'ュ'
}
func Morae(s string) ([]string, *Issue) {
	rs := []rune(s)
	out := []string{}
	fail := func(i int, code string) ([]string, *Issue) {
		return nil, &Issue{Code: code, Position: &i, Normalized: s}
	}
	for i := 0; i < len(rs); i++ {
		r := rs[i]
		if !strings.ContainsRune(Basic+Small+"ッー", r) {
			return fail(i, "character")
		}
		if i == 0 && strings.ContainsRune(Small+"ッーン", r) {
			return fail(i, "leading")
		}
		if strings.ContainsRune(Small, r) {
			return fail(i, "small_kana")
		}
		if i > 0 && rs[i-1] == 'ッ' && strings.ContainsRune("ッンー", r) {
			return fail(i, "sequence")
		}
		if r == 'ー' && i > 0 && (rs[i-1] == 'ン' || rs[i-1] == 'ッ') {
			return fail(i, "sequence")
		}
		if i+1 < len(rs) && pair(r, rs[i+1]) {
			out = append(out, string(rs[i:i+2]))
			i++
		} else {
			out = append(out, string(r))
		}
	}
	return out, nil
}

// Charset reports the first rune outside the allowed set, so that spec 2.1 can
// check the character set before the length.
func Charset(s string) *Issue {
	for i, r := range []rune(s) {
		if !strings.ContainsRune(Basic+Small+"ッー", r) {
			at := i
			return &Issue{Code: "character", Position: &at, Normalized: s}
		}
	}
	return nil
}
func Validate(in Input) (Reading, *Issue) {
	if in.Mode != "on" && in.Mode != "kun" {
		return Reading{}, &Issue{Field: "mode", Code: "enum"}
	}
	for _, f := range []struct{ name, value string }{{"reading", in.Reading}, {"okurigana", in.Okurigana}} {
		if !utf8.ValidString(f.value) {
			return Reading{}, &Issue{Field: f.name, Code: "encoding"}
		}
	}
	r := Reading{Mode: in.Mode, Reading: Normalize(in.Reading), Okurigana: Normalize(in.Okurigana)}
	// spec 2.1 orders the checks as character set, then length, then ordering.
	for _, f := range []struct{ name, value string }{{"reading", r.Reading}, {"okurigana", r.Okurigana}} {
		if issue := Charset(f.value); issue != nil {
			issue.Field = f.name
			return r, issue
		}
	}
	n, o := utf8.RuneCountInString(r.Reading), utf8.RuneCountInString(r.Okurigana)
	if n < 1 || n > 5 {
		return r, &Issue{Field: "reading", Code: "length"}
	}
	if o > 3 || in.Mode == "on" && o != 0 {
		return r, &Issue{Field: "okurigana", Code: "length"}
	}
	if n+o > 6 {
		return r, &Issue{Field: "okurigana", Code: "total_length"}
	}
	var issue *Issue
	r.Morae, issue = Morae(r.Reading)
	if issue != nil {
		issue.Field = "reading"
		return r, issue
	}
	if _, issue = Morae(r.Okurigana); issue != nil {
		issue.Field = "okurigana"
		return r, issue
	}
	r.Display = r.Reading
	if r.Mode == "kun" {
		r.Display = Hiragana(r.Reading)
		if o > 0 {
			r.Display += "（" + Hiragana(r.Okurigana) + "）"
		}
	}
	return r, nil
}
func (r Reading) Key() string {
	b, _ := json.Marshal([]string{r.Mode, r.Reading, r.Okurigana})
	return string(b)
}
