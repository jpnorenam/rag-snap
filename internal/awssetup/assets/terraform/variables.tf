variable "aws_profile" {
  description = "AWS CLI profile."
  type        = string
}

variable "region" {
  description = "AWS region."
  type        = string
}

variable "deployment_id" {
  description = "Deployment ID used in names and the rag-cli:deployment tag."
  type        = string
}

variable "instance_type" {
  description = "EC2 instance type for the OpenSearch node."
  type        = string
  default     = "t3.xlarge"
}

variable "ami_id" {
  description = "Canonical Ubuntu amd64 AMI saved for this deployment."
  type        = string
}

variable "root_volume_gib" {
  description = "Encrypted gp3 root volume size in GiB."
  type        = number
  default     = 50
}

variable "allowed_cidr" {
  description = "Single IPv4 /32 allowed to reach SSH (22) and OpenSearch (9200)."
  type        = string

  validation {
    condition     = can(cidrhost(var.allowed_cidr, 0)) && endswith(var.allowed_cidr, "/32")
    error_message = "allowed_cidr must be an IPv4 address with /32, e.g. 203.0.113.4/32."
  }
}

variable "ssh_public_key_path" {
  description = "Path to the deployment's generated SSH public key."
  type        = string
}
