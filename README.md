# 카톡 위키 API 서버

카카오톡 단체 채팅 내보내기 파일을 받아 저장하고, 나무위키 스타일 위키 문서를 관리하는 API 서버. Go + Gin + GORM.

AI 문서 생성은 **별도의 AI 에이전트 서버**가 담당한다. 이 서버는 생성 요청을 에이전트에 전달하고, 에이전트는 문서 API로 문서를 쓰고 작업 상태를 보고한다.

## 실행

```bash
cp .env.example .env   # AGENT_URL, AGENT_TOKEN 등 값 채우기
go run ./cmd/server    # :8080, SQLite(katalk-wiki.db)
go test ./...
```

## 환경 변수

서버는 시작할 때 작업 디렉터리의 `.env`를 읽습니다(파일이 없으면 무시). 이미 설정된 OS 환경 변수가 `.env`보다 우선합니다.

| 변수 | 기본값 | 설명 |
|---|---|---|
| `PORT` | `8080` | |
| `DB_DRIVER` | `sqlite` | `sqlite` 또는 `postgres` |
| `DATABASE_URL` | `katalk-wiki.db` | SQLite 파일 경로 또는 Postgres DSN |
| `PUBLIC_BASE_URL` | `https://katalk.wiki` | 위키 생성 응답의 `url` 접두사 |
| `COOKIE_SECURE` | `false` | 운영(HTTPS)에서는 `true` |
| `SESSION_TTL` | `168h` | 비밀번호 인증 세션 유효기간 |
| `MAX_UPLOAD_MB` | `50` | 업로드 최대 크기 (초과 시 413) |
| `ALLOWED_ORIGINS` | (없음) | CORS 허용 Origin, 쉼표 구분 (쿠키 포함 요청 허용) |
| `AGENT_URL` | (없음) | 생성 요청을 보낼 에이전트 서버 URL |
| `AGENT_TOKEN` | (없음) | 에이전트와 공유하는 Bearer 토큰 (양방향) |
| `AGENT_TIMEOUT` | `30s` | 에이전트 요청 타임아웃 (최대 3회 재시도) |
| `JOB_TIMEOUT` | `60m` | 이 시간 동안 끝나지 않은 작업은 다음 생성 요청 때 FAILED 처리 |

## API 문서 (Swagger)

서버를 띄운 뒤 `/swagger/index.html` (OpenAPI JSON: `/swagger/doc.json`).

