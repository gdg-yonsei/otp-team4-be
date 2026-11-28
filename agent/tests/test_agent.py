import asyncio
import json

import pytest
from fastapi.testclient import TestClient

from app.backend import Conflict
from app.config import Settings
from app.generator import WikiGeneration, WikiPage, chunk_messages, validate_pages
from app.main import JobRequest, collect_messages, create_app, run_job

PERSON = "## 기본정보\n|항목|내용|\n## 개요\n## 인간관계\n## 사건\n## 어록\n## 여담\n"


def job(**kw) -> JobRequest:
    data = {
        "jobId": "job_1", "wikiId": "w1", "wikiName": "테스트", "mode": "MERGE",
        "chats": [{
            "id": "chat_1", "fileName": "a.csv", "messageCount": 2,
            "messages": [
                {"sentAt": "2026-11-25 20:00:24", "sender": "김민정", "text": "저도..."},
                {"sentAt": "2026-11-25 19:59:58", "sender": "박건휘", "text": "집가는중"},
            ],
        }],
    }
    data.update(kw)
    return JobRequest.model_validate(data)


class FakeBackend:
    def __init__(self, titles=()):
        self.titles = list(titles)
        self.reports = []
        self.created = []

    async def report(self, wiki_id, job_id, status, created_page_ids=None, error=None):
        self.reports.append((status, created_page_ids, error))

    async def list_titles(self, wiki_id):
        return list(self.titles)

    async def create_page(self, wiki_id, page):
        if page["title"] in self.titles:
            raise Conflict("dup")
        self.titles.append(page["title"])
        self.created.append(page)
        return f"page_{len(self.created)}"

    async def aclose(self):
        pass


class FakeGenerator:
    def __init__(self, pages=None, error=None):
        self.pages, self.error, self.calls = pages or [], error, []

    async def generate(self, messages, existing_titles):
        self.calls.append((messages, existing_titles))
        if self.error:
            raise self.error
        return self.pages


# ---------- generator ----------

def test_validate_pages_ok():
    out = validate_pages(WikiGeneration(created_pages=[WikiPage(title=" 박건휘 ", category="PERSON", content=PERSON)]))
    assert out == [{"title": "박건휘", "category": "PERSON", "content": PERSON.strip()}]


def test_validate_pages_rejects_wrong_sections_and_duplicates():
    with pytest.raises(ValueError, match="섹션"):
        validate_pages(WikiGeneration(created_pages=[WikiPage(title="a", category="EVENT", content=PERSON)]))
    dup = [WikiPage(title="박건휘", category="PERSON", content=PERSON),
           WikiPage(title="박건휘 ", category="PERSON", content=PERSON)]
    with pytest.raises(ValueError, match="중복"):
        validate_pages(WikiGeneration(created_pages=dup))


def test_chunk_messages_keeps_messages_whole():
    msgs = [{"text": "x" * 50} for _ in range(10)]
    chunks = chunk_messages(msgs, 200)
    assert sum(len(c) for c in chunks) == 10
    assert all(len(c) >= 1 for c in chunks) and len(chunks) > 1


# ---------- job flow ----------

def test_collect_messages_sorts_and_dedupes():
    j = job()
    j.chats.append(j.chats[0].model_copy())  # 같은 파일을 두 번 올린 경우
    msgs = collect_messages(j.chats)
    assert [m["author"] for m in msgs] == ["박건휘", "김민정"]


def test_run_job_creates_pages_and_skips_existing():
    backend = FakeBackend(titles=["김민정"])
    gen = FakeGenerator(pages=[
        {"title": "박건휘", "category": "PERSON", "content": PERSON},
        {"title": "김민정", "category": "PERSON", "content": PERSON},  # 이미 있음 → 409 → 건너뜀
    ])
    asyncio.run(run_job(job(), backend, gen))
    assert gen.calls[0][1] == ["김민정"]
    assert backend.reports == [("PROCESSING", None, None), ("COMPLETED", ["page_1"], None)]


def test_run_job_reports_failure():
    backend = FakeBackend()
    asyncio.run(run_job(job(), backend, FakeGenerator(error=RuntimeError("boom"))))
    assert backend.reports[-1] == ("FAILED", [], "boom")


# ---------- HTTP ----------

def test_jobs_endpoint_auth_and_dedupe():
    backend, gen = FakeBackend(), FakeGenerator()
    app = create_app(Settings(agent_token="secret", openai_api_key="x"), generator=gen, backend=backend)
    body = job().model_dump()
    with TestClient(app) as c:
        assert c.post("/jobs", json=body).status_code == 401
        assert c.post("/jobs", json=body, headers={"Authorization": "Bearer wrong"}).status_code == 401
        r = c.post("/jobs", json=body, headers={"Authorization": "Bearer secret"})
        assert r.status_code == 202 and r.json() == {"accepted": True}
        r = c.post("/jobs", json=body, headers={"Authorization": "Bearer secret"})
        assert r.json()["duplicate"] is True
    assert len(gen.calls) == 1


