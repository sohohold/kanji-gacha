package config

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
)

//go:embed generator.json
var defaults []byte

type Rarity struct {
	Name        string  `json:"name"`
	Weight      int     `json:"weight"`
	MinParts    int     `json:"minParts"`
	MaxParts    int     `json:"maxParts"`
	MaxDepth    int     `json:"maxDepth"`
	Temperature float64 `json:"temperature"`
}
type Config struct {
	Version     int      `json:"version"`
	Canvas      float64  `json:"canvas"`
	Margin      float64  `json:"margin"`
	Gap         float64  `json:"gap"`
	StrokeWidth float64  `json:"strokeWidth"`
	MinSlot     float64  `json:"minSlot"`
	MinRatio    float64  `json:"minRatio"`
	MaxRatio    float64  `json:"maxRatio"`
	MaxParts    int      `json:"maxParts"`
	MaxDepth    int      `json:"maxDepth"`
	MaxStrokes  int      `json:"maxStrokes"`
	Candidates  int      `json:"candidates"`
	Workers     int      `json:"workers"`
	TimeoutMs   int      `json:"timeoutMs"`
	TopK        int      `json:"topK"`
	Alpha       float64  `json:"alpha"`
	Beta        float64  `json:"beta"`
	Gamma       float64  `json:"gamma"`
	Rarities    []Rarity `json:"rarities"`
}

func Load(path string) (Config, error) {
	b := defaults
	var err error
	if path != "" {
		b, err = os.ReadFile(path)
		if err != nil {
			return Config{}, err
		}
	}
	var c Config
	if err = json.Unmarshal(b, &c); err != nil {
		return c, err
	}
	if c.Canvas <= 0 || c.Margin < c.StrokeWidth/2 || c.Margin*2 >= c.Canvas || c.Gap < 0 || c.StrokeWidth <= 0 || c.MinSlot <= 0 || c.MinRatio <= 0 || c.MaxRatio >= 1 || c.MinRatio > c.MaxRatio || c.MaxParts < 1 || c.MaxParts > 64 || c.MaxDepth < 1 || c.MaxDepth > 10 || c.MaxStrokes < 1 || c.MaxStrokes > 512 || c.Candidates < 1 || c.Candidates > 1024 || c.Workers < 1 || c.Workers > 8 || c.TimeoutMs < 1 || c.TimeoutMs > 5000 || c.TopK < 1 {
		return c, fmt.Errorf("invalid generator limits")
	}
	total := 0
	for _, r := range c.Rarities {
		if r.Name == "" || r.Weight <= 0 || r.MinParts < 1 || r.MaxParts < r.MinParts || r.MaxParts > c.MaxParts || r.MaxDepth < 1 || r.MaxDepth > c.MaxDepth || r.MaxParts > 1<<r.MaxDepth || r.Temperature <= 0 {
			return c, fmt.Errorf("invalid rarity %s", r.Name)
		}
		total += r.Weight
	}
	if total != 10000 {
		return c, fmt.Errorf("rarity weights must sum to 10000")
	}
	return c, nil
}
