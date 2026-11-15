variable "name" {
  description = "리소스 이름 접두사"
  type        = string
  default     = "otp-server"
}

variable "zone" {
  description = "서버를 만들 존"
  type        = string
  default     = "KR-2"
}

variable "server_spec_code" {
  description = "서버 스펙. mi1-g3은 계정당 1대, 체험용(성능 보장 없음)"
  type        = string
  default     = "mi1-g3"
}

variable "image_name" {
  description = "OS 이미지 이름. 사용 가능한 목록은 README의 방법으로 확인"
  type        = string
  default     = "ubuntu-22.04-base"
}

variable "ssh_public_key" {
  description = "서버 root 계정에 등록할 SSH 공개키 (예: ~/.ssh/id_ed25519.pub 내용)"
  type        = string
}

variable "ssh_allowed_cidr" {
  description = "SSH(22) 접속을 허용할 IP 대역. 내 IP/32 로 좁히는 것을 권장"
  type        = string
  default     = "0.0.0.0/0"
}
