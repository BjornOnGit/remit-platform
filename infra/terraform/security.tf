resource "aws_security_group" "app" {
  name        = "${var.project}-app"
  description = "Docker Compose host"
  vpc_id      = aws_vpc.main.id

  ingress {
    description = "Public API (gateway)"
    from_port   = 3000
    to_port     = 3000
    protocol    = "tcp"
    cidr_blocks = [var.api_cidr]
  }

  # No SSH: access is through SSM Session Manager.
  egress {
    description = "All outbound (ECR, SQS, SSM, package repos)"
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }
}

resource "aws_security_group" "db" {
  name        = "${var.project}-db"
  description = "RDS Postgres"
  vpc_id      = aws_vpc.main.id

  ingress {
    description     = "Postgres from the app host only"
    from_port       = 5432
    to_port         = 5432
    protocol        = "tcp"
    security_groups = [aws_security_group.app.id]
  }
}
