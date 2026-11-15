# ---------- 네트워크 ----------

resource "ncloud_vpc" "main" {
  name            = "${var.name}-vpc"
  ipv4_cidr_block = "10.0.0.0/16"
}

resource "ncloud_subnet" "public" {
  name           = "${var.name}-public"
  vpc_no         = ncloud_vpc.main.vpc_no
  subnet         = cidrsubnet(ncloud_vpc.main.ipv4_cidr_block, 8, 1) # 10.0.1.0/24
  zone           = var.zone
  network_acl_no = ncloud_vpc.main.default_network_acl_no
  subnet_type    = "PUBLIC"
  usage_type     = "GEN"
}

# ---------- 방화벽 (ACG) ----------
# Micro 서버는 네트워크 인터페이스를 따로 붙일 수 없어서, 별도 ACG를 만들지 않고
# VPC의 기본 ACG에 규칙을 넣는다. 서버의 기본 NIC에 이 ACG가 적용된다.
# 주의: 이 리소스가 기본 ACG의 규칙 전체를 관리하므로, 콘솔에서 넣은 규칙은 덮어써진다.

resource "ncloud_access_control_group_rule" "default" {
  access_control_group_no = ncloud_vpc.main.default_access_control_group_no

  inbound {
    protocol    = "TCP"
    ip_block    = var.ssh_allowed_cidr
    port_range  = "22"
    description = "SSH"
  }
  inbound {
    protocol    = "TCP"
    ip_block    = "0.0.0.0/0"
    port_range  = "80"
    description = "HTTP"
  }
  inbound {
    protocol    = "TCP"
    ip_block    = "0.0.0.0/0"
    port_range  = "443"
    description = "HTTPS (Caddy 등을 붙일 때)"
  }

  outbound {
    protocol    = "TCP"
    ip_block    = "0.0.0.0/0"
    port_range  = "1-65535"
    description = "all TCP"
  }
  outbound {
    protocol    = "UDP"
    ip_block    = "0.0.0.0/0"
    port_range  = "1-65535"
    description = "all UDP (DNS, NTP)"
  }
  outbound {
    protocol    = "ICMP"
    ip_block    = "0.0.0.0/0"
    description = "ping"
  }
}

# ---------- 서버 ----------

# 로그인 키: 콘솔에서 관리자 비밀번호를 확인할 때 쓰는 키.
# 접속은 아래 init script로 등록하는 SSH 공개키로 한다.
# private_key가 state 파일에 평문으로 저장되므로 state 파일을 공유/커밋하지 말 것.
resource "ncloud_login_key" "main" {
  key_name = "${var.name}-key"
}

# 첫 부팅 때 한 번 실행: SSH 키 등록, 앱 사용자/폴더, systemd 서비스 준비
resource "ncloud_init_script" "main" {
  name = "${var.name}-init"
  content = templatefile("${path.module}/init.sh.tftpl", {
    ssh_public_key = trimspace(var.ssh_public_key)
  })
}

data "ncloud_server_image_numbers" "ubuntu" {
  server_image_name = var.image_name
  filter {
    name   = "hypervisor_type"
    values = ["KVM"]
  }
}

resource "ncloud_server" "app" {
  name                = var.name
  subnet_no           = ncloud_subnet.public.id
  server_image_number = data.ncloud_server_image_numbers.ubuntu.image_number_list[0].server_image_number
  server_spec_code    = var.server_spec_code
  login_key_name      = ncloud_login_key.main.key_name
  init_script_no      = ncloud_init_script.main.id

  # 요금제: MTRAT(시간 요금제, 기본값) / FXSUM(월 정액).
  # Micro 서버는 시간 요금제로 만들 수 없어서 FXSUM이어야 한다.
  fee_system_type_code = startswith(var.server_spec_code, "mi") ? "FXSUM" : "MTRAT"

  # SQLite DB가 이 서버의 부트 디스크에 있으므로, 실수로 서버가 교체/삭제되지 않게 막는다.
  # 정말 지우려면 이 블록을 지우고 apply한 뒤 destroy 한다.
  lifecycle {
    prevent_destroy = true
  }
}

resource "ncloud_public_ip" "app" {
  server_instance_no = ncloud_server.app.instance_no
  description        = "${var.name} public ip"
}
