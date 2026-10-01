# 架空漢字ガチャ

読みから漢字の部品を組み替えるWebアプリ。意味・実在判定は提供しません。生成の再現性には依存せず、返した一字を保存して共有します。

## ローカル起動

Go 1.26以降、Node.js 22.12以降、pnpm 11が必要です。辞書とOGP用フォントは同梱済みで、起動時のダウンロードはありません。

```sh
cd web
pnpm install --frozen-lockfile
pnpm run build
cd ..
go run ./cmd/api
```

<http://localhost:8080> を開きます。生成結果は `.local/data/glyphs/`、OGPは `.local/data/og/` に保存します。サーバーを再起動しても共有URLは使えます。

フロント開発は `cd web && pnpm dev`（APIは8080で別途起動）。OGPメタタグ込みの共有ページ確認は、ビルド後に8080へアクセスしてください。

DockerとMinIOでS3経路を確認する場合：

```sh
docker compose up --build
```

ローカル専用のMinIO資格情報を `compose.yaml` に記載しています。本番ではIAM実行ロールを使用します。第2フェーズの奉納・DynamoDBはまだ含みません。

## 機能

- 音／訓の入力（音読みは2モーラ以下）、NFKC・カナ正規化、厳密なJSONと文字列検証。
- GlyphWikiとKANJIDIC2から構築した辞書、256次元の音特徴、温度付き抽選。読みで選んだ音符1字に、読みと無関係に抽選した意符（偏・冠・脚・垂・繞・構）を重ねる形声文字型の生成。
- 左右・上下配置と手動指定した囲み・垂、N〜URの複雑性、200msの探索予算と退避候補。
- 幽霊文字・タイトの特殊枠、JSTの時刻条件。
- KAGEエンジン（Goへの移植）で肉付けした明朝体のSVG、生成直後に1画ずつ現れるアニメーション、結果表示時のbounceIn・rubberBandとレアリティ別の候補から1種類を選んだ絵文字リアクション。
- ローカル／S3の保存、共有ページ、遅延生成して保存する1200×630 PNG。
- React・TanStack Query、IME対応、モバイル表示、共有・コピー。画面は[98.css](https://jdan.github.io/98.css/)のウィンドウで、ホーム（アプリ名・入力・ボタン・ライセンス）と結果（字形・リンク・ベース・ライセンス）の縦一列。
- ダーク画面、動きを減らす設定への対応、データ提供元とライセンスの表記。

`config/generator.json` の上限・配置寸法・温度・抽選確率を変更して調整できます。設定の読み込みは起動時です。全レイアウト演算子の網羅、分割部品の完全復元、高密度字形の見栄えは引き続き調整対象です。

## 設定

| 環境変数 | 初期値・用途 |
| --- | --- |
| `LISTEN_ADDR` | `:8080` |
| `BASE_URL` | `http://localhost:8080`。共有・OGPの公開URL |
| `WEB_DIR` | `web/dist`。ビルド済みHTML・アセット |
| `DATA_DIR` | `.local/data`。ローカル保存先 |
| `S3_BUCKET` | 設定するとローカル保存からS3へ切り替え |
| `S3_ENDPOINT` | MinIO等の検証用。AWSでは指定しない |
| `AWS_REGION` | AWS/MinIO利用時は `ap-northeast-1` 等を設定 |
| `GENERATOR_CONFIG` | 未設定なら同梱した `config/generator.json` |
| `GENERATION_ENABLED` | `false` で新規生成だけ停止 |
| `TRUST_CLOUDFRONT` | `true` はOACでオリジンを限定したAWS環境だけ。IP制限で転送ヘッダーを参照 |
| `METRICS_NAMESPACE` | 設定すると生成時間・レアリティ分布をCloudWatch埋め込みメトリクスとして標準出力へ書く |

## 検証

CI（`.github/workflows/ci.yml`）と同じ手順をローカルで実行できます。

```sh
go vet ./...
go test -race ./... # Cコンパイラが必要
go test ./internal/kana -run '^$' -fuzz FuzzDecode -fuzztime 30s
cd web
pnpm install --frozen-lockfile
pnpm run types && git diff --exit-code -- src/api.generated.ts
pnpm run format:check && pnpm run build && pnpm run test
pnpm exec playwright install --with-deps chromium
# リポジトリのルートで go run ./cmd/api を起動してから（WEB_DIR の既定値は web/dist）
pnpm run e2e
```

Playwrightのブラウザが別の場所にある場合は `CHROMIUM_PATH` で指定します。

GoとTSは `shared/kana-cases.json` の同じ受理／拒否例を検証します。APIテストは保存失敗、同時OGP要求、保存済み結果の取得を含みます。E2Eは生成→共有→再表示、OGP、IME、不正入力、訓読み、特殊枠、モバイルを確認します。

`api/openapi.yaml` が公開APIの契約です。変更したら `cd web && pnpm run types` で `web/src/api.generated.ts` を再生成してコミットしてください（CIが差分を検査します）。Goハンドラは現時点では手書きで、oapi-codegenへの移行とスキーマ自動適合検証は未実施です。

## 辞書・フォントの更新

[data/README.md](data/README.md) を参照してください。部品の補正は `data/component-overrides.json`。再構築後は `data/sources.lock.json` の件数とチェックサム、字形をレビューします。表示結果の再現用に古い生成器を保持する必要はありません。

## AWS

[infra/README.md](infra/README.md) に、stateバケット・OIDCデプロイロール・Budgetsを作るbootstrap構成、ECR・Lambda・CloudFront・S3のアプリ構成、GitHub Actionsのデプロイ（`.github/workflows/deploy.yml` と `scripts/deploy.sh`）の手順を記載しています。2026-09-23に両構成を適用し、GitHub Actionsからのデプロイまで動作しています。費用は仕様書7.2の仮定を実測で更新してください。

## ライセンス

コードは GPL v3（`LICENSE`）です。`internal/kage` は KAGE エンジン（上地宏一、kurgm による TypeScript 版 `@kurgm/kage-engine`、GPL v3）を Go に移植したものです。

## データのライセンス

字形は [GlyphWiki](https://glyphwiki.org/) のデータ（改変・商用を含め自由に利用・再配布可、著作者表示の条件なし）を使います。読みは KANJIDIC2 © James William Breen and EDRDG / CC BY-SA 4.0 です。派生辞書は両データを抽出・変換したものでCC BY-SA 4.0として配布します。

GlyphWikiへ移行する前（2026-09-24以前）に保存された生成字は、KanjiVG © Ulrich Apel / CC BY-SA 3.0 の中心線を描いたSVGで、CC BY-SA 3.0です。その字を表示する画面とOGP画像には KanjiVG の表記を出します。

OGPと画面の文字はNoto Sans CJK JP © Adobe / SIL OFL 1.1をサブセットして使用しています。ライセンス本文は `licenses/` と `internal/render/assets/OFL.txt` にあります。画面のスタイルは98.css © Jordan Scales（MIT、`licenses/98.css.txt`）を使用しています。
