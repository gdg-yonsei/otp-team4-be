"""AI 에이전트 서버.

Go 서버가 POST /jobs 로 작업을 넘기면 즉시 202로 접수하고, 백그라운드에서
  1. PATCH .../generations/{jobId}  PROCESSING
  2. 대화 메시지 → OpenAI로 문서 생성
  3. POST /wikis/{wikiId}/pages     (같은 제목이 있으면 건너뜀)
  4. PATCH .../generations/{jobId}  COMPLETED(createdPageIds) 또는 FAILED(error)
를 수행한다. Go 서버 쪽 요청 타임아웃(AGENT_TIMEOUT)이 짧기 때문에 생성이 끝날 때까지
응답을 붙잡고 있으면 안 된다.
"""

from __future__ import annotations

import asyncio
import hmac
import logging
from contextlib import asynccontextmanager
from typing import Literal

from fastapi import Depends, FastAPI, Header, HTTPException, Request
from pydantic import BaseModel

from .backend import Backend, Conflict
from .config import Settings
from .generator import Generator, OpenAIGenerator

log = logging.getLogger("agent")


# ---------- Go 서버가 보내는 요청 (internal/agent/client.go 의 Request) ----------

class Message(BaseModel):
    sentAt: str
    sender: str
    text: str


class Chat(BaseModel):
    id: str
    fileName: str
    messageCount: int
    content: str = ""
    messages: list[Message] = []


class JobRequest(BaseModel):
    jobId: str
    wikiId: str
    wikiName: str
    mode: Literal["MERGE", "REGENERATE"]
    chats: list[Chat]


# ---------- 작업 처리 ----------

def collect_messages(chats: list[Chat]) -> list[dict]:
    """여러 대화 파일의 메시지를 시간순으로 합친다. 겹쳐 올린 파일의 중복 메시지는 한 번만."""
    seen = set()
    out = []
    for chat in chats:
        for m in chat.messages:
            key = (m.sentAt, m.sender, m.text)
            if key in seen:
                continue
            seen.add(key)
            out.append({"time": m.sentAt, "author": m.sender, "text": m.text})
    out.sort(key=lambda m: m["time"])  # "YYYY-MM-DD HH:MM:SS" 라 문자열 정렬 = 시간 정렬
    return out


async def run_job(job: JobRequest, backend: Backend, generator: Generator) -> None:
    wiki, jid = job.wikiId, job.jobId
    try:
        await backend.report(wiki, jid, "PROCESSING")
    except Conflict:
        log.info("job %s already finished, skipping", jid)
        return

    created: list[str] = []
    try:
        messages = collect_messages(job.chats)
        if not messages:
            raise ValueError("대화에서 메시지를 찾지 못했습니다.")

        # MERGE든 REGENERATE든 지금 남아 있는 문서는 건드리지 않는다.
        # (REGENERATE면 Go 서버가 사람이 손대지 않은 AI 문서를 이미 지워 둔 상태)
        existing = await backend.list_titles(wiki)
        pages = await generator.generate(messages, existing)

        for page in pages:
            try:
                created.append(await backend.create_page(wiki, page))
            except Conflict:
                log.info("title already exists, skipped: %s", page["title"])
        await backend.report(wiki, jid, "COMPLETED", created_page_ids=created)
        log.info("job %s completed: %d pages", jid, len(created))
    except Exception as e:
        log.exception("job %s failed", jid)
        try:
            await backend.report(wiki, jid, "FAILED", created_page_ids=created, error=str(e)[:500])
        except Exception:
            log.exception("report FAILED for job %s", jid)


# ---------- 앱 ----------

def create_app(settings: Settings, generator: Generator | None = None, backend: Backend | None = None) -> FastAPI:
    tasks: set[asyncio.Task] = set()
    seen_jobs: set[str] = set()      # Go 서버 재시도로 같은 작업이 두 번 와도 한 번만 실행
    sem = asyncio.Semaphore(settings.max_concurrent_jobs)

    @asynccontextmanager
    async def lifespan(app: FastAPI):
        app.state.backend = backend or Backend(settings.api_url, settings.agent_token)
        app.state.generator = generator or OpenAIGenerator(
            api_key=settings.openai_api_key,
            model=settings.openai_model,
            max_input_chars=settings.max_input_chars,
            max_output_tokens=settings.max_output_tokens,
            max_chunks=settings.max_chunks,
            excluded_senders=settings.excluded_senders,
            max_pages=settings.max_pages,
            write_concurrency=settings.write_concurrency,
            plan_reasoning_effort=settings.plan_reasoning_effort or None,
            write_reasoning_effort=settings.write_reasoning_effort or None,
        )
        yield
        for t in tasks:
            t.cancel()
        await app.state.backend.aclose()

    app = FastAPI(title="katalk-wiki agent", lifespan=lifespan)

    def require_token(authorization: str = Header(default="")) -> None:
        token = authorization.removeprefix("Bearer ")
        if not hmac.compare_digest(token.encode(), settings.agent_token.encode()):
            raise HTTPException(status_code=401, detail="invalid agent token")

    async def guarded(job: JobRequest, backend: Backend, gen: Generator) -> None:
        async with sem:
            await run_job(job, backend, gen)

    @app.post("/jobs", status_code=202, dependencies=[Depends(require_token)])
    async def accept_job(job: JobRequest, request: Request) -> dict:
        if job.jobId in seen_jobs:
            return {"accepted": True, "duplicate": True}
        seen_jobs.add(job.jobId)
        task = asyncio.create_task(guarded(job, request.app.state.backend, request.app.state.generator))
        tasks.add(task)
        task.add_done_callback(tasks.discard)
        log.info("accepted job %s (wiki %s, %d chats)", job.jobId, job.wikiId, len(job.chats))
        return {"accepted": True}

    @app.get("/healthz")
    async def healthz() -> dict:
        return {"ok": True, "running": len(tasks)}

    return app


def default_app() -> FastAPI:
    """환경 변수로 설정한 앱. 실행: uvicorn app.main:default_app --factory"""
    logging.basicConfig(level=logging.INFO, format="%(asctime)s %(levelname)s %(name)s %(message)s")
    return create_app(Settings.from_env())
