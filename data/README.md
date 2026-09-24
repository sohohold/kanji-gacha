# 辞書と出典

現在の埋め込み辞書は6,704字から12,855件の部品バリアントを抽出。分割グループ880組を統合、曖昧なグループ951件と部分字形355件を除外しました。数値は `sources.lock.json` のビルド報告が正です。

- KanjiVG: https://github.com/KanjiVG/kanjivg / 使用commit `422b5538595676da918c288a4230cb5e22a1ee7e`。取得したZIP全体のSHA-256はロックファイルに記録。現在の辞書は同commitをgitで取得し、`kanji/` ディレクトリをZIP化して構築しました（ルート名とSHA-256はZIPの作り方で変わります）。
- KANJIDIC2: https://www.edrdg.org/kanjidic/kanjidic2.xml.gz / 2026-09-23取得。配布元は更新されるので、再ビルド時はSHA-256差分を確認してください。
- 読み以外のKANJIDIC2の意味・各種辞書番号は取り込みません。
- 部品の所属が曖昧なら除外し、同じ字を含む別の完全なグループは残します。
- 中間データは `internal/dict/dictionary.gob.gz` にgzip圧縮したGoのgob形式で同梱します。JSONより起動時の読み込みが約4分の1で済むためです。gobはGo以外から読めないので、辞書の字の一覧が必要なときは `go run ./cmd/builddict -chars` を使います。

更新は仕様上の再現性保証とは独立です。配布元の更新条件に従い、公開運用中は少なくとも月次で辞書更新を確認してください。

```sh
mkdir -p .local/sources
curl -fL https://codeload.github.com/KanjiVG/kanjivg/zip/422b5538595676da918c288a4230cb5e22a1ee7e -o .local/sources/kanjivg.zip
curl -fL https://www.edrdg.org/kanjidic/kanjidic2.xml.gz -o .local/sources/kanjidic2.xml.gz
go run ./cmd/builddict -kanjivg .local/sources/kanjivg.zip -kanjidic .local/sources/kanjidic2.xml.gz
go test ./internal/dict ./internal/compose ./internal/render
```

辞書は `go:embed` されるため、空のリポジトリからではなく、同梱辞書が存在するチェックアウトで更新してください。生成後にバイナリを再ビルドします。

## フォント素材

OGP用の `internal/render/assets/NotoSansJP.otf` はNoto Sans CJK JP RegularからASCII・かな・必要な見出し文字等に絞った静的OTF。画面用の `web/public/kanji-ui.otf` には辞書内の漢字とIDS記号も含めています。辞書更新後の再構築は `scripts/build-font.py` を使います。スクリプトは辞書の字を `go run ./cmd/builddict -chars` で得るので、Goが使える環境で実行してください。

```sh
uv run --with fonttools python scripts/build-font.py /path/to/NotoSansCJKjp-Regular.otf
```

出典: https://github.com/notofonts/noto-cjk/tree/main/Sans/OTF/Japanese
