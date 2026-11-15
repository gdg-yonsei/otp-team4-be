package main

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/joho/godotenv"

	"otp-server/internal/agent"
	"otp-server/internal/config"
	"otp-server/internal/database"
	"otp-server/internal/handler"
	"otp-server/internal/service"
)

// API 문서 전체 정보 (swag가 읽는다). 문서 재생성: go tool swag init -g cmd/server/main.go --parseInternal
//
//	@title			카톡 위키 API
//	@version		1.0
//	@description	카카오톡 단체 채팅 내보내기 파일로 나무위키 스타일 문서를 만드는 API.
//	@description	비밀번호 위키는 POST /wikis/{wikiId}/unlock 으로 받은 세션 쿠키가 있어야 접근할 수 있다 (같은 브라우저에서 Swagger UI로 호출하면 쿠키가 자동으로 실린다).
//	@description	AI 에이전트는 Authorization: Bearer {AGENT_TOKEN} 헤더로 비밀번호 없이 접근한다.
//	@BasePath		/
//
//	@securityDefinitions.apikey	AgentToken
//	@in							header
//	@name						Authorization
//	@description				AI 에이전트 전용. "Bearer {AGENT_TOKEN}" 형식으로 입력.
func main() {
	// Load .env if present. Variables already set in the environment take precedence.
	if err := godotenv.Load(); err != nil && !errors.Is(err, fs.ErrNotExist) {
		slog.Error("load .env", "err", err)
		os.Exit(1)
	}
	cfg := config.Load()

	db, err := database.Open(cfg)
	if err != nil {
		slog.Error("open database", "err", err)
		os.Exit(1)
	}

	wikis := service.NewWikiService(db, cfg.SessionTTL)
	chats := service.NewChatService(db)
	pages := service.NewPageService(db)
	jobs := service.NewGenerationService(db, chats, cfg.JobTimeout)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if cfg.AgentURL == "" || cfg.AgentToken == "" {
		slog.Warn("AGENT_URL / AGENT_TOKEN not set: AI generation requests will fail")
	}
	dispatcher := agent.NewDispatcher(agent.NewClient(cfg.AgentURL, cfg.AgentToken, cfg.AgentTimeout), jobs, wikis, chats, pages)
	if err := dispatcher.Resume(ctx); err != nil {
		slog.Error("resume undispatched jobs", "err", err)
	}

	go func() {
		t := time.NewTicker(time.Hour)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				wikis.PurgeExpiredSessions(ctx)
			}
		}
	}()

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           handler.New(cfg, wikis, chats, pages, jobs, dispatcher).Router(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		slog.Info("listening", "addr", srv.Addr, "db", cfg.DBDriver, "agent", cfg.AgentURL)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("http server", "err", err)
			stop()
		}
	}()

	<-ctx.Done()
	slog.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
}
