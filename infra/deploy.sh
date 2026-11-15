#!/bin/bash
# 사용법: ./infra/deploy.sh <서버 IP>
# Docker 이미지를 로컬에서 빌드해서 서버로 바로 전송하고(레지스트리 없이), 컨테이너를 교체한다.
# 프로젝트 루트에 .env.prod 가 있으면 서버의 /opt/otp-server/.env 로 올린다 (git에는 안 올라감).
# 없으면 서버에 이미 있는 .env 를 그대로 쓴다.
set -euo pipefail

HOST="${1:?usage: $0 <server-ip>}"
IMAGE="otp-server:latest"
cd "$(dirname "$0")/.."

echo "==> swagger docs"
go tool swag init -g cmd/server/main.go --parseInternal -o docs --quiet

echo "==> build ($IMAGE, linux/amd64)"
docker build --platform linux/amd64 -t "$IMAGE" .

if [ -f .env.prod ]; then
  echo "==> upload .env.prod -> /opt/otp-server/.env"
  ssh "root@${HOST}" 'umask 077; cat > /opt/otp-server/.env' < .env.prod
fi

echo "==> upload (docker save | ssh docker load)"
docker save "$IMAGE" | gzip | ssh "root@${HOST}" 'gunzip | docker load'

echo "==> restart container"
ssh "root@${HOST}" "
  set -e
  docker rm -f otp-server >/dev/null 2>&1 || true
  docker run -d --name otp-server \
    --restart unless-stopped \
    -p 80:8080 \
    -v /opt/otp-server/data:/data \
    --env-file /opt/otp-server/.env \
    -e PORT=8080 \
    -e DATABASE_URL=/data/katalk-wiki.db \
    $IMAGE
  docker image prune -f >/dev/null
  docker ps --filter name=otp-server --format '{{.Names}}  {{.Status}}'
"

echo "==> health check"
sleep 2
curl -fsS -o /dev/null -w "healthz: %{http_code}\n" "http://${HOST}/healthz" \
  || echo "healthz 실패: ssh root@${HOST} docker logs otp-server 로 확인"
