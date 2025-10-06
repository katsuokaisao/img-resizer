# 既存のバケットを使う場合はこのresourceを削除し、dataに置き換えてもOK
resource "aws_s3_bucket" "source" {
  bucket = var.bucket_name
}

resource "aws_s3_bucket_public_access_block" "source" {
  bucket                  = aws_s3_bucket.source.id
  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

resource "aws_s3_bucket_policy" "source" {
  bucket = aws_s3_bucket.source.id
  policy = jsonencode({
    Version = "2012-10-17",
    Statement = [{
      Effect    = "Allow",
      Principal = { AWS = "*" },
      Action    = "s3:*",
      Resource = [
        aws_s3_bucket.source.arn,
        "${aws_s3_bucket.source.arn}/*"
      ],
      Condition = {
        StringEquals = {
          "s3:DataAccessPointAccount" = data.aws_caller_identity.current.account_id
        }
      }
    }]
  })
}

# S3 Access Point（OLAPのsupporting AP）
resource "aws_s3_access_point" "supporting_ap" {
  name   = "${var.bucket_name}-ap"
  bucket = aws_s3_bucket.source.id
}

# Supporting S3 Access Point は「OLAP経由」のアクセスのみを許可
# AWS公式: aws:CalledVia 条件でOLAP経由であることを検証
data "aws_iam_policy_document" "supporting_ap_policy" {
  statement {
    sid    = "s3objlambda"
    effect = "Allow"
    principals {
      type        = "Service"
      identifiers = ["cloudfront.amazonaws.com"]
    }
    actions = ["s3:*"]
    resources = [
      aws_s3_access_point.supporting_ap.arn,
      "${aws_s3_access_point.supporting_ap.arn}/object/*"
    ]
    condition {
      test     = "ForAnyValue:StringEquals"
      variable = "aws:CalledVia"
      values   = ["s3-object-lambda.amazonaws.com"]
    }
  }
}

resource "aws_s3control_access_point_policy" "supporting_ap_policy" {
  access_point_arn = aws_s3_access_point.supporting_ap.arn
  policy           = data.aws_iam_policy_document.supporting_ap_policy.json
}
