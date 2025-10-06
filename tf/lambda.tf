resource "aws_lambda_function" "resizer" {
  function_name = "img-resizer-olap"
  filename      = var.lambda_zip_path
  role          = aws_iam_role.lambda_role.arn
  handler       = "bootstrap" # provided.al2 の場合
  runtime       = "provided.al2"
  architectures = ["arm64"]

  timeout     = var.lambda_timeout
  memory_size = var.lambda_memory

  environment {
    variables = {
      SOURCE_KEY_PREFIX = var.source_key_prefix
      BUCKET_NAME       = var.bucket_name
    }
  }
}

resource "aws_iam_role" "lambda_role" {
  name = "img-resizer-olap-role"

  assume_role_policy = jsonencode({
    Version = "2012-10-17",
    Statement = [{
      Effect    = "Allow",
      Principal = { Service = "lambda.amazonaws.com" },
      Action    = "sts:AssumeRole"
    }]
  })
}

resource "aws_iam_role_policy_attachment" "lambda_basic" {
  role       = aws_iam_role.lambda_role.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole"
}

# 画像取得とOLAP応答のための最小ポリシー
data "aws_iam_policy_document" "lambda_access" {
  statement {
    sid       = "WriteOlapResponse"
    effect    = "Allow"
    actions   = ["s3-object-lambda:WriteGetObjectResponse"]
    resources = ["*"]
  }

  statement {
    sid       = "GetSourceObjects"
    effect    = "Allow"
    actions   = ["s3:GetObject", "s3:HeadObject"]
    resources = ["arn:aws:s3:::${var.bucket_name}/*"]
  }

  statement {
    sid       = "ListBucket"
    effect    = "Allow"
    actions   = ["s3:ListBucket"]
    resources = ["arn:aws:s3:::${var.bucket_name}"]
  }

}

resource "aws_iam_policy" "lambda_access" {
  name   = "img-resizer-olap-access"
  policy = data.aws_iam_policy_document.lambda_access.json
}

resource "aws_iam_role_policy_attachment" "lambda_access_attach" {
  role       = aws_iam_role.lambda_role.name
  policy_arn = aws_iam_policy.lambda_access.arn
}

resource "aws_lambda_permission" "allow_cloudfront_invoke" {
  statement_id  = "AllowCloudFrontInvoke"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.resizer.function_name
  principal     = "cloudfront.amazonaws.com"
  source_arn    = "arn:aws:cloudfront::${data.aws_caller_identity.current.account_id}:distribution/${aws_cloudfront_distribution.this.id}"
}
