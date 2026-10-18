locals {
  services = ["api-gateway", "fx-service", "ledger", "settlement-worker", "reconciliation"]
}

resource "aws_ecr_repository" "service" {
  for_each             = toset(local.services)
  name                 = "remit/${each.key}"
  image_tag_mutability = "MUTABLE"
  force_delete         = true # dev: allow `terraform destroy` with images present

  image_scanning_configuration {
    scan_on_push = true
  }
}

resource "aws_ecr_lifecycle_policy" "service" {
  for_each   = aws_ecr_repository.service
  repository = each.value.name

  policy = jsonencode({
    rules = [{
      rulePriority = 1
      description  = "Keep the last 10 images"
      selection = {
        tagStatus   = "any"
        countType   = "imageCountMoreThan"
        countNumber = 10
      }
      action = { type = "expire" }
    }]
  })
}
