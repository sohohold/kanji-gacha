package main

import (
	"encoding/json"
	"flag"
	"kanji-gacha/internal/dict"
	"log"
	"os"
)

func main() {
	kvg := flag.String("kanjivg", "", "KanjiVG zip")
	kd := flag.String("kanjidic", "", "KANJIDIC2 xml.gz")
	ov := flag.String("overrides", "data/component-overrides.json", "component overrides")
	out := flag.String("out", "internal/dict/dictionary.json.gz", "output")
	flag.Parse()
	read := func(p string) []byte {
		b, e := os.ReadFile(p)
		if e != nil {
			log.Fatal(e)
		}
		return b
	}
	d, e := dict.Build(read(*kvg), read(*kd), read(*ov))
	if e != nil {
		log.Fatal(e)
	}
	b, e := dict.Encode(d)
	if e != nil {
		log.Fatal(e)
	}
	if e = os.WriteFile(*out, b, 0644); e != nil {
		log.Fatal(e)
	}
	json.NewEncoder(os.Stdout).Encode(d.Report)
	manifest, _ := json.MarshalIndent(map[string]any{"version": d.Version, "sha256": d.Sources, "report": d.Report}, "", "  ")
	if e = os.WriteFile("data/sources.lock.json", append(manifest, '\n'), 0644); e != nil {
		log.Fatal(e)
	}
}
