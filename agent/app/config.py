"""환경 변수 설정. 프로젝트 루트의 .env(.env.prod)를 Go 서버와 같이 쓴다."""

from __future__ import annotations

import os
from dataclasses import dataclass


def _int(key: str, default: int) -> int:
    v = os.environ.get(key, "")
    return int(v) if v.strip() else default


@dataclass(frozen=True)
class Settings:
    agent_token: str
    openai_api_key: str
    api_url: str = "http://localhost:8080"
    openai_model: str = "gpt-6.2-astra"
    # 한 번에 모델에 넣는 대화 크기(문자 수). 넘으면 여러 조각으로 나눠 호출한다.
    max_input_chars: int = 120_000
    max_output_tokens: int = 24_000
    # 비용 폭주 방지: 조각이 이보다 많으면 작업을 FAILED 처리한다.
    max_chunks: int = 8
    max_concurrent_jobs: int = 1
    # 한 작업에서 쓸 문서 수 상한 (문서마다 모델을 한 번씩 호출한다)
    max_pages: int = 40
    # 문서 작성 동시 호출 수. 높을수록 빠르지만 OpenAI 분당 토큰 한도(TPM)에 걸리기 쉽다.
    write_concurrency: int = 20
    # 모델 추론량: minimal | low | medium | high. 빈 값이면 모델 기본값.
    plan_reasoning_effort: str = "medium"
    write_reasoning_effort: str = "low"
    # PERSON 문서를 만들지 않을 발신자 (봇 등). 쉼표 구분.
    excluded_senders: frozenset[str] = frozenset({"채팅봇"})

    @classmethod
    def from_env(cls) -> "Settings":
        token = os.environ.get("AGENT_TOKEN", "")
        key = os.environ.get("OPENAI_API_KEY", "")
        if not token or token == "change-me":
            raise SystemExit("AGENT_TOKEN이 없거나 기본값(change-me)입니다.")
        if not key:
            raise SystemExit("OPENAI_API_KEY가 필요합니다.")
        return cls(
            agent_token=token,
            openai_api_key=key,
            api_url=os.environ.get("API_URL", cls.api_url),
            openai_model=os.environ.get("OPENAI_MODEL", cls.openai_model),
            max_input_chars=_int("MAX_INPUT_CHARS", cls.max_input_chars),
            max_output_tokens=_int("MAX_OUTPUT_TOKENS", cls.max_output_tokens),
            max_chunks=_int("MAX_CHUNKS", cls.max_chunks),
            max_concurrent_jobs=_int("MAX_CONCURRENT_JOBS", cls.max_concurrent_jobs),
            max_pages=_int("MAX_PAGES", cls.max_pages),
            write_concurrency=_int("WRITE_CONCURRENCY", cls.write_concurrency),
            plan_reasoning_effort=os.environ.get("PLAN_REASONING_EFFORT", cls.plan_reasoning_effort),
            write_reasoning_effort=os.environ.get("WRITE_REASONING_EFFORT", cls.write_reasoning_effort),
            excluded_senders=(
                frozenset(x.strip() for x in os.environ["EXCLUDED_SENDERS"].split(",") if x.strip())
                if "EXCLUDED_SENDERS" in os.environ else cls.excluded_senders
            ),
        )
