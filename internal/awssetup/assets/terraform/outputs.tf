output "public_ip" {
  description = "Public IPv4 address of the OpenSearch instance."
  value       = aws_instance.opensearch.public_ip
}

output "instance_id" {
  description = "EC2 instance ID."
  value       = aws_instance.opensearch.id
}
