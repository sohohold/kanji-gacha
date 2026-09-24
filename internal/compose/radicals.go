package compose

import (
	"math"
	"math/rand/v2"
	"sort"

	"kanji-gacha/internal/dict"
	"kanji-gacha/internal/model"
)

// 意符として外側に付ける部品。読みとは無関係に抽選し、形声文字の「意味側」を担わせる。
// 字で持ち、辞書の位置別の部品（偏・旁・冠・脚は位置別の変形部品、垂・繞・構は囲いの部品）を引き当てる。
// 辞書にないものは読み込み時に読み飛ばす。
var radicalChars = map[string][]string{
	"left":   {"氵", "亻", "扌", "木", "口", "糸", "言", "金", "忄", "日", "月", "土", "女", "火", "石", "虫", "目", "王", "禾", "足", "車", "馬", "魚", "貝", "米", "衤", "礻", "⺨", "⻖", "彳", "弓", "山", "舟", "耳", "酉", "革", "冫", "歹", "矢", "方", "片", "巾", "立", "子", "羊", "鳥", "骨", "角"},
	"right":  {"刂", "攵", "力", "頁", "欠", "殳", "隹", "彡", "斤", "卩", "戈", "寸", "鳥", "見", "阝", "⻏", "邑"},
	"top":    {"艹", "宀", "竹", "雨", "穴", "罒", "冖", "爫", "山", "⺌", "覀", "癶", "耂"},
	"bottom": {"心", "灬", "皿", "貝", "衣", "儿", "廾", "木", "女", "糸", "手", "虫", "土", "⺗", "氺", "馬", "鳥"},
	"tare":   {"广", "厂", "疒", "尸", "戸", "虍"},
	"nyo":    {"辶", "⻌", "廴", "走", "鬼", "風"},
	"kamae":  {"門", "囗", "匚", "勹", "冂", "气", "鬥"},
}

type frameSpec struct {
	op    string
	inner model.Box
}

// 構・垂・繞の内側に音符を置く矩形（部品の外接矩形に対する比率）。辞書は親字での
// 使われ方から内側の矩形を求めるので、これは親字で囲いとして使われていない部品（戸など）の代わり。
var frameSpecs = map[string]frameSpec{
	"tare": {"⿸", model.Box{X: 0.3, Y: 0.3, W: 0.68, H: 0.68}},
	"nyo":  {"⿺", model.Box{X: 0.34, Y: 0, W: 0.64, H: 0.74}},
}

type radical struct {
	char, position string
	variants       []*dict.Part
	weight         float64
}

// frame reports the operator and inner box a frame radical uses.
func (r *radical) frame(p *dict.Part) (frameSpec, bool) {
	if p.Inner != nil && p.Operator != "" {
		return frameSpec{p.Operator, *p.Inner}, true
	}
	s, ok := frameSpecs[r.position]
	return s, ok
}

// span is the extent across the attachment axis; variants taken from the main
// split of their source character are the tallest (偏・旁) or widest (冠・脚).
func span(p *dict.Part, position string) float64 {
	switch position {
	case "left", "right":
		return p.Bounds.H
	case "top", "bottom":
		return p.Bounds.W
	}
	return p.Bounds.W * p.Bounds.H
}

// isFrame reports whether a radical position is a 構・垂・繞.
func isFrame(position string) bool {
	return position == "tare" || position == "nyo" || position == "kamae"
}

func buildRadicals(parts []dict.Part) []*radical {
	byKey := map[[2]string][]*dict.Part{}
	for i := range parts {
		p := &parts[i]
		byKey[[2]string{p.Char, p.Position}] = append(byKey[[2]string{p.Char, p.Position}], p)
	}
	var out []*radical
	for position, chars := range radicalChars {
		for _, c := range chars {
			if !isFrame(position) {
				vs := byKey[[2]string{c, position}]
				if len(vs) == 0 {
					continue
				}
				sort.SliceStable(vs, func(i, j int) bool { return span(vs[i], position) > span(vs[j], position) })
				out = append(out, &radical{char: c, position: position, variants: vs, weight: weight(vs[0])})
				continue
			}
			// 囲いは親字でよく使われる形から順に、内側の矩形があるものだけを使う。
			r := &radical{char: c, position: position}
			for _, v := range byKey[[2]string{c, "enclosure"}] {
				if _, ok := r.frame(v); ok {
					r.variants = append(r.variants, v)
				}
			}
			if len(r.variants) == 0 {
				continue
			}
			sort.SliceStable(r.variants, func(i, j int) bool { return r.variants[i].FrameUses > r.variants[j].FrameUses })
			r.weight = weight(r.variants[0])
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].position != out[j].position {
			return out[i].position < out[j].position
		}
		return out[i].char < out[j].char
	})
	return out
}

// weight は出現頻度の平方根。氵のような定番を多めにしつつ珍しい部首も残す。
func weight(p *dict.Part) float64 { return math.Sqrt(math.Max(p.Frequency, 0.002)) }

// 位置の種類ごとの出やすさ。偏を主役にし、構・垂・繞は控えめにする。
var positionWeights = map[string]float64{"left": 0.5, "top": 0.18, "right": 0.1, "bottom": 0.1, "tare": 0.05, "nyo": 0.04, "kamae": 0.03}

// pickRadical draws a position kind first, then a radical of that kind by
// weight, skipping characters already used.
func pickRadical(rs []*radical, used map[string]bool, rng *rand.Rand) *radical {
	total := 0.0
	for _, w := range positionWeights {
		total += w
	}
	x := rng.Float64() * total
	position := "left"
	for _, k := range []string{"left", "top", "right", "bottom", "tare", "nyo", "kamae"} {
		x -= positionWeights[k]
		if x <= 0 {
			position = k
			break
		}
	}
	if r := pickIn(rs, position, used, rng); r != nil {
		return r
	}
	return pickIn(rs, "", used, rng)
}

func pickIn(rs []*radical, position string, used map[string]bool, rng *rand.Rand) *radical {
	sum := 0.0
	for _, r := range rs {
		if !used[r.char] && (position == "" || r.position == position) {
			sum += r.weight
		}
	}
	if sum == 0 {
		return nil
	}
	x := rng.Float64() * sum
	for _, r := range rs {
		if used[r.char] || position != "" && r.position != position {
			continue
		}
		x -= r.weight
		if x <= 0 {
			return r
		}
	}
	return nil
}

// variant picks one of the two best-shaped variants for variety.
func (r *radical) variant(rng *rand.Rand) *dict.Part {
	return r.variants[rng.IntN(min(2, len(r.variants)))]
}
