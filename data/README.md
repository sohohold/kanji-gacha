# 辞書と出典

現在の埋め込み辞書は、GlyphWiki の親字6,458字から9,431件の部品（全体字形6,458、偏873、旁1,065、冠400、脚289、囲い346）を作り、11,902グリフの KAGE データを同梱しています。数値は `sources.lock.json` のビルド報告が正です。構築の手順は spec.md の第3.1節にあります。

- GlyphWiki: https://glyphwiki.org/dump.tar.gz の2026-09-24版。SHA-256 `67edffab0e177249ae7435487b695a42558ce898a369b718aa5732f72ae5b80a`。ダンプは毎日作り直され、過去の版は配布元から取得できないので、構築に使ったものをこのリポジトリのリリース [`sources-2026-09-24`](https://github.com/sohohold/kanji-gacha/releases/tag/sources-2026-09-24) に保管しています。データは改変・商用を含めて自由に利用・再配布でき、著作者表示の条件もありません（同梱の `LICENSE.txt`）。
- KANJIDIC2: https://www.edrdg.org/kanjidic/kanjidic2.xml.gz の database_version 2026-267。SHA-256 `601de9d3d7100e522e0d3b615973768680c7ec6ad4869874c8361efc8755bf3a`。同じリリースに保管しています。配布元は更新されるので、再ビルド時はSHA-256差分を確認してください。
- 読み以外のKANJIDIC2の意味・各種辞書番号は取り込みません。画数は KANJIDIC2 の値を使います。
- 部品の補正は `component-overrides.json` に部品の ID（GlyphWiki のグリフ名。親字の全体字形を囲いとして使うものは `名前#frame`）ごとに書きます。項目は `exclude`・`position`・`operator`・`inner`（囲いの内側の矩形。部品の外接矩形に対する比率）です。今は勹（`u52f9-05`）の IDS 演算子を ⿹ に直す1件だけです。自動判定では左払いが左の辺を閉じるとみなされて ⿵ になるためです。
- 中間データは `internal/dict/dictionary.gob.gz` にgzip圧縮したGoのgob形式で同梱します。gobはGo以外から読めないので、辞書の字の一覧が必要なときは `go run ./cmd/builddict -chars` を使います。

更新は仕様上の再現性保証とは独立です。配布元の更新条件に従い、公開運用中は少なくとも月次で辞書更新を確認してください。新しいダンプで作り直すときは、使ったダンプと KANJIDIC2 を新しいリリース（`sources-YYYY-MM-DD`）に保管し、この文書の版とチェックサムを書き換えます。

```sh
mkdir -p .local/sources
gh release download sources-2026-09-24 -D .local/sources
go run ./cmd/builddict -glyphwiki .local/sources/glyphwiki-dump.tar.gz -kanjidic .local/sources/kanjidic2.xml.gz
go test ./internal/dict ./internal/compose ./internal/render
```

構築には約40秒と約1.4GBのメモリがかかります（ダンプの最新版を読み込み、版を指定した参照のために全版のファイルをもう一度読むため）。辞書は `go:embed` されるため、空のリポジトリからではなく、同梱辞書が存在するチェックアウトで更新してください。生成後にバイナリを再ビルドし、生成結果の見本（レアリティごと、囲い・垂・繞、同字重ね、都市伝説枠）を目で確かめてください。

## KAGE エンジン

`internal/kage` は [@kurgm/kage-engine](https://github.com/kurgm/kage-engine) v0.7.0（KAGE エンジンの TypeScript 版、GPL v3）の明朝体の描画を Go に移植したものです。参照実装で描いた14,172件の座標（`internal/kage/testdata/golden.json.gz`）と全件で一致することをテストしています。参照実装を更新したときは、`internal/kage/testdata/gen/package.json` の版を上げ、`cd internal/kage/testdata/gen && npm ci && node gen.mjs` でテストデータを作り直して差分を確かめます。

## フォント素材

OGP用の `internal/render/assets/NotoSansJP.otf` はNoto Sans CJK JP RegularからASCII・かな・必要な見出し文字等に絞った静的OTF。画面用の `web/public/kanji-ui.otf` には辞書内の漢字とIDS記号も含めています。辞書更新後の再構築は `scripts/build-font.py` を使います。スクリプトは辞書の字を `go run ./cmd/builddict -chars` で得るので、Goが使える環境で実行してください。

```sh
uv run --with fonttools python scripts/build-font.py /path/to/NotoSansCJKjp-Regular.otf
```

出典: https://github.com/notofonts/noto-cjk/tree/main/Sans/OTF/Japanese
