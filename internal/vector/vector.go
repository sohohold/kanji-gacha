package vector

import (
	"hash/fnv"
	"kanji-gacha/internal/kana"
	"math"
	"strings"
)

const D = 256

type Vector [D]float32

func (v *Vector) add(s string, w float32) { h := fnv.New32a(); h.Write([]byte(s)); v[h.Sum32()%D] += w }
func (v *Vector) Normalize() {
	var n float64
	for _, x := range v {
		n += float64(x * x)
	}
	if n == 0 {
		return
	}
	n = math.Sqrt(n)
	for i := range v {
		v[i] /= float32(n)
	}
}
func (v *Vector) Add(other Vector, w float32) {
	for i, x := range other {
		v[i] += x * w
	}
}
func Cos(a, b Vector) float64 {
	var s float64
	for i, x := range a {
		s += float64(x * b[i])
	}
	return s
}
func Encode(morae []string) Vector {
	var v Vector
	for i, m := range morae {
		v.add("m:"+m, 1)
		if i == 0 {
			v.add("head:"+m, 0.6)
		}
		if i > 0 {
			v.add("pair:"+morae[i-1]+m, 0.5)
		}
		rs := []rune(m)
		r := rs[0]
		base := r
		for _, group := range []string{"カガ", "キギ", "クグ", "ケゲ", "コゴ", "サザ", "シジ", "スズ", "セゼ", "ソゾ", "タダ", "チヂ", "ツヅ", "テデ", "トド", "ハバパ", "ヒビピ", "フブプ", "ヘベペ", "ホボポ"} {
			if strings.ContainsRune(group, r) {
				base = []rune(group)[0]
				if r != base {
					v.add("voiced", 0.2)
				}
			}
		}
		for row, g := range []string{"アイウエオ", "カキクケコ", "サシスセソ", "タチツテト", "ナニヌネノ", "ハヒフヘホ", "マミムメモ", "ヤユヨ", "ラリルレロ", "ワヲ", "ヴ"} {
			if strings.ContainsRune(g, base) {
				v.add("row:"+string(rune('a'+row)), 0.8)
			}
		}
		tail := base
		if len(rs) > 1 {
			tail = rs[len(rs)-1]
		}
		for col, g := range []string{"アカサタナハマヤラワァャ", "イキシチニヒミリヰィ", "ウクスツヌフムユルヴゥュ", "エケセテネヘメレヱェ", "オコソトノホモヨロヲォョ"} {
			if strings.ContainsRune(g, tail) {
				v.add("vowel:"+string(rune('a'+col)), 0.8)
			}
		}
		if len(rs) > 1 {
			v.add("small", 0.2)
		}
		if strings.ContainsAny(m, "ンッー") {
			v.add("special:"+m, 0.3)
		}
	}
	v.Normalize()
	return v
}
func Reading(s string) Vector {
	m, e := kana.Morae(kana.Normalize(s))
	if e != nil {
		return Vector{}
	}
	return Encode(m)
}
