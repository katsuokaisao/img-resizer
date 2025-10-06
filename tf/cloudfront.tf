# ----- OLAP ARN ベースのドメイン名 -----
# AWS公式: S3 Object Lambda Access Point を CloudFront のオリジンとして使用する場合
# ドメイン名は {alias}.s3.{region}.amazonaws.com 形式を使用
locals {
  # ARN: arn:aws:s3-object-lambda:region:account-id:accesspoint/name
  # Domain: {alias}.s3.{region}.amazonaws.com (NOT s3-object-lambda!)
  olap_origin_domain = "${aws_s3control_object_lambda_access_point.olap.alias}.s3.${var.region}.amazonaws.com"
}

# ----- Origin Access Control (OAC) -----
# S3 Object Lambda へのアクセスに SigV4 署名を使用
resource "aws_cloudfront_origin_access_control" "olap_oac" {
  name                              = "olap-oac"
  description                       = "OAC for S3 Object Lambda Access Point"
  origin_access_control_origin_type = "s3"
  signing_behavior                  = "always"
  signing_protocol                  = "sigv4"
}

# ----- CloudFront Distribution -----
resource "aws_cloudfront_distribution" "this" {
  enabled             = true
  is_ipv6_enabled     = true
  comment             = "Image resize via S3 Object Lambda"
  default_root_object = ""

  origin {
    origin_id                = local.olap_origin_domain
    domain_name              = local.olap_origin_domain
    origin_access_control_id = aws_cloudfront_origin_access_control.olap_oac.id
    # S3 Origin として扱う（OAC により SigV4 署名が自動適用される）
  }

  custom_error_response {
    error_code            = 403
    error_caching_min_ttl = 0 # エラーをキャッシュしない
  }

  default_cache_behavior {
    target_origin_id       = local.olap_origin_domain
    viewer_protocol_policy = "redirect-to-https"

    # ★ポイント1: CloudFrontのキャッシュを無効化
    cache_policy_id = data.aws_cloudfront_cache_policy.caching_disabled.id

    # ★ポイント2: 可能な限り全情報をオリジンへ（Host除く）
    origin_request_policy_id = data.aws_cloudfront_origin_request_policy.all_viewer_except_host_header.id

    # ★ポイント3: 下流（ブラウザ/プロキシ）での再利用を防止
    response_headers_policy_id = aws_cloudfront_response_headers_policy.no_cache_strict.id

    allowed_methods = ["GET", "HEAD"]
    cached_methods  = ["GET", "HEAD"]
  }

  price_class      = "PriceClass_200"
  retain_on_delete = false

  restrictions {
    geo_restriction {
      restriction_type = "whitelist"
      locations        = ["JP"]
    }
  }

  viewer_certificate {
    cloudfront_default_certificate = true
    # 独自ドメインを使う場合は上を false にして ACM 証明書を指定:
    # acm_certificate_arn            = var.acm_cert_arn
    # ssl_support_method             = "sni-only"
    # minimum_protocol_version       = "TLSv1.2_2021"
  }
}

data "aws_cloudfront_origin_request_policy" "all_viewer_except_host_header" {
  name = "Managed-AllViewerExceptHostHeader"
}

resource "aws_cloudfront_response_headers_policy" "no_cache_strict" {
  name = "NoCache-Strict"

  custom_headers_config {
    items {
      header   = "Cache-Control"
      value    = "no-store, no-cache, must-revalidate, max-age=0, s-maxage=0"
      override = true
    }
    items {
      header   = "Pragma"
      value    = "no-cache"
      override = true
    }
    items {
      header   = "Expires"
      value    = "0"
      override = true
    }
  }

  # （任意）CORSが必要ならSimpleCORS相当も付けたい場合は下を活用
  # cors_config {
  #   access_control_allow_credentials = false
  #   access_control_allow_headers {
  #     items = ["*"]
  #   }
  #   access_control_allow_methods {
  #     items = ["GET", "HEAD", "OPTIONS"]
  #   }
  #   access_control_allow_origins {
  #     items = ["*"]
  #   }
  #   origin_override = true
  # }
}

data "aws_cloudfront_response_headers_policy" "simple_cors" {
  name = "Managed-SimpleCORS"
}

data "aws_cloudfront_cache_policy" "caching_disabled" {
  name = "Managed-CachingDisabled"
}
