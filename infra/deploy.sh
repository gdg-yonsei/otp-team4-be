#!/bin/bash
# 사용법: ./infra/deploy.sh <서버 IP>
# Docker 이미지 2개(Go API 서버, Python AI 에이전트)를 로컬에서 빌드해 서버로 바로 전송하고(레지스트리 없이),
# 컨테이너를 교체한다. 두 컨테이너는 Docker 네트워크 otp 로 연결된다.
#
#   otp-server  :80 -> 8080   외부 공개
#   agent       :8000         외부 비공개 (otp 네트워크 안에서만 http://agent:8000)
#
# 프로젝트 루트에 .env.prod 가 있으면 서버의 /opt/otp-server/.env 로 올린다 (git에는 안 올라감).
# 없으면 서버에 이미 있는 .env 를 그대로 쓴다. 두 컨테이너가 같은 파일을 쓴다.
# 서로의 주소(AGENT_URL, API_URL)는 아래에서 -e 로 덮어쓰므로 .env 에 적을 필요 없다.
set -euo pipefail

HOST="${1:?usage: $0 <server-ip>}"
API_IMAGE="otp-server:latest"
AGENT_IMAGE="otp-agent:latest"
cd "$(dirname "$0")/.."

if [ -f .env.prod ] && ! grep -q '^OPENAI_API_KEY=.' .env.prod; then
  echo "!! .env.prod 에 OPENAI_API_KEY 가 없습니다. 에이전트가 시작되지 않습니다." >&2
  exit 1
fi

echo "==> swagger docs"
go tool swag init -g cmd/server/main.go --parseInternal -o docs --quiet

echo "==> build ($API_IMAGE, $AGENT_IMAGE; linux/amd64)"
docker build --platform linux/amd64 -t "$API_IMAGE" .
docker build --platform linux/amd64 -t "$AGENT_IMAGE" agent

if [ -f .env.prod ]; then
  echo "==> upload .env.prod -> /opt/otp-server/.env"
  ssh "root@${HOST}" 'umask 077; cat > /opt/otp-server/.env' < .env.prod
fi

echo "==> upload (docker save | ssh docker load)"
docker save "$API_IMAGE" "$AGENT_IMAGE" | gzip | ssh "root@${HOST}" 'gunzip | docker load'

echo "==> restart containers"
ssh "root@${HOST}" "
  set -e
  docker network inspect otp >/dev/null 2>&1 || docker network create otp >/dev/null

  docker rm -f agent >/dev/null 2>&1 || true
  docker run -d --name agent \
    --restart unless-stopped \
    --network otp \
    --env-file /opt/otp-server/.env \
    -e API_URL=http://otp-server:8080 \
    $AGENT_IMAGE

  docker rm -f otp-server >/dev/null 2>&1 || true
  docker run -d --name otp-server \
    --restart unless-stopped \
    --network otp \
    -p 80:8080 \
    -v /opt/otp-server/data:/data \
    --env-file /opt/otp-server/.env \
    -e PORT=8080 \
    -e DATABASE_URL=/data/katalk-wiki.db \
    -e AGENT_URL=http://agent:8000/jobs \
    $API_IMAGE

  docker image prune -f >/dev/null
  docker ps --filter name=otp-server --filter name=agent --format '{{.Names}}  {{.Status}}'
"

echo "==> health check"
sleep 3
curl -fsS -o /dev/null -w "otp-server healthz: %{http_code}\n" "http://${HOST}/healthz" \
  || echo "otp-server 실패: ssh root@${HOST} docker logs otp-server"
ssh "root@${HOST}" "docker exec agent python -c \"import urllib.request as u; print('agent healthz:', u.urlopen('http://localhost:8000/healthz').status)\"" \
  || echo "agent 실패: ssh root@${HOST} docker logs agent"
