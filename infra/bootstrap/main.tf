# Account-level resources the deploy workflow depends on but must not manage
# itself: the Terraform state bucket, the GitHub OIDC deploy role and the
# permissions boundary that caps what that role can grant, and the budget alert.
# Apply this with a person's credentials; see infra/README.md.
terraform {
  required_version = ">= 1.10.0"
  required_providers {
    aws = { source = "hashicorp/aws", version = "~> 6.0" }
  }
}
provider "aws" {
  region = var.region
  default_tags {
    tags = {
      Project   = var.name
      Component = "bootstrap"
      ManagedBy = "terraform"
    }
  }
}
variable "region" { default = "ap-northeast-1" }
variable "name" { default = "kanji-gacha" }
variable "github_repository" { default = "sohohold/kanji-gacha" }
variable "github_environment" {
  default     = "production"
  description = "GitHub Environment the deploy job runs in; the role trusts only this environment."
}
variable "github_oidc_provider_arn" {
  type        = string
  default     = ""
  description = "ARN of an existing token.actions.githubusercontent.com provider to reuse; empty creates one."
}
variable "budget_limit_usd" {
  type    = string
  default = "1"
}
variable "budget_emails" {
  type        = list(string)
  description = "Addresses notified when actual monthly cost exceeds the budget."
  validation {
    condition     = length(var.budget_emails) > 0
    error_message = "Set at least one address in budget_emails."
  }
}
data "aws_caller_identity" "current" {}
locals {
  account = data.aws_caller_identity.current.account_id
  suffix  = "${var.name}-${local.account}"
  # Names the app configuration (infra/main.tf) creates. They are fixed so the
  # deploy role can be scoped before those resources exist.
  app = {
    ecr         = "arn:aws:ecr:${var.region}:${local.account}:repository/${var.name}"
    spa         = "arn:aws:s3:::${local.suffix}-spa"
    results     = "arn:aws:s3:::${local.suffix}-results"
    lambda_role = "arn:aws:iam::${local.account}:role/${var.name}-lambda"
    function    = "arn:aws:lambda:${var.region}:${local.account}:function:${var.name}"
    log_group   = "arn:aws:logs:${var.region}:${local.account}:log-group:/aws/lambda/${var.name}"
  }
  oidc_provider_arn = var.github_oidc_provider_arn != "" ? var.github_oidc_provider_arn : aws_iam_openid_connect_provider.github[0].arn
}

# --- Terraform state -------------------------------------------------------

resource "aws_s3_bucket" "state" {
  bucket = "${local.suffix}-tfstate"
  lifecycle { prevent_destroy = true }
}
resource "aws_s3_bucket_public_access_block" "state" {
  bucket                  = aws_s3_bucket.state.id
  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}
resource "aws_s3_bucket_versioning" "state" {
  bucket = aws_s3_bucket.state.id
  versioning_configuration { status = "Enabled" }
}
resource "aws_s3_bucket_server_side_encryption_configuration" "state" {
  bucket = aws_s3_bucket.state.id
  rule {
    apply_server_side_encryption_by_default { sse_algorithm = "AES256" }
  }
}
resource "aws_s3_bucket_ownership_controls" "state" {
  bucket = aws_s3_bucket.state.id
  rule { object_ownership = "BucketOwnerEnforced" }
}
resource "aws_s3_bucket_lifecycle_configuration" "state" {
  bucket = aws_s3_bucket.state.id
  rule {
    id     = "expire-old-state-versions"
    status = "Enabled"
    filter {}
    noncurrent_version_expiration { noncurrent_days = 90 }
  }
}
resource "aws_s3_bucket_policy" "state" {
  bucket = aws_s3_bucket.state.id
  policy = jsonencode({ Version = "2012-10-17", Statement = [{
    Sid       = "DenyInsecureTransport", Effect = "Deny", Principal = "*", Action = "s3:*",
    Resource  = [aws_s3_bucket.state.arn, "${aws_s3_bucket.state.arn}/*"],
    Condition = { Bool = { "aws:SecureTransport" = "false" } }
  }] })
  depends_on = [aws_s3_bucket_public_access_block.state]
}

# --- GitHub Actions OIDC ---------------------------------------------------

