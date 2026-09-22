terraform {
  required_version = ">= 1.6.0"
  required_providers {
    aws = { source = "hashicorp/aws", version = "~> 6.0" }
  }
}
provider "aws" { region = var.region }
variable "region" { default = "ap-northeast-1" }
variable "name" { default = "kanji-gacha" }
variable "image_uri" {
  type        = string
  default     = ""
  description = "ECR image digest URI; empty creates bootstrap storage and ECR only."
}
variable "generation_enabled" { default = true }
data "aws_caller_identity" "current" {}
locals {
  deploy = var.image_uri != ""
  suffix = "${var.name}-${data.aws_caller_identity.current.account_id}"
}
resource "aws_ecr_repository" "app" {
  name                 = var.name
  image_tag_mutability = "IMMUTABLE"
}
resource "aws_ecr_repository_policy" "lambda" {
  repository = aws_ecr_repository.app.name
  policy = jsonencode({ Version = "2012-10-17", Statement = [{
    Sid       = "LambdaImageRetrieval", Effect = "Allow",
    Principal = { Service = "lambda.amazonaws.com" },
    Action    = ["ecr:BatchGetImage", "ecr:GetDownloadUrlForLayer"],
    Condition = { ArnLike = { "aws:SourceArn" = "arn:aws:lambda:${var.region}:${data.aws_caller_identity.current.account_id}:function:${var.name}" } }
  }] })
}
resource "aws_ecr_lifecycle_policy" "app" {
  repository = aws_ecr_repository.app.name
  policy     = jsonencode({ rules = [{ rulePriority = 1, description = "Keep last three images", selection = { tagStatus = "any", countType = "imageCountMoreThan", countNumber = 3 }, action = { type = "expire" } }] })
}
resource "aws_s3_bucket" "spa" { bucket = "${local.suffix}-spa" }
resource "aws_s3_bucket" "results" { bucket = "${local.suffix}-results" }
resource "aws_s3_bucket_public_access_block" "private" {
  for_each                = { spa = aws_s3_bucket.spa.id, results = aws_s3_bucket.results.id }
  bucket                  = each.value
  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}
