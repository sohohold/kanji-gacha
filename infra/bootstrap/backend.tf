# The bucket is created by this configuration, so the first apply runs on local
# state and is then migrated here; see infra/README.md. The bucket,
# kanji-gacha-<account ID>-tfstate, is passed to init with -backend-config so
# the account ID stays out of the repository.
terraform {
  backend "s3" {
    key          = "bootstrap/terraform.tfstate"
    region       = "ap-northeast-1"
    encrypt      = true
    use_lockfile = true
  }
}