resource "aws_iam_openid_connect_provider" "github" {
  count          = var.github_oidc_provider_arn == "" ? 1 : 0
  url            = "https://token.actions.githubusercontent.com"
  client_id_list = ["sts.amazonaws.com"]
}
resource "aws_iam_role" "deploy" {
  name                 = "${var.name}-deploy"
  max_session_duration = 3600
  assume_role_policy = jsonencode({ Version = "2012-10-17", Statement = [{
    Effect    = "Allow",
    Principal = { Federated = local.oidc_provider_arn },
    Action    = "sts:AssumeRoleWithWebIdentity",
    Condition = { StringEquals = {
      "token.actions.githubusercontent.com:aud" = "sts.amazonaws.com",
      # With a job-level environment the subject names the environment instead
      # of the branch; the environment's branch rule restricts it to main.
      "token.actions.githubusercontent.com:sub" = "repo:${var.github_repository}:environment:${var.github_environment}"
    } }
  }] })
}

# The Lambda role's ceiling. The deploy role may only create or change that
# role while this boundary stays attached, so editing its inline policy through
# Terraform cannot hand the function (and so the deploy role) wider access.
resource "aws_iam_policy" "lambda_boundary" {
  name = "${var.name}-lambda-boundary"
  policy = jsonencode({ Version = "2012-10-17", Statement = [
    { Effect = "Allow", Action = ["s3:GetObject", "s3:PutObject"], Resource = ["${local.app.results}/glyphs/*", "${local.app.results}/og/*"] },
    { Effect = "Allow", Action = ["s3:ListBucket"], Resource = local.app.results },
    { Effect = "Allow", Action = ["logs:CreateLogStream", "logs:PutLogEvents"], Resource = "${local.app.log_group}:*" }
  ] })
}

