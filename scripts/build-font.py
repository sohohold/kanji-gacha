"""Subset a locally downloaded Noto Sans CJK JP font."""
import subprocess
import sys
from pathlib import Path
from fontTools.ttLib import TTFont
from fontTools import subset

root = Path(__file__).resolve().parents[1]
font = TTFont(sys.argv[1])
text = '架空漢字ガチャ読みからまだ見ぬ一画の未登録作品生成回音訓履歴共有結果意味'
text += (root / 'web/src/main.tsx').read_text() + (root / 'web/src/kana.ts').read_text()
codes = set(range(0x20, 0x100)) | set(range(0x3000, 0x3100)) | {ord(c) for c in text} | {0xFF08, 0xFF09, 0x2014}
opts = subset.Options()
opts.name_IDs = ['*']
sub = subset.Subsetter(options=opts)
sub.populate(unicodes=codes)
sub.subset(font)
font.save(root / 'internal/render/assets/NotoSansJP.otf')
# Include every source character in browser text, where IDS strings are dynamic.
font = TTFont(sys.argv[1])
# The dictionary is gob, so ask the Go tool for its characters.
chars = subprocess.run(['go', 'run', './cmd/builddict', '-chars'], cwd=root, check=True, capture_output=True, text=True).stdout
codes.update(range(0x2FF0, 0x3000))
codes.update(ord(c) for c in chars.rstrip('\n'))
sub = subset.Subsetter(options=opts)
sub.populate(unicodes=codes)
sub.subset(font)
font.save(root / 'web/public/kanji-ui.otf')
