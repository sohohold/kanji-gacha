# 残件の引き継ぎ

b94cd83（初期実装）に対するコードレビュー10件のうち、仕様・テスト仕様とのすり合わせが不要な8件を PR #1 で修正した。レビューの過程で Codex から受けた指摘7件（いずれも PR #1 の変更が持ち込んだもの）も同じ PR で修正済みである。修正内容と、修正ごとに追加した回帰テストの一覧は PR #1 の本文にまとめてある。

本書はそのあとに残る作業を、判断の要るもの、本番環境でしか確かめられないもの、優先度の低い改善の順に記す。

## 2. 本番環境での実測が必要なもの

いずれもローカルの Go テストや MinIO では確かめられない。デプロイ後に CloudFront 経由で確認する。

### 2.1 `CloudFront-Viewer-Address` の到達と形式

レート制限のキーは `internal/httpapi/server.go` の `viewerIP` で `CloudFront-Viewer-Address` から取り出している。このヘッダーは `infra/main.tf` の origin request policy に追加済みだが、Lambda Function URL まで実際に届くかは未確認である。届かなければ全利用者が `RemoteAddr`（Web Adapter 経由では常に 127.0.0.1）の1バケットを共有し、正常な利用者が 429 を受ける。

IPv6 の値に括弧が付くか（`[2001:db8::1]:52011` か `2001:db8::1:52011` か）も未確認である。`viewerIP` はどちらも解釈できるが、ポートのない裸の IPv6 は、ポート付きと区別できないため意図的に破棄して共有バケットへ落としている。ポートが16進グループとして成立する値（`2001:db8::1:443`）を丸ごとキーにすると接続ごとにバケットが変わり制限を回避できるため、この挙動は変えないこと（`TestRateLimitIgnoresViewerPort` が検査している）。

確認方法は、一時的に受信ヘッダーをログへ出し、IPv4 と IPv6 の両方の閲覧者から POST して値を見ること。

### 2.2 生成の 200ms 予算

PR #1 で、分節の DP を部品の枠数ごとに解き直すようにした（spec 3.3）。区間スコアはキャッシュを共有するので `rank` の呼び出しは区間ごとに1回だが、5モーラの読みでは区間が最大15になり、各区間で約1.3万部品を走査する。この計算は候補生成より前に同じ 200ms 予算の中で行われるので、Lambda（1024MB、約0.58 vCPU）で予算を食い潰すと候補がほとんど作られず、退避候補ばかりになる。

保存される結果 JSON の `fallback` の比率で確認できる。比率が高ければ `rank` の計測と、`config/generator.json` の `timeoutMs`・`candidates`・`workers` の調整が要る。

### 2.3 辞書のコールドスタート

`internal/dict/dictionary.json.gz`（約11MB）を起動時に展開・デコードしている。コールドスタートの所要時間とメモリ使用量を計測する。

### 2.4 S3 の条件付き PUT と OGP の競合

OGP 画像の初回同時要求で条件付き PUT が競合したとき、`rereadAfterConflict` は `ErrNotFound`（`NoSuchKey`）に限って 25ms から倍々で4回、合計約375ms 再試行し、勝者の書き込んだオブジェクトを返す。それ以外のエラーは 503 にする。これは、競合時に S3 が `ConditionalRequestConflict` か `PreconditionFailed` を返し、勝者の書き込みが約375ms 以内に読めるようになる、という前提に立っている。S3 と MinIO の両方で実際の挙動を確認する。

以前は再取得に失敗すると自前の描画結果を返していたが、ローリングデプロイ中はレンダラやフォントの版が異なるインスタンスが併存しうるため、保存済みオブジェクトと異なる画像を1年 immutable で固定しうる。自前の描画結果で代替する形には戻さないこと。

### 2.5 OAC 経由の POST

CloudFront の OAC で Lambda Function URL へ POST するときの `x-amz-content-sha256` によるハッシュ検証が、本文あり・なし・改変のそれぞれで想定どおり動くかを確認する。`infra/README.md` の手順8にある確認項目と同じである。

## 3. 優先度の低い改善

`infra/main.tf` の `/g/*` と `/og/*` の ordered_cache_behavior は全メソッドを許可している。サーバー側で正しい `Allow` 付きの 405 を返すようにしたので実害はないが、`["GET", "HEAD"]` に絞ればオリジンへの無駄な呼び出しが減る。`/api/*` は POST が要るので現状のままにする。

GitHub Actions で Node 20 の非推奨警告が出ている（`actions/checkout@v4`、`actions/setup-go@v5`、`actions/setup-node@v4`、`pnpm/action-setup@v4`）。現状は Node 24 で強制実行されて通っているが、各アクションの新しいメジャー版へ上げておくとよい。

特殊枠の素材が実行時に欠けたとき、`Generate` は 503 ではなく通常抽選へ退避するが、ログや計測を出していない。辞書ビルド時の `verifySpecials` が素材の欠落でビルドを失敗させるので、通常は起きない。

`infra/README.md` の「未実装」節にある、GitHub Actions の AWS OIDC デプロイロール、apply を含むデプロイワークフロー、Budgets 通知、EMF 監視は初期実装から持ち越している。

## 検証の手順

CI（`.github/workflows/`）と同じ手順をローカルで実行できる。

```bash
go vet ./...
go test -race ./...
go test ./internal/kana -run '^$' -fuzz FuzzDecode -fuzztime 30s

cd web
pnpm install --frozen-lockfile
pnpm run types && git diff --exit-code -- src/api.generated.ts
pnpm run format:check && pnpm run build && pnpm run test
```

e2e は、先に `pnpm run build` で `web/dist` を作り、リポジトリのルートで API サーバーを起動してから実行する（`WEB_DIR` の既定値は `web/dist`）。Playwright のブラウザが別の場所にある場合は `CHROMIUM_PATH` で指定する。

```bash
go build -o /tmp/kanji-api ./cmd/api
/tmp/kanji-api &
cd web && pnpm run e2e
```

`api/openapi.yaml` を変更したら、必ず `pnpm run types` で `web/src/api.generated.ts` を再生成してコミットすること。CI はこの差分を検査している。
