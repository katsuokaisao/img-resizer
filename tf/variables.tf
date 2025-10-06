variable "region" {
  type        = string
  description = "AWS region"
  default     = "ap-northeast-1"
}

variable "bucket_name" {
  type        = string
  description = "元画像を格納するS3バケット名（既存でもOK）"
}

variable "lambda_zip_path" {
  type        = string
  description = "ビルド済みLambda zipパス（例: ./function.zip）"
  default     = "./function.zip"
}

variable "lambda_memory" {
  type    = number
  default = 768
}

variable "lambda_timeout" {
  type    = number
  default = 20
}

# 例: bot/images/{imageId}.{jpg|png}
variable "source_key_prefix" {
  type        = string
  description = "元画像キーのプレフィックス（任意・運用メモ用）"
  default     = "bot/images/"
}
