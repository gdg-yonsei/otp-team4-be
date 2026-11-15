# syntax=docker/dockerfile:1

# ---------- 1단계: 빌드 ----------
# BUILDPLATFORM: 빌드하는 머신(Mac이면 arm64)에서 Go 컴파일러를 그대로 돌리고,
# GOOS/GOARCH로 대상 플랫폼(서버는 amd64)용 바이너리를 크로스 컴파일한다. (에뮬레이션보다 훨씬 빠름)
FROM --platform=$BUILDPLATFORM golang:1.27-alpine AS build
WORKDIR /src

# 의존성만 먼저 받아 두면, 소스만 바뀌었을 때 이 레이어는 캐시를 재사용한다.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
ARG TARGETOS TARGETARCH
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server

# ---------- 2단계: 실행 ----------
# distroless/static: 셸도 패키지 매니저도 없는 최소 이미지 (CA 인증서, 시간대 데이터만 포함).
# SQLite 드라이버가 순수 Go라 cgo/libc가 필요 없어서 static으로 충분하다.
# :nonroot 태그는 uid 65532 사용자로 실행된다.
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /out/server /app/server

# SQLite 파일은 /data 볼륨에 둔다 (컨테이너를 지우고 다시 만들어도 유지).
WORKDIR /data
VOLUME /data
ENV PORT=8080 \
    DATABASE_URL=/data/katalk-wiki.db

EXPOSE 8080
ENTRYPOINT ["/app/server"]
