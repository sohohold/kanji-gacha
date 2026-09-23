# AWSデプロイ整備の引き継ぎ（2026-09-23）

## 到達点と次の作業

`infra/README.md` で未実装としていた4項目（GitHub Actions用OIDCロール、applyを含むデプロイワークフロー、Budgets通知、EMF監視）と、stateのS3共有化を実装した。コードとしての検証（Terraformのformat・validate、Goのテスト、ワークフローのlint、デプロイスクリプトの分岐）は済んでいるが、AWSへのapplyとGitHub側の設定はまだ行っていない。実装済みであることと、AWS上で動作を確認したことを混同しないこと。

次の作業は、ユーザーの認証で `infra/bootstrap` を適用してstateをS3へ移し、GitHubに `production` Environmentと変数 `AWS_DEPLOY_ROLE_ARN` を設定し、初回デプロイを実行して結果を確認すること。手順は [infra/README.md](../infra/README.md) の「初回セットアップ」にある。初回のデプロイでデプロイロールの権限不足（`AccessDenied`）が見つかる可能性があり、その場合は該当アクションを `infra/bootstrap/main.tf` に追加する。

プロジェクトの `AGENTS.md` により、サブエージェントは禁止。調査・実装・レビュー・検証はメインエージェントが直接行う。

## 確認済みの状態

- リポジトリ：`git@github.com:sohohold/kanji-gacha.git`
- AWSアカウント：`473536070013`、配置先リージョン：`ap-northeast-1`、AWS CLIプロファイル：`kanji-gacha`
- ユーザーが提示した認証先：`arn:aws:iam::473536070013:user/root`（名前が `root` のIAMユーザーで、ルートユーザーではない。IAM・Budgetsの作成権限は未確認）
- ユーザーのTerraform：`1.11.4`。構成の制約は `>= 1.10.0`（S3ネイティブロックのため引き上げた）。Actionsも1.11.4
- AWS Provider：`6.66.0`（制約 `~> 6.0`）。`infra/bootstrap/.terraform.lock.hcl` はアプリ側と同じ内容
- 認証は `aws login --profile kanji-gacha --region ap-northeast-1 --remote`（AWS CLI 2.32.0以降）で成功した。SSOは未設定。`infra/README.md` にこの経路を反映済み
- 以前の変更前の構成で `terraform plan` が `14 to add` で成功したことはユーザーの報告による。今回の変更後の構成ではAWS認証付きのplanを実行していない（この作業環境にはAWS認証がない）

## 実装した内容

### stateとbootstrap（`infra/bootstrap`）

stateバケット `kanji-gacha-473536070013-tfstate`（非公開・SSE-S3・バージョニング・旧版90日削除・HTTPSのみ・`prevent_destroy`）、GitHub OIDC Provider（既存があれば `github_oidc_provider_arn` で再利用）、デプロイロール `kanji-gacha-deploy`、Lambda実行ロールの権限境界 `kanji-gacha-lambda-boundary`、Budgetを作る。バケットを自ら作るため、初回は `local_override.tf` でローカルstateにして適用し、その後 `init -migrate-state` でS3の `bootstrap/terraform.tfstate` へ移す。アプリのstateは `app/terraform.tfstate`。バックエンドのバケット名は変数を使えないため `backend.tf` に直接書いた。

デプロイロールの信頼条件は `aud=sts.amazonaws.com` と `sub=repo:sohohold@94224267/kanji-gacha@1381573804:environment:production`。このリポジトリのOIDCトークンはオーナーとリポジトリのIDを含むイミュータブルな `sub` で発行される（`gh api repos/sohohold/kanji-gacha/actions/oidc/customization/sub` の `use_immutable_subject: true`）。当初は `repo:sohohold/kanji-gacha:...` で信頼条件を書いていたため、最初のデプロイで `AssumeRoleWithWebIdentity` が拒否された。接頭辞は変数 `github_sub_prefix` で持つ。ブランチの制限はGitHubのEnvironment側（Deployment branchesを `main` のみ）で行う前提で、その設定はまだされていない。権限はアプリ名のARNへ絞り、削除系は与えていない。CloudFrontはARNで絞れないため操作の種類だけを限定した。

Lambda実行ロールをデプロイロールが変更できると、インラインポリシー経由で権限を広げられる。これを防ぐため、アプリ側の `aws_iam_role.lambda` に権限境界を付け、デプロイロールは境界が付いている場合だけ `CreateRole`・`PutRolePolicy` 等を行えるようにした。そのためbootstrapをアプリ構成より先に適用する必要がある。

### デプロイ（`.github/workflows/deploy.yml`、`scripts/deploy.sh`）

