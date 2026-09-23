package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestRejectInvalidRarityDepth(t *testing.T) {
	c, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	for _, depth := range []int{-1, 0, 100} {
		c.Rarities[0].MaxDepth = depth
		data, err := json.Marshal(c)
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(t.TempDir(), "generator.json")
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(path); err == nil {
			t.Errorf("accepted depth %d", depth)
		}
	}
}

func TestRejectUnknownRarityName(t *testing.T) {
	c, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	c.Rarities[0].Name = "LR"
	data, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "generator.json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Error("accepted rarity name outside the API enum")
	}
}

func TestRejectUnreachablePartCount(t *testing.T) {
	c, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	c.MaxParts = 16
	last := len(c.Rarities) - 1
	c.Rarities[last].MaxDepth = c.MaxDepth
	c.Rarities[last].MaxParts = MaxReachableParts(c.MaxDepth) + 1
	data, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "generator.json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Errorf("accepted %d parts at depth %d", c.Rarities[last].MaxParts, c.MaxDepth)
	}
}
