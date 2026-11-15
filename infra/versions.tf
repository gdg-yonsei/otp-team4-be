terraform {
  required_version = ">= 1.5"

  required_providers {
    ncloud = {
      source  = "NaverCloudPlatform/ncloud"
      version = "~> 4.0" # 4.x 안에서만 업데이트 (작성 시점 v4.0.7로 검증)
    }
  }
}

# 인증 키는 코드에 넣지 않고 환경 변수로 준다.
#   export NCLOUD_ACCESS_KEY=...
#   export NCLOUD_SECRET_KEY=...
provider "ncloud" {
  region      = "KR"
  support_vpc = true
}
