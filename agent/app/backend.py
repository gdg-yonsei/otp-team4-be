"""Go API 서버(otp-server) 호출. 모든 요청에 Authorization: Bearer AGENT_TOKEN을 붙인다."""

from __future__ import annotations

import httpx


class Conflict(Exception):
    """409 응답 (제목 중복, 이미 끝난 작업 등)."""


class Backend:
    def __init__(self, base_url: str, token: str, timeout: float = 30.0):
        self.http = httpx.AsyncClient(
            base_url=base_url.rstrip("/"),
            headers={"Authorization": f"Bearer {token}"},
            timeout=timeout,
        )

    async def aclose(self) -> None:
        await self.http.aclose()

    async def _send(self, method: str, path: str, **kw) -> dict:
        resp = await self.http.request(method, path, **kw)
        if resp.status_code == 409:
            raise Conflict(resp.text)
        resp.raise_for_status()
        return resp.json()

    async def report(
        self,
        wiki_id: str,
        job_id: str,
        status: str,
        created_page_ids: list[str] | None = None,
        error: str | None = None,
    ) -> None:
        body: dict = {"status": status}
        if created_page_ids is not None:
            body["createdPageIds"] = created_page_ids
        if error:
            body["error"] = error
        await self._send("PATCH", f"/wikis/{wiki_id}/generations/{job_id}", json=body)

    async def list_titles(self, wiki_id: str) -> list[str]:
        data = await self._send("GET", f"/wikis/{wiki_id}/pages")
        return [p["title"] for p in data["pages"]]

    async def create_page(self, wiki_id: str, page: dict) -> str:
        """문서를 만들고 id를 돌려준다. 같은 제목이 이미 있으면 Conflict."""
        data = await self._send("POST", f"/wikis/{wiki_id}/pages", json=page)
        return data["id"]
