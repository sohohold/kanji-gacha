# AWSへの配置（未デプロイ）

TerraformはECR、非公開S3、Lambda arm64、Function URL（AWS_IAM）、OAC、CloudFront、ロググループを定義しています。顧客VPC・NAT Gatewayは作りません。初期状態の `image_uri=""` はイメージpush前に必要なECR・S3・IAM・ログ等のみ作ります。

以下は実際にAWSリソースと費用が発生する操作です。実行対象アカウントとplanを確認してから使ってください。この実装作業ではapplyしていません。

1. `terraform init` → `terraform plan` → `terraform apply` で保存先とECRを作成。
2. `terraform output -raw repository_url` でECR URIを確認し、AWS CLIの `ecr get-login-password` を `docker login --password-stdin` に渡して認証。
3. リポジトリルートで `docker buildx build --platform linux/arm64 --provenance=false -t <ECR URI>:<一意の版> --push .`。
4. pushしたイメージのdigest URIを `image_uri` に指定してplan/apply。CloudFrontとLambdaを作成。
5. `terraform output -raw base_url` の値を `base_url` 変数へ設定して再度plan/apply。初回のみ依存循環回避のため2段階としています。この完了までは共有URLがlocalhostを指すため公開しないでください。
6. `web/dist/assets/` をSPAバケットの `assets/` へ、`Cache-Control: public,max-age=31536000,immutable` で同期。`index.html` は `no-cache`、`kanji-ui.otf` は `public,max-age=86400` でアップロード。
7. CloudFrontの `/` `/index.html` `/g/*` を無効化。旧共有HTMLが参照するハッシュ付きアセットは、HTMLキャッシュの寿命を超えるまで削除しないでください。
8. CloudFront経由でPOST→GET→共有HTML→OGPを確認。POSTの本文ハッシュ有／無・改変時の動作、Function URLへの直接呼び出しが拒否されることを確認。

結果JSON・PNGには削除ライフサイクルを設定していません。S3をSPA同期先と結果保存先で分けているため、フロントの同期が生成結果を削除することはありません。

IAMは結果バケットの `glyphs/*` と `og/*` のGet/Put、および404判定に必要な限定的ListBucketを許可します。Lambdaへの呼び出し権限は対象CloudFront distribution ARNに限定します。

未実装：GitHub ActionsのAWS OIDCデプロイロール、applyを含むデプロイワークフロー、Budgets通知、EMF監視。既存アカウントのIAM/OIDC設定は未調査なので、自動で変更していません。これらとAWS上の実動確認を終えてから公開してください。

Terraform stateはローカル保存です。チーム運用へ進む場合は、アクセス制御されたリモートバックエンドとstateロックを設定してください。
