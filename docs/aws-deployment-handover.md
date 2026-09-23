# AWSデプロイ整備の引き継ぎ（2026-09-23）

## 今回の到達点と次の作業

AWS認証エラーを解消し、ユーザーのターミナルで `terraform plan` が成功した。`infra/README.md` の未実装4項目（GitHub Actions用OIDCロール、applyを含むデプロイワークフロー、Budgets通知、EMF監視）は、コードを調査した結果、すべて実装可能と回答した。

「今の構成を先にapplyするか、実装を進めるか」という相談には、stateの共有化も含めて実装を先に進め、その後に初期構築をapplyする順序を推奨した。ユーザーの直近の依頼は使用量上限に備えた引き継ぎドキュメントの作成であり、4項目の実装・AWSへのapplyはこの会話では行っていない。推奨した進め方と実施済み作業を混同しないこと。

プロジェクトの `AGENTS.md` により、サブエージェントは禁止。調査・実装・レビュー・検証はメインエージェントが直接行う。

## 確認済みの状態

- リポジトリ：`git@github.com:sohohold/kanji-gacha.git`
- AWSアカウント：`473536070013`
- 配置先リージョン：`ap-northeast-1`
- AWS CLIプロファイル：`kanji-gacha`
- ユーザーが提示した認証先：`arn:aws:iam::473536070013:user/root`
  - これは名前が `root` のIAMユーザーを示すARNで、AWSアカウントのルートユーザーARNではない。作成権限の詳細は未確認。
- ローカルで確認したTerraform：`1.11.4`
- `infra/main.tf` のTerraform制約：`>= 1.6.0`
- `infra/.terraform.lock.hcl` のAWS Provider：`6.66.0`（制約 `~> 6.0`）
- Terraform stateはローカル保存。リモートバックエンドは未実装。
- `.github/workflows/ci.yml` は存在するが、デプロイワークフローはない。
- 引き継ぎ文書作成前の `git status --short` は空だった。再開時には改めて確認する。

### 認証について

ユーザーはIAM Identity Center（SSO）を設定していない。最初に案内した `aws configure sso` は使わず、次のブラウザ認証でログインできた。

```bash
aws login --profile kanji-gacha --region ap-northeast-1 --remote
export AWS_PROFILE=kanji-gacha
aws sts get-caller-identity
```

`aws login` はAWS CLI 2.32.0以降の機能。セッションが切れたら再ログインする。AWS skills / MCPの追加設定を尋ねられた際は `n` でよいと案内した。

以前のエージェント環境では `aws` コマンド・認証設定が見つからず、sudoも制限されていた。その後のインストール・ログイン・plan成功はユーザーのターミナルから報告されたもの。次のエージェントが同じ認証を利用できるとは限らないため、実行環境を確認する。

`infra/README.md` にはこの会話で追加した認証手順があるが、SSOと既存アクセスキーの説明が中心で、成功した `aws login` の経路はまだ反映していない。次の文書更新で補う。

### 成功したplan

ユーザー添付の出力では `data.aws_caller_identity.current` の読み取りが成功し、結果は次のとおり。

```text
Plan: 14 to add, 0 to change, 0 to destroy.
```

内訳：ECRリポジトリ・ポリシー・ライフサイクル、S3バケット2個・公開ブロック2個、Lambda実行ロール・ポリシー、ロググループ、CloudFrontキャッシュポリシー・オリジンリクエストポリシー・OAC2個。

`image_uri=""` の初期構成なので、Lambda関数・Function URL・CloudFront distributionはまだplanに含まれない。plan成功は認証と計画生成の確認であり、作成権限やアプリの実動作まで検証したものではない。保存済みplan（`-out`）もない。会話中にapply成功の報告はない。

## 推奨する実装順序

### 1. state保存先とOIDCの初期構築

- アプリ用stateをS3へ保存し、ロックを有効化する。S3のネイティブロック（`use_lockfile = true`）を使うならTerraformの最低バージョンを1.10以上へ更新し、CIと揃える。
- stateバケットは非公開・暗号化・バージョニングを設定。アプリ用バケットとは分離する。
- state保存先自体とOIDCロールは、手元の認証で初回構築する。アプリの通常デプロイから独立したbootstrap構成を検討し、そのstateの保管方法も文書化する。
- 既にstateやAWSリソースが存在する場合は移行・importを検討する。今回のplanだけでアカウント全体が空だとは判断しない。
- GitHub用OIDC Providerが既にあるか読み取りで確認し、既存なら再利用する。
- ロールの信頼条件は `sohohold/kanji-gacha` の対象ブランチまたはGitHub Environmentへ限定し、`aud` も指定する。Environmentを使う場合はOIDCの `sub` の形式がブランチ指定と異なる。
- ECR push、Terraform管理リソース、state、S3同期、CloudFront無効化に必要な権限を設計する。`iam:PassRole` は対象のLambda実行ロールへ限定する。

