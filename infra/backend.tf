# Created by infra/bootstrap. The deploy role can read and write only the app/
# prefix. The bucket, kanji-gacha-<account ID>-tfstate, is passed to init with
# -backend-config so the account ID stays out of the repository.
terraform {
  backend "s3" {
    key          = "app/terraform.tfstate"
    region       = "ap-northeast-1"
    encrypt      = true
    use_lockfile = true
  }
}
