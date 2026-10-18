variable "region" {
  description = "AWS region"
  type        = string
  default     = "eu-west-1"
}

variable "project" {
  description = "Name prefix for all resources"
  type        = string
  default     = "remit"
}

variable "instance_type" {
  description = "EC2 instance type for the Docker Compose host"
  type        = string
  default     = "t3.small"
}

variable "db_instance_class" {
  type    = string
  default = "db.t4g.micro"
}

variable "db_name" {
  type    = string
  default = "remit"
}

variable "db_username" {
  type    = string
  default = "remit"
}

variable "api_cidr" {
  description = "CIDR allowed to reach the public API on port 3000"
  type        = string
  default     = "0.0.0.0/0"
}
