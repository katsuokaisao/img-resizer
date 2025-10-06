terraform {
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
  }
  backend "s3" {
    bucket  = "katsuoka-study-tfstate"
    key     = "terraform.tfstate"
    region  = "ap-northeast-1"
    profile = "study"
  }
}

provider "aws" {
  profile = "study"
}

data "aws_caller_identity" "current" {}
