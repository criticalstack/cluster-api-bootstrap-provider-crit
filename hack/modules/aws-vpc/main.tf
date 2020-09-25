provider "aws" {
  region = "us-east-1"
}

variable "name" {
  type = string
}

module "main_sg" {
  source = "terraform-aws-modules/security-group/aws"

  name        = "${var.name}-capi-sg"
  description = "${var.name}-capi"
  vpc_id      = module.vpc.vpc_id

  ingress_with_self = [
    {
      from_port = 7980
      to_port   = 7980
      protocol  = "udp"
      self      = true
    },
    {
      from_port = 7980
      to_port   = 7980
      protocol  = "tcp"
      self      = true
    },
  ]

  ingress_with_cidr_blocks = [
    {
      from_port   = 22
      to_port     = 22
      protocol    = "tcp"
      cidr_blocks = "0.0.0.0/0"
    },
  ]

  egress_with_cidr_blocks = [
    {
      rule        = "all-all"
      cidr_blocks = "0.0.0.0/0"
    },
  ]
}

module "vpc" {
  source = "terraform-aws-modules/vpc/aws"

  name = var.name

  cidr = "10.0.0.0/16"

  azs             = ["us-east-1c", "us-east-1d"]
  private_subnets = ["10.0.4.0/24", "10.0.5.0/24"]
  public_subnets  = ["10.0.6.0/24"]

  enable_ipv6 = false

  enable_nat_gateway = true
  single_nat_gateway = true

  public_subnet_tags = {
    Name                                   = "${var.name}-subnet-public"
    "kubernetes.io/cluster/${var.name}" = "shared"
    "kubernetes.io/role/elb"               = "1"
  }

  private_subnet_tags = {
    Name                                   = "${var.name}-subnet-private"
    "kubernetes.io/cluster/${var.name}" = "shared"
    "kubernetes.io/role/internal-elb"      = "1"
  }

  public_route_table_tags = {
    "kubernetes.io/cluster/${var.name}" = "shared"
  }
  private_route_table_tags = {
    "kubernetes.io/cluster/${var.name}" = "shared"
  }

  vpc_tags = {
    Name = "${var.name}-vpc"
  }

  tags = {
    Owner       = "user"
    Environment = "dev"
  }
}
