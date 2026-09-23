# AWSへの配置（未デプロイ）

Terraformは2つの構成に分かれています。`infra/bootstrap` はアカウント単位で一度だけ人の認証で適用するもので、Terraform stateのS3バケット、GitHub ActionsのOIDCデプロイロール、Lambda実行ロールの権限境界、AWS Budgetsの通知を作ります。`infra` はアプリ本体で、ECR、非公開S3、Lambda arm64、Function URL（AWS_IAM）、OAC、CloudFront、ロググループを定義します。顧客VPC・NAT Gatewayは作りません。アプリ本体は通常 `.github/workflows/deploy.yml` が `scripts/deploy.sh` を通して適用します。

`infra/bootstrap` は2026-09-23にアカウント `473536070013` へapply済みで、stateはS3にあります。`infra`（アプリ本体）はまだapplyしていません。以下の操作は実際にAWSリソースと費用を発生させるので、対象アカウントとplanを確認してから実行してください。

## 実行前のAWS認証

`terraform plan` にもAWS認証が必要です。Terraformを実行する環境（WSL・コンテナの場合はその内部）で認証を設定してください。以下のコマンドはリポジトリルートから実行します。

[AWS CLI v2](https://docs.aws.amazon.com/cli/latest/userguide/getting-started-install.html) の2.32.0以降では、IAM Identity Center（SSO）を設定していなくてもブラウザ経由でログインできます。このプロジェクトではこの方法でplanまで確認済みです。

```bash
aws login --profile kanji-gacha --region ap-northeast-1 --remote
export AWS_PROFILE=kanji-gacha
aws sts get-caller-identity
```

`get-caller-identity` の `Account` が `473536070013` であることを確認してから進んでください。セッションが切れたら `aws login` を再実行します。

SSOを使う場合は `aws configure sso --profile kanji-gacha`（初回のみ）と `aws sso login --profile kanji-gacha` で、既存のアクセスキーを使う場合は `aws configure --profile kanji-gacha` で同じプロファイルを用意します。一時認証情報を環境変数で渡す場合は `AWS_SESSION_TOKEN` も必要です。認証情報を `.tf`・`.tfvars` やGitに保存しないでください。

`No valid credential sources found` と `no EC2 IMDS role found` が出る場合、利用可能な認証情報が見つからず、EC2のインスタンスメタデータへの問い合わせも失敗しています。上記の認証設定を確認してください。`AWS_EC2_METADATA_DISABLED=true` だけを設定しても認証情報は補えません。

`aws` コマンドと `terraform plan` は通るのに `terraform init` のS3バックエンドだけが同じエラーで失敗することがあります。Terraform 1.11.4同梱のS3バックエンドは、AWS Providerより古いSDKを使っていて `aws login` のプロファイルを解決できないためです。一時認証情報を環境変数へ展開してから実行してください。

```bash
eval "$(aws configure export-credentials --profile kanji-gacha --format env)"
```

参考：[AWS CLIのブラウザ認証](https://docs.aws.amazon.com/cli/latest/userguide/cli-configure-sign-in.html)、[AWS Providerの認証設定](https://registry.terraform.io/providers/hashicorp/aws/latest/docs#authentication-and-configuration)。

## 初回セットアップ

Terraformは1.10以上が必要です（S3バックエンドのネイティブロック `use_lockfile` を使うため）。Actionsでは1.11.4を使います。

### 1. 既存のGitHub OIDC Providerを確認する

```bash
aws iam list-open-id-connect-providers
```

`token.actions.githubusercontent.com` のARNが既にあれば、次の手順で `github_oidc_provider_arn` に指定して再利用します。1アカウントに同じURLのProviderは1つしか作れません。

### 2. bootstrapを適用し、stateをS3へ移す

stateの保存先バケット自体をこの構成で作るため、初回だけローカルstateで適用し、その後S3へ移します。`infra/bootstrap/terraform.tfvars` に通知先などを書きます（このファイルは `.gitignore` 済みです）。

```hcl
budget_emails = ["通知先のメールアドレス"]
# budget_limit_usd         = "1"
# github_oidc_provider_arn = "arn:aws:iam::473536070013:oidc-provider/token.actions.githubusercontent.com"
```

```bash
cd infra/bootstrap
printf 'terraform {\n  backend "local" {}\n}\n' > local_override.tf
terraform init
terraform plan    # state バケット、OIDC Provider、デプロイロール、権限境界、Budget を確認
terraform apply
rm local_override.tf
eval "$(aws configure export-credentials --profile kanji-gacha --format env)"   # バックエンド用
terraform init -migrate-state   # ローカルstateをS3へコピーするか聞かれたら yes
terraform plan                  # S3のstateで差分が出ないことを確認
rm terraform.tfstate terraform.tfstate.backup
cd ../..
```

`local_override.tf` は `backend.tf` のS3設定を一時的に置き換えるためのファイルで、Gitには入りません。bootstrapのstateは `bootstrap/terraform.tfstate` に置かれ、デプロイロールからは読み書きできません。以後bootstrapを変更するときは、人の認証で `infra/bootstrap` から `terraform plan` / `apply` します。stateバケットには `prevent_destroy` を付け、バージョニング（旧版は90日で削除）、SSE-S3暗号化、公開ブロック、HTTPSのみのポリシーを設定しています。

バックエンドの設定には変数を使えないため、`infra/backend.tf` と `infra/bootstrap/backend.tf` にはバケット名 `kanji-gacha-473536070013-tfstate` を直接書いています。別アカウントへ移す場合は両方を書き換えてください。

### 3. GitHubを設定する

リポジトリのSettingsで次を設定します。

- Environments に `production` を作り、Deployment branches を `main` のみに制限する。デプロイロールはこのEnvironmentのジョブだけを信頼するため（OIDCの `sub` が `repo:sohohold/kanji-gacha:environment:production`）、この制限がブランチの制限になります。必要なら Required reviewers も設定できます。
- Variables に `AWS_DEPLOY_ROLE_ARN` を追加し、`terraform -chdir=infra/bootstrap output -raw deploy_role_arn` の値を入れる。未設定の間、デプロイワークフローは実行されずにスキップされます。
- 任意で `GENERATION_ENABLED`（`true`/`false`、既定 `true`）と `BASE_URL` を追加する。`BASE_URL` は独自ドメインへ移すときだけ使い、未設定ならCloudFrontのドメインになります。下の「独自ドメイン」の準備が済むまでは設定しないでください。

長期のアクセスキーはGitHubに登録しません。

### 4. 最初のデプロイ

`deploy.yml` が `main` に入ると、以後 `main` へのpushでCIが成功するたびにデプロイされます。Actionsの画面から `Deploy` を手動実行（`main` のみ）しても構いませんが、デプロイされるのはCIが成功した最新のコミットなので、マージ後のCIが終わってから実行してください。初回は次のように進みます。

1. ECR・S3・IAM・ログ等だけを作る（`image_uri` 空）。
2. イメージをbuild/pushし、そのdigestでLambda・Function URL・CloudFrontを作る。この時点の共有URLはlocalhostを指します。
3. CloudFrontのドメインを `base_url` に入れて再度apply。ワークフローが疎通確認まで成功するまでは、URLを公開しないでください。

以前このリポジトリの `infra` で `terraform init` した手元の環境では、バックエンドがS3に変わったため `terraform -chdir=infra init -reconfigure` が必要です（applyしていなければ移すstateはありません）。

`vars.AWS_DEPLOY_ROLE_ARN` を設定する前に手元から配置したい場合は、認証したシェルで `scripts/deploy.sh` を実行すると同じ手順を踏みます（aws、terraform、docker buildx、jq、curl、gitが必要）。

## デプロイの流れ

`deploy.yml` は `CI` ワークフローが `main` へのpushで成功したとき（`workflow_run`）に動くので、テストが失敗したコミットはデプロイされません。`[skip ci]` を付けたコミットもCIが走らないためデプロイされません。`workflow_run` は既定ブランチにあるワークフロー定義で動く点に注意してください。同時実行は1つに限り、実行中のものは取り消しません。

GitHubは同じconcurrencyグループで待機中の実行を1つしか残さず、後から来た実行が待機中のものを取り消します。そのためCIの完了順が前後すると、新しいコミットの実行が古いコミットの実行に取り消されることがあります。これに備え、各実行は自分をトリガーしたコミットではなく、「`main` へのpushでCIが成功したコミットのうち最も新しいもの」を調べてデプロイします（`target` ステップ）。CIが失敗したコミットや走らなかったコミットは飛ばします。CIの完了で起動した実行は、そのコミットが既にデプロイ済みなら何もしません。手動実行は同じコミットでも再デプロイします。

`scripts/deploy.sh` の処理は次のとおりです。

1. `terraform init`。stateに記録された前回のコミット（出力 `release`）が今回のコミットを含んでいれば、古い版で上書きしないよう終了する。CIの完了順が前後しても新しい版が戻されません。同じコミットの再実行は続行します。`release` は下の9の疎通確認まで成功した後に最後のapplyで更新するので、途中で失敗した実行が古いコミットのデプロイを止めることはありません。
2. 初回のみ、`image_uri` を空にしてECRとバケットを作る。
3. コミットSHAをタグにarm64イメージを `--provenance=false` でbuild/pushする。タグは上書き不可なので、同じコミットの再実行では既存のイメージを再利用する。
4. pushしたdigestのイメージを取り出し、その中の `/app/web` を静的ファイルとして使う。共有HTMLはLambdaがイメージ内の `index.html` から返すため、手元の `web/dist` を使うとハッシュ付きファイル名が食い違います。
5. ハッシュ付きアセットを先にS3へ追加する（1年・immutable）。旧版のアセットは削除しません。キャッシュされた旧HTMLが参照し続けるためです。
6. digestを指定して `plan -out` し、削除・置換を含むplanなら中止してからそのplanをapplyする。`base_url` はLambdaに設定済みの値（出力 `configured_base_url`。初回はCloudFrontのドメイン）か `vars.BASE_URL` を必ず渡し、既定値のlocalhostへ戻しません。末尾の `/` は取り除きます。`https://` とホスト名だけのURLでない場合（パス・ポート・クエリ付きや `http://`）と、ホストがCloudFrontのドメインでも別名でもない場合は、何もapplyせずに中止します。
7. `kanji-ui.otf` と `index.html`（`no-cache`）をアップロードし、`index.html` が参照する実体がS3にあるか確認する。
8. CloudFrontの `/` `/index.html` `/g/*` `/kanji-ui.otf` を無効化し、完了を待つ。
9. CloudFront経由でトップ・アセット・フォント・API（不正IDで400）・共有ページ（存在しないIDで404のSPA）を確認し、Function URLへの直接呼び出しが403になることを確認する。生成（POST）はしないので、確認のたびに結果が保存されることはありません。

Dockerfileのweb・Goのステージはビルドマシンの環境で動き、Goは `GOARCH` を指定してクロスコンパイルします。amd64のランナーでもQEMUによるエミュレーションなしでarm64イメージを作れます。

### 削除を含むplanで止まった場合

リソース名の変更などで削除・置換が必要な変更は、ワークフローでは適用しません。手元の認証で内容を確認し、現在のイメージとURLを指定してapplyしてください。`image_uri`・`base_url`・`generation_enabled` を省略すると既定値（空・localhost・`true`）に戻り、Lambda等の削除、共有URLの破損、停止中の生成の再開につながります。

```bash
terraform -chdir=infra output   # release, configured_base_url, generation_enabled を確認
IMAGE=$(aws lambda get-function --function-name kanji-gacha --query 'Code.ImageUri' --output text)
terraform -chdir=infra plan -out=manual.tfplan -var "image_uri=$IMAGE" \
  -var "base_url=$(terraform -chdir=infra output -raw configured_base_url)" \
  -var "release=$(terraform -chdir=infra output -raw release)" \
  -var "generation_enabled=$(terraform -chdir=infra output -raw generation_enabled)"
terraform -chdir=infra apply manual.tfplan
```

## 運用

新規生成を止めるには、リポジトリ変数 `GENERATION_ENABLED` を `false` にして `Deploy` を手動実行します（同じコミットの再デプロイになります）。POSTは503を返し、保存済み結果の閲覧は続きます。変数を戻して再実行すると再開します。手元からapplyして切り替えた場合も、次のデプロイで変数の値に戻る点に注意してください。

AWS Budgetsは、アカウント全体の実コストが月 `budget_limit_usd`（既定1 USD）を超えたときに `budget_emails` へメールします。通知するだけで、何も止めません。アプリ分だけを対象にしたい場合は、下のタグを使ってフィルタを追加してください。

## 独自ドメイン

`BASE_URL` は、アプリが共有リンクとOGP画像のURLを組み立てるときに使う値です。変えるのはLambdaの環境変数だけで、CloudFrontをそのドメインで応答させる設定はしません。独自ドメインで公開するには、us-east-1で発行したACM証明書と、CloudFrontの別名（`aliases`）・`viewer_certificate` の設定、DNSのレコードが必要です。この構成にはまだその設定がありません。

`scripts/deploy.sh` は、`BASE_URL` のホストがdistributionのドメインか別名に含まれていない場合、何もapplyせずに中止します。存在しないドメインを誤って設定し、開けない共有リンクを発行することを防ぐためです。独自ドメインへ移すときは、先に上記の設定をTerraformへ追加して適用し、最後にリポジトリ変数 `BASE_URL` を設定してください。

## タグ

両構成の `provider "aws"` に `default_tags` を置き、タグを持てるリソースすべてに次を付けます。

| キー | 値 |
| --- | --- |
| `Project` | `kanji-gacha` |
| `Component` | `app`（`infra`）／ `bootstrap`（`infra/bootstrap`） |
| `ManagedBy` | `terraform` |

タグを条件にしたポリシーは書いていないので、タグは分類のためだけのものです。ただしデプロイロールには、タグを書き換えるための `iam:TagRole`・`*:UntagResource` 等を追加しています。`iam:TagRole` はCreateRole時に権限境界の条件が付かない経路でも必要になるため、境界条件のない別ステートメントにしてあります。

コスト配分に使うには、Billing and Cost Management → Cost allocation tags で `Project` を有効化します。`Component` も分けたいなら同様に有効化します。一覧にはタグの付いた課金対象リソースが検出されてから現れるので、applyの直後には出ません（最大24時間）。有効化した日以降のコストだけが対象で、遡るには同画面の「タグのバックフィル」を使います。

このアプリのリソースをまとめて見るには、Resource Groups & Tag Editor で `Project` = `kanji-gacha` のタグベースのグループ（Resource types は `AWS::AllSupported`）を作ります。追加料金はありません。タグが付いていないものも含めてアカウント全体を見るには AWS Resource Explorer（追加料金なし、要有効化）を使います。AWS Configは構成項目ごとの課金があるため使っていません。

## 監視（CloudWatch埋め込みメトリクス）

Lambdaの環境変数 `METRICS_NAMESPACE=KanjiGacha` が設定されていると、APIは生成のたびにEMF形式のJSONを1行、標準出力へ書きます。CloudWatch Logsがロググループ `/aws/lambda/kanji-gacha` からメトリクスを取り出すので、PutMetricDataの権限や追加の設定は要りません。通常のログは標準エラーへ日時付きで出るため、EMFの行とは混ざりません。

| メトリクス | 単位 | ディメンション | 内容 |
| --- | --- | --- | --- |
| `GenerationTime` | Milliseconds | なし | 生成器（`Engine.Generate`）の所要時間。入力検証・保存は含まない。成功時のみ |
| `Glyphs` | Count | `Rarity`（N/R/SR/SSR/UR） | 抽選されたレアリティの件数。保存に失敗した抽選も含む |
| `Fallback` | Count | なし | 退避候補を返した件数（成功件数に対する割合が退避率） |
| `GenerationFailed` | Count | なし | 生成器がエラーを返した件数 |

入力・読み・SVG・生成ID・IPはメトリクスにもログにも書きません。形式エラー、生成停止中、レート制限で生成器まで届かなかった要求は数えません。カスタムメトリクスは名前とディメンション値の組ごとに課金されるため、ディメンションはレアリティだけにしてあり、全部で8個です（無料枠の10個以内）。ディメンションやメトリクスを増やすときは費用を確認してください。

値はCloudWatchコンソールの Metrics → `KanjiGacha` で見られます。ダッシュボードとアラームは閾値が未決定のため作っていません。

## 権限の設計

デプロイロール（`kanji-gacha-deploy`）の権限は `infra/bootstrap/main.tf` にあります。stateは `app/` 配下だけ、ECR・S3・Lambda・ロググループはこのアプリの名前のARNに限っています。CloudFrontはIDが作成時に決まるためARNで絞れず、作成・更新・無効化の操作だけを許可しています。バケット・リポジトリ・関数・distributionの削除権限は与えていません。

Lambda実行ロールは権限境界 `kanji-gacha-lambda-boundary` を付けたときだけ作成・変更できます。境界は結果バケットの `glyphs/*`・`og/*` の読み書き、そのListBucket、ロググループへの書き込みに限られるため、Terraform経由でインラインポリシーを書き換えてもLambdaの権限はそれ以上に広がりません。`iam:PassRole` はこのロールをLambdaへ渡す場合だけです。境界ポリシーはアプリ構成より先に存在する必要があるので、bootstrapを先に適用してください。

アプリ側のIAMは結果バケットの `glyphs/*` と `og/*` のGet/Put、および404判定に必要な限定的ListBucketを許可します。Lambdaへの呼び出し権限は対象CloudFront distribution ARNに限定します。

結果JSON・PNGには削除ライフサイクルを設定していません。S3をSPA同期先と結果保存先で分けているため、フロントの同期が生成結果を削除することはありません。

## 未確認の事項

bootstrapのapplyは済んでいます（12リソース作成、S3 stateで差分なしを確認）。アプリ構成のapply、OIDCでのロール引受、Actions経由のデプロイ、Budgetsの通知、CloudWatchでのメトリクス抽出は、まだAWS上で確認していません。デプロイロールの権限はTerraformとAWS CLIが呼ぶAPIから設計したもので、初回のデプロイで不足が見つかった場合は `AccessDenied` のアクションを追加してください。公開前に、CloudFront経由のPOST→GET→共有HTML→OGP、POSTの本文ハッシュ有／無・改変時の動作も確認してください。
