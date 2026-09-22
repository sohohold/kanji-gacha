package model

import (
	"kanji-gacha/internal/kana"
	"time"
)

type Point struct{ X, Y float64 }
type Curve struct{ Start, C1, C2, End Point }
type Stroke []Curve
type Box struct{ X, Y, W, H float64 }
type Node struct {
	Operator    string  `json:"operator,omitempty"`
	ComponentID string  `json:"componentId,omitempty"`
	Char        string  `json:"char,omitempty"`
	Children    []*Node `json:"children,omitempty"`
}

func (n *Node) IDS() string {
	if n == nil {
		return ""
	}
	s := n.Operator
	if s == "" {
		return n.Char
	}
	for _, c := range n.Children {
		s += c.IDS()
	}
	return s
}

type Component struct {
	ID    string  `json:"id"`
	Char  string  `json:"char"`
	Role  string  `json:"role"`
	Score float64 `json:"score"`
}
type Glyph struct {
	ID                string       `json:"id"`
	SchemaVersion     int          `json:"schemaVersion"`
	GeneratorVersion  int          `json:"generatorVersion"`
	ConfigVersion     int          `json:"configVersion"`
	DictionaryVersion string       `json:"dictionaryVersion"`
	Key               string       `json:"normalizedKey"`
	Reading           kana.Reading `json:"reading"`
	CreatedAt         time.Time    `json:"createdAt"`
	Rarity            string       `json:"rarity"`
	IDS               string       `json:"ids"`
	Strokes           int          `json:"strokes"`
	SVG               string       `json:"svg"`
	Components        []Component  `json:"components"`
	Tree              *Node        `json:"tree"`
	Legend            *string      `json:"legend"`
	OGPKey            string       `json:"ogpKey"`
	OGPReading        string       `json:"ogpReading"`
	License           string       `json:"license"`
	Fallback          bool         `json:"fallback"`
}