CIが `main` へのpushで成功したときに `workflow_run` で動く。手動実行は `main` のみ。`vars.AWS_DEPLOY_ROLE_ARN` が未設定の間はジョブ自体をスキップする。同時実行は1つで、実行中は取り消さない。GitHubは待機中の実行を1つしか残さず後着で取り消すため、各実行はトリガーのコミットではなく、mainでCIが成功した最新のコミットを調べてデプロイする（CI完了で起動した実行は、それが既にデプロイ済みなら終了）。処理本体は `scripts/deploy.sh` にあり、人の認証で手元から実行しても同じ手順になる。

主な判断：イメージタグはコミットSHAで、同じコミットの再実行では既存イメージを再利用する。stateの出力 `release` に前回のコミットを記録し、それより古いコミットは適用しない。`release` は疎通確認まで成功した後の最後のapply（出力だけが変わる）で更新する。planに削除・置換が含まれれば中止する（空の `image_uri` による削除を防ぐ）。`base_url` はLambdaに設定済みの値（出力 `configured_base_url`）か `vars.BASE_URL` を必ず渡す。`BASE_URL` のホストがCloudFrontのドメインか別名でなければ中止する（独自ドメイン用の別名・ACM証明書はまだTerraformにない）。静的ファイルはpushしたdigestのイメージから取り出し、ハッシュ付きアセットはapply前に追加、`index.html` はapply後に更新し、旧アセットは削除しない。疎通確認はPOSTせず、トップ・アセット・フォント・APIの400・共有ページの404・Function URL直接呼び出しの403を見る。`GENERATION_ENABLED` はリポジトリ変数で管理する。

Dockerfileのweb・Goステージを `--platform=$BUILDPLATFORM` にし、Goを `GOARCH=$TARGETARCH` でクロスコンパイルするよう変えた。amd64ランナーでQEMUなしにarm64イメージを作るため。

スクリプトの分岐（初回の2段階apply、同じコミットの再実行、生成停止、古いコミットのスキップ、削除を含むplanでの中止）は、terraform・aws・docker・curlのスタブで確認した。実際のAWSとDockerイメージでの実行はしていない。この作業環境ではpublic.ecr.awsへの接続が拒否されたため、イメージ全体のビルドもできなかった（Goステージがamd64で動くこと、arm64の静的バイナリができることは確認した）。

### Budgets通知

bootstrapの `aws_budgets_budget` で、アカウント全体の月額実コストが `budget_limit_usd`（既定1 USD）を超えたら `budget_emails` へメールする。通知先はユーザー未提供のため必須変数にし、`infra/bootstrap/terraform.tfvars`（`.gitignore` 済み）に書く想定。アプリ限定の予算はコスト配分タグの設計が必要なので見送った。

### EMF監視

`internal/metrics` を追加し、`METRICS_NAMESPACE` が設定されているとAPIが生成ごとにEMFのJSONを1行標準出力へ書く。Terraformで `KanjiGacha` を設定した。メトリクスは `GenerationTime`（生成器のみ、成功時）、`Glyphs`（`Rarity` ディメンション）、`Fallback`、`GenerationFailed` の計8個で、無料枠の10個以内。入力・ID・IPは書かない。保存失敗でも抽選は数え、検証エラー・停止中・レート制限は数えない。これらはテストで確認した。ダッシュボードとアラームは閾値が未決定のため作っていない。

## 未決定・未確認の事項

- Budgetsの通知先メールアドレス、アカウント全体かアプリ限定か。
- GitHub Environment `production` のDeployment branches設定と、Required reviewersを付けるか。
- メトリクスのアラーム閾値と通知先。
- AWS上での確認一式：bootstrapとアプリのapply、OIDCでのロール引受、Actions経由のデプロイ、Budgets設定、CloudWatchのメトリクス抽出、CloudFront経由のPOST→GET→共有HTML→OGP、POST本文ハッシュ有無・改変、Function URL直接呼び出しの拒否。
- その他の実測残件（閲覧者IP、退避率、コールドスタート、S3競合）は既存の [handover.md](handover.md) を参照する。

## 参照先

- [配置手順](../infra/README.md)、[bootstrap](../infra/bootstrap/main.tf)、[アプリのTerraform](../infra/main.tf)、[デプロイスクリプト](../scripts/deploy.sh)、[デプロイワークフロー](../.github/workflows/deploy.yml)、[仕様書](../spec.md)
- [AWS CLIのブラウザ認証](https://docs.aws.amazon.com/cli/latest/userguide/cli-configure-sign-in.html)
- [GitHub ActionsのAWS OIDC](https://docs.github.com/en/actions/how-tos/secure-your-work/security-harden-deployments/oidc-in-aws)
- [Terraform S3 backend](https://developer.hashicorp.com/terraform/language/backend/s3)
- [Terraform Budgets](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/budgets_budget)
- [CloudWatch EMF仕様](https://docs.aws.amazon.com/AmazonCloudWatch/latest/monitoring/CloudWatch_Embedded_Metric_Format_Specification.html)
