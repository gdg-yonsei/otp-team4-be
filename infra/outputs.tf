output "public_ip" {
  description = "서버 공인 IP"
  value       = ncloud_public_ip.app.public_ip
}

output "ssh" {
  description = "SSH 접속 명령"
  value       = "ssh root@${ncloud_public_ip.app.public_ip}"
}

output "url" {
  value = "http://${ncloud_public_ip.app.public_ip}"
}
