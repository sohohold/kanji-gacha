# The bucket is created by this configuration, so the first apply runs on local
# state and is then migrated here; see infra/README.md. Backend blocks cannot
# use variables, hence the literal account ID.
terraform {
  backend "s3" {
    bucket       = "kanji-gacha-473536070013-tfstate"
    key          = "bootstrap/terraform.tfstate"
    region       = "ap-northeast-1"
    encrypt      = true
    use_lockfile = true
  }
}
