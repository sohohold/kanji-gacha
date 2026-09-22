"""Subset a locally downloaded Noto Sans CJK JP font and outline U+30EDE."""
import sys
import gzip
import json
from pathlib import Path
from fontTools.ttLib import TTFont
from fontTools import subset
from fontTools.pens.svgPathPen import SVGPathPen
from fontTools.pens.boundsPen import BoundsPen
from fontTools.pens.transformPen import TransformPen

root = Path(__file__).resolve().parents[1]
font = TTFont(sys.argv[1])
glyphs = font.getGlyphSet()
name = font.getBestCmap()[0x30EDE]
bounds = BoundsPen(glyphs)
glyphs[name].draw(bounds)
x0, y0, x1, y1 = bounds.bounds
scale = min(97 / (x1 - x0), 97 / (y1 - y0))
pen = SVGPathPen(glyphs)
glyphs[name].draw(TransformPen(pen, (scale, 0, 0, -scale, 54.5 - (x0+x1)*scale/2, 54.5 + (y0+y1)*scale/2)))
(root / 'internal/render/assets/biang.svg').write_text('<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 109 109" role="img" aria-label="ビャン"><title>ビャン</title><metadata>Noto Sans CJK JP © Adobe, SIL Open Font License 1.1; subset and converted to outlines.</metadata><g fill="#151a17" stroke="none" stroke-width="0"><path d="' + pen.getCommands() + '"/></g></svg>')
text = '架空漢字ガチャ読みからまだ見ぬ一画の未登録作品生成回音訓履歴共有結果意味'
text += (root / 'web/src/main.tsx').read_text() + (root / 'web/src/kana.ts').read_text()
codes = set(range(0x20, 0x100)) | set(range(0x3000, 0x3100)) | {ord(c) for c in text} | {0x30EDE, 0xFF08, 0xFF09, 0x2014}
opts = subset.Options()
opts.name_IDs = ['*']
sub = subset.Subsetter(options=opts)
sub.populate(unicodes=codes)
sub.subset(font)
font.save(root / 'internal/render/assets/NotoSansJP.otf')
# Include every source character in browser text, where IDS strings are dynamic.
font = TTFont(sys.argv[1])
dictionary = json.loads(gzip.decompress((root / 'internal/dict/dictionary.json.gz').read_bytes()))
codes.update(range(0x2FF0, 0x3000))
for part in dictionary['Parts']:
    codes.update(ord(c) for c in part['Char'])
sub = subset.Subsetter(options=opts)
sub.populate(unicodes=codes)
sub.subset(font)
font.save(root / 'web/public/kanji-ui.otf')
