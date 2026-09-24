# Third-party data

- GlyphWiki glyph data. Free to use, modify and redistribute, including commercially, without warranty; no attribution required. https://glyphwiki.org/ — the dump's `LICENSE.txt`, quoted in `../data/README.md`.
- KAGE engine © Koichi Kamichi; TypeScript port @kurgm/kage-engine © kurgm. GPL v3. https://github.com/kurgm/kage-engine — ported to Go in `../internal/kage`; the license is `../LICENSE`.
- KanjiVG © 2009–2026 Ulrich Apel. CC BY-SA 3.0. https://kanjivg.tagaini.net/ — `KanjiVG.txt`. No longer used to build the dictionary; glyphs saved before the move to GlyphWiki are KanjiVG strokes and are still served.
- KANJIDIC2 © James William Breen and the Electronic Dictionary Research and Development Group. CC BY-SA 4.0. https://www.edrdg.org/edrdg/licence.html — `EDRDG.html`.
- Noto Sans CJK JP © 2014–2021 Adobe (http://www.adobe.com/). SIL Open Font License 1.1. https://github.com/notofonts/noto-cjk — `../internal/render/assets/OFL.txt`.
- 98.css © 2020 Jordan Scales. MIT License. https://github.com/jdan/98.css — `98.css.txt`.

The dictionary combines GlyphWiki's KAGE data and reading statistics; meanings are not imported. Components are rearranged and drawn with the KAGE engine for generated glyphs. The font is subsetted for the UI/OGP. These modifications do not imply endorsement by the source authors.
