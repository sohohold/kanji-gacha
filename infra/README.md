# AWSへの配置（未デプロイ）

TerraformはECR、非公開S3、Lambda arm64、Function URL（AWS_IAM）、OAC、CloudFront、ロググループを定義しています。顧客VPC・NAT Gatewayは作りません。初期状態の `image_uri=""` はイメージpush前に必要なECR・S3・IAM・ログ等のみ作ります。

## 実行前のAWS認証

`terraform plan` にもAWS認証が必要です。Terraformを実行する環境（WSL・コンテナの場合はその内部）で認証を設定してください。以下のコマンドはリポジトリルートから実行します。

まず [AWS CLI v2](https://docs.aws.amazon.com/cli/latest/userguide/getting-started-install.html) をインストールし、`aws --version` で確認します。

IAM Identity Center（SSO）を利用できる場合は、次の手順でプロファイルを設定します。SSO Start URLとSSOリージョンは利用先の設定値を入力してください。SSOリージョンはリソースの配置先 `ap-northeast-1` と異なる場合があります。

```bash
aws configure sso --profile kanji-gacha # 初回のみ
aws sso login --profile kanji-gacha
export AWS_PROFILE=kanji-gacha
aws sts get-caller-identity
terraform -chdir=infra init
terraform -chdir=infra plan
```

`get-caller-identity` の `Account` と `Arn` が意図したアカウント・ロールか確認してからplanへ進んでください。既存のプロファイルを使う場合は `kanji-gacha` をその名前に置き換え、初回設定を省略します。SSOのセッションが切れた場合は `aws sso login --profile kanji-gacha` を再実行します。

SSOを使わず、既にIAMユーザーのアクセスキーが発行されている場合は、上記の `configure sso` と `sso login` の代わりに `aws configure --profile kanji-gacha` で対話入力します。一時認証情報を環境変数で渡す場合は `AWS_ACCESS_KEY_ID`・`AWS_SECRET_ACCESS_KEY` に加え `AWS_SESSION_TOKEN` も必要です。認証情報を `.tf`・`.tfvars` やGitに保存しないでください。

`No valid credential sources found` と `no EC2 IMDS role found` が出る場合、利用可能な認証情報が見つからず、EC2のインスタンスメタデータへの問い合わせも失敗しています。ローカル実行では上記の認証設定を確認してください。`AWS_EC2_METADATA_DISABLED=true` だけを設定しても認証情報は補えません。

参考：[AWS CLIのSSO設定](https://docs.aws.amazon.com/cli/latest/userguide/cli-configure-sso.html)、[AWS Providerの認証設定](https://registry.terraform.io/providers/hashicorp/aws/latest/docs#authentication-and-configuration)。

## 配置手順

以下は実際にAWSリソースと費用が発生する操作です。実行対象アカウントとplanを確認してから使ってください。この実装作業ではapplyしていません。

1. 認証したシェルで `cd infra` し、`terraform init` → `terraform plan` → `terraform apply` で保存先とECRを作成。手順6の同期コマンドはリポジトリルートから実行。
2. `terraform output -raw repository_url` でECR URIを確認し、AWS CLIの `ecr get-login-password` を `docker login --password-stdin` に渡して認証。
3. リポジトリルートで `docker buildx build --platform linux/arm64 --provenance=false -t <ECR URI>:<一意の版> --push .`。
4. pushしたイメージのdigest URIを `image_uri` に指定してplan/apply。CloudFrontとLambdaを作成。
5. `terraform output -raw base_url` の値を `base_url` 変数へ設定して再度plan/apply。初回のみ依存循環回避のため2段階としています。この完了までは共有URLがlocalhostを指すため公開しないでください。
6. 共有HTMLはLambdaがイメージ内の `/app/web/index.html` を返すため、S3へ同期するアセットは必ず同じイメージから取り出す。手元の `web/dist` を同期するとハッシュ付きファイル名が食い違い、共有ページのJS/CSSが403になる。

   ```bash
   SPA_BUCKET=$(terraform -chdir=infra output -raw spa_bucket)
   cid=$(docker create <ECR URI>:<版>)
   # 取り出し先は毎回作り直す。既存のディレクトリへ docker cp すると中身を
   # 置き換えず release-web/web として入れ子になり、前回の版を同期してしまう。
   rm -rf ./release-web && mkdir -p ./release-web
   docker cp "$cid:/app/web/." ./release-web && docker rm "$cid"
   aws s3 sync ./release-web/assets "s3://$SPA_BUCKET/assets/" \
     --cache-control 'public,max-age=31536000,immutable'
   aws s3 cp ./release-web/index.html "s3://$SPA_BUCKET/index.html" --cache-control 'no-cache'
   aws s3 cp ./release-web/kanji-ui.otf "s3://$SPA_BUCKET/kanji-ui.otf" --cache-control 'public,max-age=86400'
   # index.html が参照する実体がS3にあるか確認する
   grep -o '/assets/[A-Za-z0-9._-]*' ./release-web/index.html | sort -u | while read -r k; do
     aws s3api head-object --bucket "$SPA_BUCKET" --key "${k#/}" >/dev/null || { echo "missing $k"; exit 1; }
   done
   ```
7. CloudFrontの `/` `/index.html` `/g/*` を無効化。旧共有HTMLが参照するハッシュ付きアセットは、HTMLキャッシュの寿命を超えるまで削除しないでください。
8. CloudFront経由でPOST→GET→共有HTML→OGPを確認。POSTの本文ハッシュ有／無・改変時の動作、Function URLへの直接呼び出しが拒否されることを確認。

結果JSON・PNGには削除ライフサイクルを設定していません。S3をSPA同期先と結果保存先で分けているため、フロントの同期が生成結果を削除することはありません。

IAMは結果バケットの `glyphs/*` と `og/*` のGet/Put、および404判定に必要な限定的ListBucketを許可します。Lambdaへの呼び出し権限は対象CloudFront distribution ARNに限定します。

未実装：GitHub ActionsのAWS OIDCデプロイロール、applyを含むデプロイワークフロー、Budgets通知、EMF監視。既存アカウントのIAM/OIDC設定は未調査なので、自動で変更していません。これらとAWS上の実動確認を終えてから公開してください。

Terraform stateはローカル保存です。チーム運用へ進む場合は、アクセス制御されたリモートバックエンドとstateロックを設定してください。