핸들러의 `// @Summary ...` 주석([swaggo](https://github.com/swaggo/swag))에서 `docs/`를 생성한다. 핸들러나 DTO(`internal/handler/dto.go`)를 바꾸면 다시 생성할 것 (`infra/deploy.sh`는 빌드 전에 자동으로 실행).

```bash
go tool swag init -g cmd/server/main.go --parseInternal -o docs
```

## 구조

```
cmd/server          진입점, graceful shutdown
internal/config     환경 변수
internal/model      GORM 모델 (Wiki, WikiSession, Chat, Page, GenerationJob)
internal/service    비즈니스 로직
internal/handler    Gin 라우터/핸들러
internal/middleware 위키 로드·세션 검사, rate limit, CORS
internal/kakao      카카오톡 내보내기 파서 (TXT: PC / Android / iOS, 한/영 · CSV)
internal/agent      AI 에이전트 서버로 생성 요청 전송
agent/              AI 에이전트 서버 (Python, FastAPI + OpenAI) — 아래 "에이전트 서버" 참고
```

## AI 에이전트 연동

```
클라이언트 ──POST /generations──▶ 이 서버 ──POST {AGENT_URL}──▶ AI 에이전트
                                   ▲                              │
                                   └── 문서 API / 상태 보고 ──────┘
                                      (Authorization: Bearer AGENT_TOKEN)
```

**1. 이 서버 → 에이전트**: `POST {AGENT_URL}`, 헤더 `Authorization: Bearer {AGENT_TOKEN}`. 2xx로 응답하면 접수된 것으로 본다. 5xx/429/네트워크 오류는 최대 3회 재시도하고, 끝내 실패하면 작업은 `FAILED`.

```json
{
  "jobId": "job_9f1c...",
  "wikiId": "550e8400-...",
  "wikiName": "우리들의 위키",
  "mode": "MERGE",
  "chats": [
    {
      "id": "chat_...", "fileName": "KakaoTalk_Chat.csv", "messageCount": 1523,
      "content": "내보내기 원문 (TXT 또는 CSV)...",
      "messages": [
        { "sentAt": "2026-11-25 19:59:58", "sender": "박건휘", "text": "저 집가는중이라..." }
      ]
    }
  ]
}
```

`messages`는 서버가 TXT/CSV를 파싱한 결과라 파일 형식과 관계없이 같은 모양이다 (시간은 KST).

**2. 에이전트 → 문서 API**: 같은 Bearer 토큰으로 `/wikis/{wikiId}/pages` API를 호출한다. 토큰이 있으면 위키 비밀번호 없이 접근할 수 있고, rate limit에서도 제외된다. 에이전트가 쓴 문서는 `AI` 작성으로 기록된다.

**3. 에이전트 → 상태 보고**: `PATCH /wikis/{wikiId}/generations/{jobId}` (에이전트 토큰 필수, 아니면 `403 AGENT_ONLY`)

```json
{ "status": "COMPLETED", "createdPageIds": ["page_..."], "updatedPageIds": ["page_..."] }
```

- `status`: `PROCESSING` → `COMPLETED` 또는 `FAILED`. 실패 시 `"error": "사유"`.
- `createdPageIds`/`updatedPageIds`는 보낼 때마다 전체 목록으로 교체된다(생략하면 유지).
- 이미 끝난 작업에 보고하면 `409 JOB_ALREADY_FINISHED`.

### 에이전트 서버 (`agent/`)

위 규약을 구현한 Python 서버. `POST /jobs`를 받으면 바로 `202`로 응답하고 백그라운드에서 처리한다.

1. `PROCESSING` 보고
2. 모든 대화 파일의 `messages`를 시간순으로 합치고(중복 제거) 2단계로 생성한다 (`app/generator.py`의 `PLAN_PROMPT`, `SYSTEM_PROMPT`)
   - **계획**: 만들 문서 목록(제목, 카테고리, 다룰 내용)을 정한다. 참여자 전원(`EXCLUDED_SENDERS` 제외)의 `PERSON`은 코드가 보장한다.
   - **작성**: 문서마다 한 번씩 호출해서 그 문서 하나만 자세히 쓴다 (`WRITE_CONCURRENCY`개씩 병렬, 기본 10, 최대 `MAX_PAGES`개). system + 대화 부분이 매번 같아서 OpenAI 프롬프트 캐시가 적용된다. 한 문서가 실패해도 나머지는 저장한다.
   - 입력이 `MAX_INPUT_CHARS`를 넘으면 메시지 단위로 나눠 조각마다 계획하고, 문서는 관련 조각으로 쓴다. `MAX_CHUNKS`를 넘으면 실패 처리(비용 보호).
   - 섹션 구성이 틀리면 한 번 더 생성하고, 위키에 없는 제목으로 가는 `[[링크]]`는 일반 글자로 푼다.
3. 위키에 이미 있는 제목은 건너뛰고 `POST /pages` (문서는 Markdown)
4. `COMPLETED`(createdPageIds) 또는 `FAILED`(error) 보고

```bash
cd agent
uv sync                                         # 의존성 설치 (uv.lock 기준)
uv run --env-file ../.env uvicorn app.main:default_app --factory --port 8000
uv run pytest                                   # 테스트 (OpenAI 호출 없음)
```

로컬에서는 `.env`에 `AGENT_URL=http://localhost:8000/jobs`, `API_URL=http://localhost:8080`, `OPENAI_API_KEY`를 넣고 Go 서버와 에이전트를 둘 다 띄운다.
현재 한계: 기존 문서를 고치거나 합치지 않는다(`MERGE`도 새 문서만 추가). 처리 중 에이전트가 재시작되면 그 작업은 `JOB_TIMEOUT` 후 `FAILED`가 된다.

## 명세에서 정한 세부 동작

- **접근 제어**: 비밀번호가 있는 위키는 `/unlock`을 제외한 모든 API에 세션 쿠키(`wiki_session_{wikiId}`, HttpOnly)가 필요하다. 없으면 `401 WIKI_LOCKED`, 비밀번호가 틀리면 `403 WIKI_PASSWORD_INVALID`.
- **위키 생성자**는 생성 응답에서 바로 세션 쿠키를 받는다.
- **비밀번호 변경** (`PATCH /wikis/{id}` `{"password": "..."}`): 기존 세션을 모두 무효화하고 요청자에게만 새 세션을 준다. `"password": ""`는 비밀번호 제거.
- **위키 삭제** (`DELETE /wikis/{id}`): 문서, 대화 파일, 생성 작업, 세션까지 한 트랜잭션으로 지운다. 비밀번호 위키는 세션이 필요하다. 진행 중인 AI 작업은 막지 않으며, 그 작업은 다음 API 호출에서 404를 받고 실패한다.
- **AI 생성**: 위키당 진행 중인 작업은 하나만 허용(`409 GENERATION_IN_PROGRESS`).
  - `REGENERATE`: 에이전트에 요청을 보내기 전에 **AI가 만들고 사람이 수정하지 않은** 문서를 서버가 먼저 지운다. 사람이 쓰거나 고친 문서는 보존된다.
  - 에이전트에 전달되기 전에 서버가 재시작되면, 시작 시 다시 전달한다.
- **문서 제목**은 위키 안에서 유일하다(`409 PAGE_TITLE_DUPLICATE`). 하위 문서는 `김철수/논란 및 사건사고` 형식.
- **문서 수정**은 `version` 기반 낙관적 잠금. 불일치 시 `409 PAGE_VERSION_CONFLICT`. `title`, `category`도 수정 가능.
- **문서 본문**은 Markdown이고, 다른 문서로 가는 링크는 위키식 `[[제목]]`이다 (별칭 `[[제목|글자]]`는 AI가 쓰지 않는다). 프론트는 `[[X]]`를 `/w/{wikiId}/X` 같은 문서 링크로 바꿔 렌더링하고, 없는 제목이면 빨간 링크로 보여 주면 된다. AI 에이전트는 생성 후 위키에 없는 제목으로 가는 링크를 일반 글자로 풀어 둔다.
- **카테고리**: `PERSON`, `EVENT`, `INCIDENT`, `SCHEDULE`, `PLACE`, `GROUP`, `MEME`, `OTHER`.
- `GET /wikis/{id}/pages`는 `?category=`, `?q=`(제목 검색)를 지원한다.
- 작업 상태 응답에는 명세 필드 외에 `updatedPageIds`, `mode`, `error`(실패 시)가 추가로 포함된다.
- **Rate limit (429)**: IP당 초당 20회, unlock은 IP+위키당 분당 5회, 생성 요청은 IP+위키당 분당 1회(버스트 5), 위키 생성은 IP당 분당 10회.

## Docker

```bash
docker build -t otp-server .
docker run --rm -p 8080:8080 --env-file .env -v "$PWD/data:/data" otp-server
```

- 멀티 스테이지 빌드: `golang:1.27-alpine`에서 정적 바이너리를 만들고 `distroless/static:nonroot`에 담는다.
- SQLite DB는 컨테이너의 `/data/katalk-wiki.db`(볼륨). 컨테이너는 uid 65532로 실행되므로 마운트할 폴더에 쓰기 권한이 있어야 한다.

## 배포 (네이버 클라우드, Terraform + Docker)

`infra/`에 VPC, 서브넷, 방화벽(ACG), 서버(`mi1-g3`), 공인 IP를 만드는 Terraform 설정이 있다.

```bash
export NCLOUD_ACCESS_KEY=... NCLOUD_SECRET_KEY=...
cd infra
cp terraform.tfvars.example terraform.tfvars   # SSH 공개키, 허용 IP 입력
terraform init
terraform plan
terraform apply                                # 출력된 public_ip 확인

# 서버에 .env 채우기 (처음 한 번): AGENT_TOKEN, OPENAI_API_KEY, PUBLIC_BASE_URL 등
cp ../.env.example ../.env.prod                 # 운영 값으로 수정 (deploy.sh가 서버로 올림)

cd .. && ./infra/deploy.sh <IP>                # 이미지 2개 빌드 → 서버로 전송 → 컨테이너 교체 → /healthz 확인
```

```
[서버]  Docker 네트워크 otp
  ├─ otp-server (Go)       :80 → 8080   외부 공개
  └─ agent (Python)        :8000        외부 비공개. otp-server가 http://agent:8000/jobs 로 호출
```

- 첫 부팅 시 `init.sh.tftpl`이 SSH 키 등록, Docker 설치, `/opt/otp-server/{.env,data}` 준비를 한다.
- 레지스트리 없이 `docker save | ssh docker load`로 이미지를 보낸다. 컨테이너는 `--restart unless-stopped`라 재부팅 후에도 자동 시작된다.
- `PORT`, `DATABASE_URL`, `AGENT_URL`(otp-server)과 `API_URL`(agent)은 deploy.sh가 컨테이너 기준 값으로 고정한다(.env 값보다 우선). 두 컨테이너는 같은 `/opt/otp-server/.env`를 쓴다.
- SQLite DB는 서버의 `/opt/otp-server/data/katalk-wiki.db`(부트 디스크). 서버가 교체되지 않도록 `prevent_destroy`를 걸어 두었다.
- 로그: `ssh root@<IP> docker logs -f otp-server` / `docker logs -f agent`
- 사용 가능한 이미지 이름 확인: `ncloud_server_image_numbers`에 `output_file = "image.json"`을 넣고 `terraform plan`.
- `terraform.tfstate`에는 로그인 키의 private key가 평문으로 들어 있으므로 커밋하지 않는다(.gitignore 처리됨).
