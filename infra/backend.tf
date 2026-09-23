# Created by infra/bootstrap. The deploy role can read and write only the app/
# prefix. Backend blocks cannot use variables, hence the literal account ID.
terraform {
  backend "s3" {
    bucket       = "kanji-gacha-473536070013-tfstate"
    key          = "app/terraform.tfstate"
    region       = "ap-northeast-1"
    encrypt      = true
    use_lockfile = true
  }
}