resource "aws_iam_role_policy" "deploy" {
  role = aws_iam_role.deploy.id
  policy = jsonencode({ Version = "2012-10-17", Statement = [
    # State for the app configuration only; the bootstrap state is off limits.
    { Sid = "StateList", Effect = "Allow", Action = "s3:ListBucket", Resource = aws_s3_bucket.state.arn },
    { Sid = "StateObjects", Effect = "Allow", Action = ["s3:GetObject", "s3:PutObject", "s3:DeleteObject"], Resource = "${aws_s3_bucket.state.arn}/app/*" },

    { Sid = "EcrLogin", Effect = "Allow", Action = "ecr:GetAuthorizationToken", Resource = "*" },
    { Sid = "EcrRepository", Effect = "Allow", Resource = local.app.ecr, Action = [
      "ecr:CreateRepository", "ecr:DescribeRepositories", "ecr:ListTagsForResource", "ecr:TagResource",
      "ecr:GetRepositoryPolicy", "ecr:SetRepositoryPolicy", "ecr:GetLifecyclePolicy", "ecr:PutLifecyclePolicy",
      "ecr:UntagResource", "ecr:PutImageTagMutability", "ecr:PutImageScanningConfiguration",
      "ecr:DescribeImages", "ecr:BatchGetImage", "ecr:GetDownloadUrlForLayer", "ecr:BatchCheckLayerAvailability",
      "ecr:InitiateLayerUpload", "ecr:UploadLayerPart", "ecr:CompleteLayerUpload", "ecr:PutImage"
    ] },

    # Terraform reads every bucket sub-resource on refresh, hence Get*/List*.
    # Deleting buckets is not granted.
    { Sid = "AppBucketsRead", Effect = "Allow", Action = ["s3:Get*", "s3:List*"], Resource = [local.app.spa, "${local.app.spa}/*", local.app.results, "${local.app.results}/*"] },
    { Sid = "AppBucketsManage", Effect = "Allow", Resource = [local.app.spa, local.app.results], Action = [
      "s3:CreateBucket", "s3:PutBucketPublicAccessBlock", "s3:PutBucketPolicy", "s3:DeleteBucketPolicy", "s3:PutBucketTagging"
    ] },
    { Sid = "SpaObjects", Effect = "Allow", Action = ["s3:PutObject"], Resource = "${local.app.spa}/*" },

    { Sid = "LambdaRoleRead", Effect = "Allow", Resource = local.app.lambda_role, Action = [
      "iam:GetRole", "iam:GetRolePolicy", "iam:ListRolePolicies", "iam:ListAttachedRolePolicies", "iam:ListInstanceProfilesForRole"
    ] },
    { Sid    = "LambdaRoleWriteWithinBoundary", Effect = "Allow", Resource = local.app.lambda_role,
      Action = ["iam:CreateRole", "iam:PutRolePolicy", "iam:DeleteRolePolicy", "iam:PutRolePermissionsBoundary"],
    Condition = { StringEquals = { "iam:PermissionsBoundary" = aws_iam_policy.lambda_boundary.arn } } },
    # default_tags reaches the Lambda role too. CreateRole carries the boundary
    # condition, but tagging an existing role does not, so it needs its own
    # statement. Tags grant nothing here; no policy keys off them.
    { Sid = "LambdaRoleTags", Effect = "Allow", Resource = local.app.lambda_role, Action = [
      "iam:TagRole", "iam:UntagRole", "iam:ListRoleTags"
    ] },
    { Sid = "LambdaRolePass", Effect = "Allow", Action = "iam:PassRole", Resource = local.app.lambda_role,
    Condition = { StringEquals = { "iam:PassedToService" = "lambda.amazonaws.com" } } },

    { Sid = "LambdaFunction", Effect = "Allow", Resource = [local.app.function, "${local.app.function}:*"], Action = [
      "lambda:Get*", "lambda:List*", "lambda:CreateFunction", "lambda:UpdateFunctionCode", "lambda:UpdateFunctionConfiguration",
      "lambda:PutFunctionConcurrency", "lambda:DeleteFunctionConcurrency", "lambda:TagResource", "lambda:UntagResource",
      "lambda:AddPermission", "lambda:RemovePermission",
      "lambda:CreateFunctionUrlConfig", "lambda:UpdateFunctionUrlConfig"
    ] },

    { Sid = "LogGroupList", Effect = "Allow", Action = "logs:DescribeLogGroups", Resource = "*" },
    { Sid = "LogGroup", Effect = "Allow", Resource = [local.app.log_group, "${local.app.log_group}:*"], Action = [
      "logs:CreateLogGroup", "logs:PutRetentionPolicy", "logs:ListTagsForResource", "logs:ListTagsLogGroup", "logs:TagResource", "logs:UntagResource"
    ] },

    # CloudFront IDs are assigned on creation, so these cannot be scoped by ARN.
    # Deletes are not granted. With default_tags the provider creates the
    # distribution through CreateDistributionWithTags, a separate action.
    { Sid = "CloudFront", Effect = "Allow", Resource = "*", Action = [
      "cloudfront:Get*", "cloudfront:List*",
      "cloudfront:CreateDistribution", "cloudfront:CreateDistributionWithTags", "cloudfront:UpdateDistribution", "cloudfront:TagResource", "cloudfront:UntagResource",
      "cloudfront:CreateCachePolicy", "cloudfront:UpdateCachePolicy",
      "cloudfront:CreateOriginRequestPolicy", "cloudfront:UpdateOriginRequestPolicy",
      "cloudfront:CreateOriginAccessControl", "cloudfront:UpdateOriginAccessControl",
      "cloudfront:CreateInvalidation"
    ] }
  ] })
}

# --- Cost alert ------------------------------------------------------------

# Covers the whole account: this notifies, it does not stop anything. Pausing
# new generation is the separate generation_enabled switch.
resource "aws_budgets_budget" "monthly" {
  name         = "${var.name}-monthly"
  budget_type  = "COST"
  limit_amount = var.budget_limit_usd
  limit_unit   = "USD"
  time_unit    = "MONTHLY"
  notification {
    comparison_operator        = "GREATER_THAN"
    threshold                  = 100
    threshold_type             = "PERCENTAGE"
    notification_type          = "ACTUAL"
    subscriber_email_addresses = var.budget_emails
  }
}

output "state_bucket" { value = aws_s3_bucket.state.id }
output "deploy_role_arn" { value = aws_iam_role.deploy.arn }
output "lambda_boundary_arn" { value = aws_iam_policy.lambda_boundary.arn }