resource "aws_iam_role" "lambda" {
  name               = "${var.name}-lambda"
  assume_role_policy = jsonencode({ Version = "2012-10-17", Statement = [{ Effect = "Allow", Principal = { Service = "lambda.amazonaws.com" }, Action = "sts:AssumeRole" }] })
}
resource "aws_cloudwatch_log_group" "app" {
  name              = "/aws/lambda/${var.name}"
  retention_in_days = 7
}
resource "aws_iam_role_policy" "lambda" {
  role = aws_iam_role.lambda.id
  policy = jsonencode({ Version = "2012-10-17", Statement = [
    { Effect = "Allow", Action = ["s3:GetObject", "s3:PutObject"], Resource = ["${aws_s3_bucket.results.arn}/glyphs/*", "${aws_s3_bucket.results.arn}/og/*"] },
    { Effect = "Allow", Action = ["s3:ListBucket"], Resource = aws_s3_bucket.results.arn },
    { Effect = "Allow", Action = ["logs:CreateLogStream", "logs:PutLogEvents"], Resource = "${aws_cloudwatch_log_group.app.arn}:*" }
  ] })
}
# Create the Function URL first, then CloudFront. BASE_URL is supplied at the next
# apply using the CloudFront domain to avoid a Terraform dependency cycle.
variable "base_url" {
  type    = string
  default = "http://localhost:8080"
  validation {
    condition     = can(regex("^https?://", var.base_url))
    error_message = "base_url must be an absolute HTTP(S) URL."
  }
}
resource "aws_lambda_function" "app" {
  count                          = local.deploy ? 1 : 0
  function_name                  = var.name
  role                           = aws_iam_role.lambda.arn
  package_type                   = "Image"
  image_uri                      = var.image_uri
  architectures                  = ["arm64"]
  memory_size                    = 1024
  timeout                        = 10
  reserved_concurrent_executions = 10
  environment {
    variables = {
      S3_BUCKET          = aws_s3_bucket.results.id
      BASE_URL           = var.base_url
      GENERATION_ENABLED = tostring(var.generation_enabled)
      TRUST_CLOUDFRONT   = "true"
    }
  }
  depends_on = [aws_iam_role_policy.lambda]
}
resource "aws_lambda_function_url" "app" {
  count              = local.deploy ? 1 : 0
  function_name      = aws_lambda_function.app[0].function_name
  authorization_type = "AWS_IAM"
}
resource "aws_cloudfront_origin_access_control" "spa" {
  name                              = "${var.name}-spa"
  origin_access_control_origin_type = "s3"
  signing_behavior                  = "always"
  signing_protocol                  = "sigv4"
}
resource "aws_cloudfront_origin_access_control" "api" {
  name                              = "${var.name}-api"
  origin_access_control_origin_type = "lambda"
  signing_behavior                  = "always"
  signing_protocol                  = "sigv4"
}
resource "aws_cloudfront_cache_policy" "app" {
  name        = var.name
  min_ttl     = 0
  default_ttl = 0
  max_ttl     = 31536000
  parameters_in_cache_key_and_forwarded_to_origin {
    enable_accept_encoding_gzip   = true
    enable_accept_encoding_brotli = true
    cookies_config { cookie_behavior = "none" }
    headers_config { header_behavior = "none" }
    query_strings_config { query_string_behavior = "none" }
  }
}
resource "aws_cloudfront_origin_request_policy" "api" {
  name = "${var.name}-api"
  cookies_config { cookie_behavior = "none" }
  query_strings_config { query_string_behavior = "none" }
  headers_config {
    header_behavior = "whitelist"
    # CloudFront-Viewer-Address is the rate-limit key; the cache policy must not
    # include it or the cache would split per viewer.
    headers { items = ["Content-Type", "x-amz-content-sha256", "CloudFront-Viewer-Address"] }
  }
}
resource "aws_cloudfront_distribution" "app" {
  count               = local.deploy ? 1 : 0
  enabled             = true
  default_root_object = "index.html"
  origin {
    domain_name              = aws_s3_bucket.spa.bucket_regional_domain_name
    origin_id                = "spa"
    origin_access_control_id = aws_cloudfront_origin_access_control.spa.id
  }
  origin {
    domain_name              = trimsuffix(trimprefix(aws_lambda_function_url.app[0].function_url, "https://"), "/")
    origin_id                = "api"
    origin_access_control_id = aws_cloudfront_origin_access_control.api.id
    custom_origin_config {
      http_port              = 80
      https_port             = 443
      origin_protocol_policy = "https-only"
      origin_ssl_protocols   = ["TLSv1.2"]
    }
  }
  default_cache_behavior {
    target_origin_id       = "spa"
    viewer_protocol_policy = "redirect-to-https"
    allowed_methods        = ["GET", "HEAD"]
    cached_methods         = ["GET", "HEAD"]
    cache_policy_id        = aws_cloudfront_cache_policy.app.id
    compress               = true
  }
  dynamic "ordered_cache_behavior" {
    for_each = ["/api/*", "/g/*", "/og/*"]
    content {
      path_pattern             = ordered_cache_behavior.value
      target_origin_id         = "api"
      viewer_protocol_policy   = "redirect-to-https"
      allowed_methods          = ["GET", "HEAD", "OPTIONS", "PUT", "PATCH", "POST", "DELETE"]
      cached_methods           = ["GET", "HEAD"]
      cache_policy_id          = aws_cloudfront_cache_policy.app.id
      origin_request_policy_id = aws_cloudfront_origin_request_policy.api.id
      compress                 = true
    }
  }
  dynamic "custom_error_response" {
    for_each = [400, 403, 404, 405, 414, 416, 500, 501, 502, 503, 504]
    content {
      error_code            = custom_error_response.value
      error_caching_min_ttl = 0
    }
  }
  restrictions {
    geo_restriction { restriction_type = "none" }
  }
  viewer_certificate { cloudfront_default_certificate = true }
}
resource "aws_s3_bucket_policy" "spa" {
  count  = local.deploy ? 1 : 0
  bucket = aws_s3_bucket.spa.id
  policy = jsonencode({ Version = "2012-10-17", Statement = [{ Effect = "Allow", Principal = { Service = "cloudfront.amazonaws.com" }, Action = "s3:GetObject", Resource = "${aws_s3_bucket.spa.arn}/*", Condition = { StringEquals = { "AWS:SourceArn" = aws_cloudfront_distribution.app[0].arn } } }] })
}
resource "aws_lambda_permission" "url" {
  count                  = local.deploy ? 1 : 0
  statement_id           = "CloudFrontFunctionURL"
  action                 = "lambda:InvokeFunctionUrl"
  function_name          = aws_lambda_function.app[0].function_name
  principal              = "cloudfront.amazonaws.com"
  source_arn             = aws_cloudfront_distribution.app[0].arn
  function_url_auth_type = "AWS_IAM"
}
resource "aws_lambda_permission" "invoke" {
  count                    = local.deploy ? 1 : 0
  statement_id             = "CloudFrontInvokeFunction"
  action                   = "lambda:InvokeFunction"
  function_name            = aws_lambda_function.app[0].function_name
  principal                = "cloudfront.amazonaws.com"
  source_arn               = aws_cloudfront_distribution.app[0].arn
  invoked_via_function_url = true
}
output "repository_url" { value = aws_ecr_repository.app.repository_url }
output "spa_bucket" { value = aws_s3_bucket.spa.id }
output "results_bucket" { value = aws_s3_bucket.results.id }
output "base_url" { value = local.deploy ? "https://${aws_cloudfront_distribution.app[0].domain_name}" : null }
output "distribution_id" { value = local.deploy ? aws_cloudfront_distribution.app[0].id : null }
