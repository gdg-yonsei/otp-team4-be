# syntax=docker/dockerfile:1
FROM python:3.12-slim

# uv: uv.lock에 고정된 버전 그대로 설치 (pip보다 빠르고 재현 가능)
COPY --from=ghcr.io/astral-sh/uv:0.12.0 /uv /usr/local/bin/uv

WORKDIR /app
ENV UV_COMPILE_BYTECODE=1 \
    UV_LINK_MODE=copy \
    PYTHONUNBUFFERED=1 \
    PATH="/app/.venv/bin:$PATH"

# 의존성 레이어 먼저 (소스만 바뀌면 캐시 재사용)
COPY pyproject.toml uv.lock ./
RUN uv sync --frozen --no-dev --no-install-project

COPY app ./app

USER nobody
EXPOSE 8000
CMD ["uvicorn", "app.main:default_app", "--factory", "--host", "0.0.0.0", "--port", "8000"]
