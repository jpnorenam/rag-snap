# Dedicated network and one EC2 instance for the rag-cli OpenSearch node.
# No secret is passed to Terraform: passwords and TLS passphrases never reach
# variables, state, or user-data.

# Fails the plan unless the saved AMI is a Canonical image in this region.
# Deprecated images are included so plan and destroy keep working after the
# saved AMI passes its deprecation date.
data "aws_ami" "ubuntu" {
  owners             = ["099720109477"]
  include_deprecated = true

  filter {
    name   = "image-id"
    values = [var.ami_id]
  }

  filter {
    name   = "architecture"
    values = ["x86_64"]
  }
}

data "aws_ec2_instance_type_offerings" "azs" {
  location_type = "availability-zone"

  filter {
    name   = "instance-type"
    values = [var.instance_type]
  }
}

resource "aws_vpc" "main" {
  cidr_block           = "10.42.0.0/16"
  enable_dns_support   = true
  enable_dns_hostnames = true
  tags                 = { Name = "${var.deployment_id}-vpc" }
}

resource "aws_internet_gateway" "main" {
  vpc_id = aws_vpc.main.id
  tags   = { Name = "${var.deployment_id}-igw" }
}

resource "aws_subnet" "main" {
  vpc_id                  = aws_vpc.main.id
  cidr_block              = "10.42.1.0/24"
  availability_zone       = sort(data.aws_ec2_instance_type_offerings.azs.locations)[0]
  map_public_ip_on_launch = true
  tags                    = { Name = "${var.deployment_id}-subnet" }
}

resource "aws_route_table" "main" {
  vpc_id = aws_vpc.main.id

  route {
    cidr_block = "0.0.0.0/0"
    gateway_id = aws_internet_gateway.main.id
  }

  tags = { Name = "${var.deployment_id}-rt" }
}

resource "aws_route_table_association" "main" {
  subnet_id      = aws_subnet.main.id
  route_table_id = aws_route_table.main.id
}

resource "aws_security_group" "main" {
  name_prefix = "${var.deployment_id}-"
  description = "rag-cli OpenSearch: SSH and HTTPS 9200 from one address"
  vpc_id      = aws_vpc.main.id

  ingress {
    description = "SSH"
    from_port   = 22
    to_port     = 22
    protocol    = "tcp"
    cidr_blocks = [var.allowed_cidr]
  }

  ingress {
    description = "OpenSearch HTTPS"
    from_port   = 9200
    to_port     = 9200
    protocol    = "tcp"
    cidr_blocks = [var.allowed_cidr]
  }

  egress {
    description = "Snap Store and Ubuntu archive"
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }

  tags = { Name = "${var.deployment_id}-sg" }

  lifecycle {
    create_before_destroy = true
  }
}

resource "aws_key_pair" "main" {
  key_name   = var.deployment_id
  public_key = file(var.ssh_public_key_path)
}

resource "aws_instance" "opensearch" {
  ami                         = data.aws_ami.ubuntu.id
  instance_type               = var.instance_type
  subnet_id                   = aws_subnet.main.id
  vpc_security_group_ids      = [aws_security_group.main.id]
  key_name                    = aws_key_pair.main.key_name
  associate_public_ip_address = true

  root_block_device {
    volume_type           = "gp3"
    volume_size           = var.root_volume_gib
    encrypted             = true
    delete_on_termination = true
  }

  metadata_options {
    http_tokens = "required"
  }

  tags = { Name = "${var.deployment_id}-opensearch" }
}
