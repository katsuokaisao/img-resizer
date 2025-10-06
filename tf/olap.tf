# Object Lambda Access Point（変換はGetObject）
resource "aws_s3control_object_lambda_access_point" "olap" {
  name = "img-resizer-olap-ap"

  configuration {
    supporting_access_point = aws_s3_access_point.supporting_ap.arn

    transformation_configuration {
      actions = ["GetObject"]

      content_transformation {
        aws_lambda {
          function_arn = aws_lambda_function.resizer.arn
        }
      }
    }
  }
}

# OLAP APのアクセスポリシー
# AWS公式: CloudFront Distribution からのアクセスのみを許可
data "aws_iam_policy_document" "olap_policy_strict" {
  statement {
    effect = "Allow"
    principals {
      type        = "Service"
      identifiers = ["cloudfront.amazonaws.com"]
    }
    actions = ["s3-object-lambda:Get*"]
    resources = [
      aws_s3control_object_lambda_access_point.olap.arn,
    ]
    condition {
      test     = "StringEquals"
      variable = "aws:SourceArn"
      values   = [aws_s3control_object_lambda_access_point.olap.arn]
    }
  }
}

resource "aws_s3control_object_lambda_access_point_policy" "olap_policy" {
  name   = aws_s3control_object_lambda_access_point.olap.name
  policy = data.aws_iam_policy_document.olap_policy_strict.json
}