### 2. デプロイワークフロー

仕様書ではmainへのpushで実行する想定。現在のCIと連携させ、テスト失敗時にはデプロイしない構成を設計する。

流れ：OIDC認証 → 初期リソースの確認・構築 → arm64イメージのbuild/push → digestを指定してplan/apply → 公開URLの反映 → 同じイメージから取り出した静的ファイルをS3へ同期 → CloudFront無効化 → 疎通確認。

注意点：

- ECRはタグ上書き不可。コミットSHAだけでは再実行時に衝突しうるので、イメージの再利用または一意のタグを設計する。
- Lambda用buildには現手順の `--platform linux/arm64 --provenance=false` を引き継ぐ。
- 初回はCloudFront URL確定後に `base_url` を反映する2段階構成。途中の `localhost` の共有URLを公開完了扱いにしない。
- 更新時に `image_uri`・`base_url` を省略して既定値へ戻さない。空の `image_uri` はLambda等の削除計画につながる。
- 静的ファイルは必ずデプロイするイメージから抽出する。手元で別途ビルドした `web/dist` は使わない。
- 旧HTMLが参照するハッシュ付きアセットは、キャッシュ寿命を超えるまで保持する。
- stateロックに加えActionsの同時デプロイも制御する。apply途中の安易なキャンセルを避ける。

### 3. Budgets通知

- `spec.md` 7.3の要件は月1 USD超過時のメール通知。通知先メールアドレスは未提供。
- 月額・通知先を変数として実装できる。アカウント全体を対象にするか、このアプリだけにするかは未決定。アプリ限定ならコスト配分タグ等の設計が必要。
- 通知は利用停止機能ではない。既存の `generation_enabled` による新規生成停止とは別。

### 4. EMF監視

- `spec.md` の要件は生成時間・レアリティ分布。実装箇所候補は `internal/httpapi/server.go` の生成処理と `cmd/api/main.go` の初期化。
- EMF形式のJSONをログへ出し、CloudWatchでメトリクスとして抽出させる。通常の `log.Printf` の日時プレフィックス等でEMFのJSON形式を壊さないこと。
- 入力本文・SVG・IP・生成IDをメトリクスのディメンションにしない。ディメンションの種類を抑え、カスタムメトリクス費用を考慮する。
- 生成時間の範囲（生成エンジンだけか保存込みか）を明確化し、失敗時・退避時の扱いもテストする。
- メトリクス出力、可視化、アラーム通知のどこまでを実装するかを明確にする。現時点ではアラーム閾値は未決定。

## 検証と完了条件

- Terraform：format、validate、実際の認証でplanし、意図した追加・変更だけか確認する。
- Go変更：EMFのJSON構造・計測範囲・失敗時の扱いをテストし、`go vet ./...` と `go test -race ./...` を実行する。
- ワークフロー：YAML、OIDC条件、変数の受け渡し、初回と更新・再実行を検証する。
- AWS側：OIDCでのロール引受、Actions経由のデプロイ、Budgets設定、CloudWatchのメトリクス抽出を確認する。
- CloudFront経由のPOST→GET→共有HTML→OGP、POST本文ハッシュ有無・改変、Function URL直接呼び出しの拒否を確認する。
- その他の実測残件（閲覧者IP、退避率、コールドスタート、S3競合）は既存の [handover.md](handover.md) を参照する。

## 参照先

- [配置手順](../infra/README.md)、[Terraform](../infra/main.tf)、[仕様書](../spec.md)、[既存CI](../.github/workflows/ci.yml)
- [AWS CLIのブラウザ認証](https://docs.aws.amazon.com/cli/latest/userguide/cli-configure-sign-in.html)
- [GitHub ActionsのAWS OIDC](https://docs.github.com/en/actions/how-tos/secure-your-work/security-harden-deployments/oidc-in-aws)
- [Terraform S3 backend](https://developer.hashicorp.com/terraform/language/backend/s3)
- [Terraform Budgets](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/budgets_budget)
- [CloudWatch EMF仕様](https://docs.aws.amazon.com/en_en/AmazonCloudWatch/latest/monitoring/CloudWatch_Embedded_Metric_Format_Specification.html)
