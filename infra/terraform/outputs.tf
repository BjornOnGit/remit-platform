output "instance_id" {
  value = aws_instance.app.id
}

output "instance_public_ip" {
  value = aws_instance.app.public_ip
}

output "rds_endpoint" {
  value = aws_db_instance.main.address
}

output "rds_secret_arn" {
  description = "Secrets Manager secret holding the generated DB master password"
  value       = aws_db_instance.main.master_user_secret[0].secret_arn
}

output "sqs_queue_name" {
  value = aws_sqs_queue.events.name
}

output "sqs_queue_url" {
  value = aws_sqs_queue.events.url
}

output "ecr_registry" {
  description = "Use as IMAGE_REGISTRY (with a trailing slash)"
  value       = "${data.aws_caller_identity.current.account_id}.dkr.ecr.${var.region}.amazonaws.com/"
}

output "ecr_repository_urls" {
  value = { for k, r in aws_ecr_repository.service : k => r.repository_url }
}

data "aws_caller_identity" "current" {}