# ---------- 참여자 전원 PERSON ----------

from app.generator import OpenAIGenerator, participants_of  # noqa: E402


def msgs(*authors):
    return [{"time": f"2026-11-25 20:{i:02d}:00", "author": a, "text": "hi"} for i, a in enumerate(authors)]


def test_participants_of_sorted_by_count_and_excludes_bots():
    m = msgs("A", "B", "B", "채팅봇", "C", "B", "A")
    assert participants_of(m, frozenset({"채팅봇"})) == ["B", "A", "C"]


EVENT = "## 개요\n## 참여자\n## 일정\n## 사건 내용\n## 진행 상태\n"


def make_gen(plans, contents=None, fail=()):
    """_parse를 가짜로 바꾼 생성기.
    plans: 조각마다 돌려줄 계획 [(title, category), ...]
    contents: 제목별 본문 (없으면 카테고리 기본 본문)
    fail: 작성에 실패할 제목
    """
    from app.generator import PagePlan, WikiPage, WikiPlan
    gen = OpenAIGenerator(api_key="x", model="m", max_input_chars=100_000, max_output_tokens=1000,
                          max_chunks=8, excluded_senders=frozenset({"채팅봇"}))
    calls = {"plan": [], "write": []}
    plans = list(plans)

    async def fake_parse(system, chat, task, schema):
        if schema is WikiPlan:
            calls["plan"].append(task)
            return WikiPlan(pages=[PagePlan(title=t, category=c, summary="s") for t, c in plans.pop(0)])
        t = task["target"]
        calls["write"].append(task)
        if t["title"] in fail:
            raise RuntimeError("boom")
        body = (contents or {}).get(t["title"]) or (PERSON if t["category"] == "PERSON" else EVENT)
        return WikiPage(title="모델이 바꾼 제목", category=t["category"], content=body)

    gen._parse = fake_parse
    return gen, calls


def test_generate_plans_then_writes_each_page():
    gen, calls = make_gen([[("A", "PERSON"), ("모임", "EVENT")]])
    pages = asyncio.run(gen.generate(msgs("A", "B", "C", "채팅봇", "A"), existing_titles=["D"]))
    # 참여자 전원 PERSON(발언 많은 순) → 그 밖의 문서. 제목은 계획대로 고정.
    assert [p["title"] for p in pages] == ["A", "B", "C", "모임"]
    assert calls["plan"][0]["existing_page_titles"] == ["D"]
    assert len(calls["write"]) == 4
    assert "A" not in calls["write"][0]["link_titles"] and "모임" in calls["write"][0]["link_titles"]


def test_generate_skips_existing_and_survives_one_failure():
    gen, calls = make_gen([[("A", "PERSON"), ("모임", "EVENT")]], fail={"모임"})
    pages = asyncio.run(gen.generate(msgs("A", "B"), existing_titles=["a"]))  # 대소문자 무시
    assert [p["title"] for p in pages] == ["B"]


def test_generate_fails_when_every_page_fails():
    gen, _ = make_gen([[]], fail={"A"})
    with pytest.raises(RuntimeError, match="모든 문서"):
        asyncio.run(gen.generate(msgs("A"), existing_titles=[]))


def test_generate_cleans_links_against_created_titles():
    gen, _ = make_gen([[("모임", "EVENT")]],
                      contents={"A": PERSON + "[[B]], [[모임]], [[유령]]"}, fail={"모임"})
    pages = asyncio.run(gen.generate(msgs("A", "B"), existing_titles=[]))
    # 모임은 작성 실패 → 링크도 풀린다
    assert pages[0]["content"].endswith("[[B]], 모임, 유령")


def test_prompt_mentions_wiki_link_syntax():
    from app.generator import SYSTEM_PROMPT
    assert "[[제목]]" in SYSTEM_PROMPT and "별칭" in SYSTEM_PROMPT


def test_clean_links():
    from app.generator import clean_links
    src = "[[박건휘]]는 [[ 김민정 ]]과 [[없는사람]], [[김민정|민정]], [[나]] 와 [[모임]]에서"
    out = clean_links(src, ["박건휘", "김민정", "모임", "나"], self_title="나")
    assert out == "[[박건휘]]는 [[김민정]]과 없는사람, [[김민정]], 나 와 [[모임]]에서"
