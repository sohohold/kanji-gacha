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

## 現在動く機能

- 音／訓の入力、NFKC・カナ正規化、厳密なJSONと文字列検証。
- KanjiVGとKANJIDIC2から構築した辞書、256次元の音特徴、分節DP、温度付き抽選。
- 左右・上下配置と手動指定した囲み・垂、N〜URの複雑性、200msの探索予算と退避候補。
- 幽霊文字・タイト・ビャンの特殊枠、JSTの時刻条件。
- Go側で座標変換したSVG、描画再生、ブラウザ履歴20件。
- ローカル／S3の保存、共有ページ、遅延生成して保存する1200×630 PNG。
- React・TanStack Query、IME対応、モバイル表示、共有・コピー・X投稿リンク。
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

## 検証

```sh
go test ./...
go test -race ./... # Cコンパイラが必要
go vet ./...
cd web
pnpm run test
pnpm run build
pnpm exec playwright install --with-deps chromium
# 別ターミナルで go run ./cmd/api を起動してから
pnpm run e2e
```

GoとTSは `shared/kana-cases.json` の同じ受理／拒否例を検証します。APIテストは保存失敗、同時OGP要求、保存済み結果の取得を含みます。E2Eは生成→共有→再表示、OGP、IME、不正入力、訓読み、特殊枠、モバイルを確認します。

`api/openapi.yaml` が公開APIの契約です。TS型の再生成は `cd web && pnpm run types`。Goハンドラは現時点では手書きで、oapi-codegenへの移行とスキーマ自動適合検証は未実施です。

## 辞書・フォントの更新

[data/README.md](data/README.md) を参照してください。部品の補正は `data/component-overrides.json`。再構築後は `data/sources.lock.json` の件数とチェックサム、字形をレビューします。表示結果の再現用に古い生成器を保持する必要はありません。

## AWS

[infra/README.md](infra/README.md) にECR・Lambda・CloudFront・S3の構築手順を記載しています。VPC接続はありません。Terraform、Dockerfile、CIは用意していますが、AWSへのデプロイ・実環境でのOAC検証はまだ行っていません。費用は仕様書7.2の仮定を実測で更新してください。

公開前の残作業：実環境のOAC・POSTハッシュ検証、字形の目視調整、NGワード辞書の拡充（現在2語の最小実装）、Budgets通知、EMF計測、デプロイ用OIDCロール／ワークフロー。PGO・夜間fuzz・カバレッジ閾値・静的lintの整備は別途行います。

## データのライセンス

KanjiVG © Ulrich Apel / CC BY-SA 3.0、KANJIDIC2 © James William Breen and EDRDG / CC BY-SA 4.0。派生辞書は両データを抽出・変換したものでCC BY-SA 4.0として配布します。KanjiVGから生成したSVGはCC BY-SA 3.0です。

OGPの文字とビャン字形はNoto Sans CJK JP © Adobe / SIL OFL 1.1をサブセット・アウトライン変換して使用しています。ビャンの字形は筆順を持たないため再生しません。ライセンス本文は `licenses/` と `internal/render/assets/OFL.txt` にあります。
