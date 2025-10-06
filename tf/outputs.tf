locals {
  olap_domain = "${aws_s3control_object_lambda_access_point.olap.name}-${data.aws_caller_identity.current.account_id}.s3-object-lambda.${var.region}.amazonaws.com"
}

output "olap_endpoint_base" {
  description = "S3 Object Lambda Access Point のFQDN（ベースURL）"
  value       = "https://${local.olap_domain}"
}

output "example_urls" {
  description = "例: 5サイズのURL（CloudFront経由）"
  value = [
    "https://${aws_cloudfront_distribution.this.domain_name}/bot/images/rm001/240",
    "https://${aws_cloudfront_distribution.this.domain_name}/bot/images/rm001/300",
    "https://${aws_cloudfront_distribution.this.domain_name}/bot/images/rm001/460",
    "https://${aws_cloudfront_distribution.this.domain_name}/bot/images/rm001/700",
    "https://${aws_cloudfront_distribution.this.domain_name}/bot/images/rm001/1040",
  ]
}

output "cloudfront_domain_name" {
  value       = aws_cloudfront_distribution.this.domain_name
  description = "例: dxxxxxxxxxxxx.cloudfront.net"
}
