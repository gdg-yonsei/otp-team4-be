package handler_test

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"otp-server/internal/agent"
	"otp-server/internal/config"
	"otp-server/internal/database"
	"otp-server/internal/handler"
	"otp-server/internal/service"
)

type client struct {
	t     *testing.T
	srv   *httptest.Server
	hc    *http.Client
	token string // agent bearer token
}

func (c *client) do(method, path string, body any, out any) int {
	c.t.Helper()
	var r *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	} else {
		r = bytes.NewReader(nil)
	}
	req, _ := http.NewRequest(method, c.srv.URL+path, r)
	req.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer resp.Body.Close()
	if out != nil {
		_ = json.NewDecoder(resp.Body).Decode(out)
	}
	return resp.StatusCode
}

func (c *client) upload(path, name, content string, out any) int {
	c.t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("file", name)
	fw.Write([]byte(content))
	mw.Close()
	req, _ := http.NewRequest(http.MethodPost, c.srv.URL+path, &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := c.hc.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer resp.Body.Close()
	if out != nil {
		_ = json.NewDecoder(resp.Body).Decode(out)
	}
	return resp.StatusCode
}

func newClient(t *testing.T, srv *httptest.Server) *client {
	jar, _ := cookiejar.New(nil)
	return &client{t: t, srv: srv, hc: &http.Client{Jar: jar}}
}

func TestEndToEnd(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := config.Load()
	cfg.DBDriver = "sqlite"
	cfg.DatabaseURL = t.TempDir() + "/test.db"
	cfg.MaxUploadBytes = 1 << 20
	cfg.AgentToken = "agent-secret"

	db, err := database.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	wikis := service.NewWikiService(db, time.Hour)
	chats := service.NewChatService(db)
	pages := service.NewPageService(db)
	jobs := service.NewGenerationService(db, chats, time.Hour)

	// Fake AI agent: on dispatch it creates (or updates) one page through the
	// public API with its bearer token, then reports the job as completed.
	var srv *httptest.Server
	agentSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer agent-secret" {
			t.Errorf("dispatch without agent token")
		}
		var req agent.Request
		json.NewDecoder(r.Body).Decode(&req)
		if len(req.Chats) != 1 || !strings.Contains(req.Chats[0].Content, "부산 가자") {
			t.Errorf("unexpected dispatch payload: %+v", req)
		}
		w.WriteHeader(http.StatusAccepted)
		go func() {
			ag := newClient(t, srv)
			ag.token = "agent-secret"
			base := "/wikis/" + req.WikiID
			ag.do("PATCH", base+"/generations/"+req.JobID, map[string]any{"status": "PROCESSING"}, nil)
			var list struct{ Pages []map[string]any }
			ag.do("GET", base+"/pages?q=부산", nil, &list)
			report := map[string]any{"status": "COMPLETED"}
			if len(list.Pages) == 0 {
				var p map[string]any
				ag.do("POST", base+"/pages", map[string]any{"title": "2026년 부산 여행", "category": "EVENT", "content": "## 개요"}, &p)
				report["createdPageIds"] = []string{p["id"].(string)}
			} else {
				id := list.Pages[0]["id"].(string)
				var p map[string]any
				ag.do("GET", base+"/pages/"+id, nil, &p)
				ag.do("PATCH", base+"/pages/"+id, map[string]any{"content": "## 개요\n추가", "version": p["version"]}, nil)
				report["updatedPageIds"] = []string{id}
			}
			ag.do("PATCH", base+"/generations/"+req.JobID, report, nil)
		}()
	}))
	defer agentSrv.Close()

	dispatcher := agent.NewDispatcher(agent.NewClient(agentSrv.URL, cfg.AgentToken, 5*time.Second), jobs, wikis, chats, pages)
	srv = httptest.NewServer(handler.New(cfg, wikis, chats, pages, jobs, dispatcher).Router())
	defer srv.Close()

	owner := newClient(t, srv)
	stranger := newClient(t, srv)

	// create (owner gets a session cookie automatically)
	var wiki map[string]any
	if s := owner.do("POST", "/wikis", map[string]any{"name": "우리들의 위키", "password": "1234"}, &wiki); s != 201 {
		t.Fatalf("create wiki: %d", s)
	}
	id := wiki["id"].(string)
	if wiki["hasPassword"] != true || !strings.HasSuffix(wiki["url"].(string), "/w/"+id) {
		t.Fatalf("bad wiki response: %v", wiki)
	}
	base := "/wikis/" + id

	// access control
	var e map[string]any
	if s := stranger.do("GET", base, nil, &e); s != 401 || e["code"] != "WIKI_LOCKED" {
		t.Fatalf("stranger get: %d %v", s, e)
	}
	if s := stranger.do("POST", base+"/unlock", map[string]any{"password": "nope"}, &e); s != 403 || e["code"] != "WIKI_PASSWORD_INVALID" {
		t.Fatalf("wrong password: %d %v", s, e)
	}
	if s := stranger.do("POST", base+"/unlock", map[string]any{"password": "1234"}, nil); s != 204 {
		t.Fatalf("unlock: %d", s)
	}
	if s := stranger.do("GET", base, nil, nil); s != 200 {
		t.Fatalf("after unlock: %d", s)
	}
	if s := owner.do("GET", "/wikis/00000000-0000-0000-0000-000000000000", nil, nil); s != 404 {
		t.Fatalf("missing wiki: %d", s)
	}

	// password change revokes other sessions but keeps the requester in
	if s := owner.do("PATCH", base, map[string]any{"name": "부산 여행 위키", "password": "5678"}, &wiki); s != 200 || wiki["name"] != "부산 여행 위키" {
		t.Fatalf("patch wiki: %d %v", s, wiki)
	}
	if s := stranger.do("GET", base, nil, nil); s != 401 {
		t.Fatalf("stranger session should be revoked: %d", s)
	}
	if s := owner.do("GET", base, nil, nil); s != 200 {
		t.Fatalf("owner should stay signed in: %d", s)
	}

	// chats
	chatTxt := "--------------- 2026년 11월 5일 목요일 ---------------\n[김철수] [오후 1:23] 부산 가자\n[이영희] [오후 1:24] 12월 20일 콜\n"
	var chat map[string]any
	if s := owner.upload(base+"/chats", "KakaoTalk_Chat.txt", chatTxt, &chat); s != 201 || chat["messageCount"].(float64) != 2 {
		t.Fatalf("upload: %d %v", s, chat)
	}
	if s := owner.upload(base+"/chats", "x.txt", "그냥 텍스트", &e); s != 400 || e["code"] != "INVALID_CHAT_FILE" {
		t.Fatalf("invalid upload: %d %v", s, e)
	}
	if s := owner.upload(base+"/chats", "big.txt", strings.Repeat("a", 3<<20), &e); s != 413 {
		t.Fatalf("big upload: %d %v", s, e)
	}
	var chatList struct{ Chats []map[string]any }
	if owner.do("GET", base+"/chats", nil, &chatList); len(chatList.Chats) != 1 || chatList.Chats[0]["fileName"] != "KakaoTalk_Chat.txt" {
		t.Fatalf("list chats: %v", chatList)
	}

	// generation
	runJob := func(mode string) map[string]any {
		var j map[string]any
		if s := owner.do("POST", base+"/generations", map[string]any{"chatIds": []string{chat["id"].(string)}, "mode": mode}, &j); s != 202 {
			t.Fatalf("create job: %d %v", s, j)
		}
		for i := 0; i < 100; i++ {
			owner.do("GET", base+"/generations/"+j["jobId"].(string), nil, &j)
			if j["status"] == "COMPLETED" || j["status"] == "FAILED" {
				return j
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatalf("job did not finish: %v", j)
		return nil
	}
	j := runJob("MERGE")
	if j["status"] != "COMPLETED" || j["createdPages"].(float64) != 1 {
		t.Fatalf("first job: %v", j)
	}
	j = runJob("MERGE")
	if j["createdPages"].(float64) != 0 || j["updatedPages"].(float64) != 1 {
		t.Fatalf("second job: %v", j)
	}
	if s := owner.do("POST", base+"/generations", map[string]any{"chatIds": []string{"chat_nope"}}, &e); s != 404 {
		t.Fatalf("missing chat: %d", s)
	}
	// only the agent may report job status, and finished jobs are immutable
	if s := owner.do("PATCH", base+"/generations/"+j["jobId"].(string), map[string]any{"status": "FAILED"}, &e); s != 403 {
		t.Fatalf("user report: %d %v", s, e)
	}
	ag := newClient(t, srv)
	ag.token = "agent-secret"
	if s := ag.do("PATCH", base+"/generations/"+j["jobId"].(string), map[string]any{"status": "FAILED"}, &e); s != 409 {
		t.Fatalf("report finished job: %d %v", s, e)
	}

	// pages
	var created map[string]any
	if s := owner.do("POST", base+"/pages", map[string]any{"title": "김철수", "category": "PERSON", "content": "## 개요"}, &created); s != 201 {
		t.Fatalf("create page: %d %v", s, created)
	}
	if s := owner.do("POST", base+"/pages", map[string]any{"title": "김철수", "category": "PERSON"}, &e); s != 409 {
		t.Fatalf("duplicate title: %d", s)
	}
	if s := owner.do("POST", base+"/pages", map[string]any{"title": "x", "category": "NOPE"}, &e); s != 400 {
		t.Fatalf("bad category: %d", s)
	}
	pid := created["id"].(string)
	var page map[string]any
	owner.do("GET", base+"/pages/"+pid, nil, &page)
	if page["version"].(float64) != 1 {
		t.Fatalf("get page: %v", page)
	}
	var upd map[string]any
	if s := owner.do("PATCH", base+"/pages/"+pid, map[string]any{"content": "## 개요\n수정", "version": 1}, &upd); s != 200 || upd["version"].(float64) != 2 {
		t.Fatalf("update: %d %v", s, upd)
	}
	if s := owner.do("PATCH", base+"/pages/"+pid, map[string]any{"content": "늦은 수정", "version": 1}, &e); s != 409 || e["code"] != "PAGE_VERSION_CONFLICT" {
		t.Fatalf("conflict: %d %v", s, e)
	}
	var list struct{ Pages []map[string]any }
	owner.do("GET", base+"/pages", nil, &list)
	if len(list.Pages) != 2 {
		t.Fatalf("list pages: %v", list)
	}
	owner.do("GET", base+"/pages?category=PERSON", nil, &list)
	if len(list.Pages) != 1 {
		t.Fatalf("filter pages: %v", list)
	}

	// REGENERATE removes AI-only pages but keeps human pages
	j = runJob("REGENERATE")
	if j["status"] != "COMPLETED" || j["createdPages"].(float64) != 1 {
		t.Fatalf("regenerate: %v", j)
	}
	owner.do("GET", base+"/pages", nil, &list)
	if len(list.Pages) != 2 {
		t.Fatalf("after regenerate: %v", list)
	}

	if s := owner.do("DELETE", base+"/pages/"+pid, nil, nil); s != 204 {
		t.Fatalf("delete: %d", s)
	}
	if s := owner.do("GET", base+"/pages/"+pid, nil, &e); s != 404 || e["code"] != "PAGE_NOT_FOUND" {
		t.Fatalf("deleted page: %d %v", s, e)
	}
}
