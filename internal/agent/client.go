// Package agent sends generation requests to the external AI agent server.
//
// This server does not run any AI itself. When a generation job is created it
// POSTs the job (with the chat logs) to AGENT_URL. The agent then:
//   - reads/writes pages through the regular page API using
//     "Authorization: Bearer <AGENT_TOKEN>" (which bypasses wiki passwords), and
//   - reports progress via PATCH /wikis/{wikiId}/generations/{jobId}.
package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"otp-server/internal/model"
	"otp-server/internal/service"
)

// Request is the JSON body sent to the agent server.
type Request struct {
	JobID    string               `json:"jobId"`
	WikiID   string               `json:"wikiId"`
	WikiName string               `json:"wikiName"`
	Mode     model.GenerationMode `json:"mode"`
	Chats    []Chat               `json:"chats"`
}

type Chat struct {
	ID           string `json:"id"`
	FileName     string `json:"fileName"`
	MessageCount int    `json:"messageCount"`
	Content      string `json:"content"` // original KakaoTalk export text
}

type Client struct {
	url   string
	token string
	http  *http.Client
}

func NewClient(url, token string, timeout time.Duration) *Client {
	return &Client{url: url, token: token, http: &http.Client{Timeout: timeout}}
}

// Send POSTs the request, retrying network errors and 5xx/429 responses.
func (c *Client) Send(ctx context.Context, req Request) error {
	if c.url == "" {
		return errors.New("AGENT_URL이 설정되지 않았습니다")
	}
	body, err := json.Marshal(req)
	if err != nil {
		return err
	}

	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(attempt*attempt) * time.Second):
			}
		}
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(body))
		if err != nil {
			return err
		}
		httpReq.Header.Set("Content-Type", "application/json")
		if c.token != "" {
			httpReq.Header.Set("Authorization", "Bearer "+c.token)
		}
		resp, err := c.http.Do(httpReq)
		if err != nil {
			lastErr = err
			continue
		}
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		resp.Body.Close()
		if resp.StatusCode < 300 {
			return nil
		}
		lastErr = fmt.Errorf("agent responded %d: %s", resp.StatusCode, bytes.TrimSpace(msg))
		if resp.StatusCode < 500 && resp.StatusCode != http.StatusTooManyRequests {
			return lastErr // client errors are not retried
		}
	}
	return lastErr
}

// Dispatcher prepares a job and hands it to the agent in the background.
type Dispatcher struct {
	client *Client
	jobs   *service.GenerationService
	wikis  *service.WikiService
	chats  *service.ChatService
	pages  *service.PageService
}

func NewDispatcher(client *Client, jobs *service.GenerationService, wikis *service.WikiService,
	chats *service.ChatService, pages *service.PageService) *Dispatcher {
	return &Dispatcher{client: client, jobs: jobs, wikis: wikis, chats: chats, pages: pages}
}

// Dispatch sends the job asynchronously. Failures mark the job FAILED.
func (d *Dispatcher) Dispatch(jobID string) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		if err := d.dispatch(ctx, jobID); err != nil {
			slog.Error("dispatch generation job", "job", jobID, "err", err)
			if ferr := d.jobs.Fail(context.Background(), jobID, "에이전트 요청 실패: "+err.Error()); ferr != nil {
				slog.Error("mark job failed", "job", jobID, "err", ferr)
			}
		}
	}()
}

// Resume re-sends jobs that were created but never handed to the agent
// (e.g. the server stopped in between).
func (d *Dispatcher) Resume(ctx context.Context) error {
	ids, err := d.jobs.UndispatchedIDs(ctx)
	if err != nil {
		return err
	}
	for _, id := range ids {
		d.Dispatch(id)
	}
	return nil
}

func (d *Dispatcher) dispatch(ctx context.Context, jobID string) error {
	job, err := d.jobs.GetByID(ctx, jobID)
	if err != nil {
		return err
	}
	wiki, err := d.wikis.Get(ctx, job.WikiID)
	if err != nil {
		return err
	}
	chats, err := d.chats.GetMany(ctx, job.WikiID, job.ChatIDs)
	if err != nil {
		return err
	}

	if job.Mode == model.ModeRegenerate {
		// Remove pages the AI created that nobody has edited since; human edits are kept.
		n, err := d.pages.DeleteAIOnly(ctx, job.WikiID)
		if err != nil {
			return err
		}
		slog.Info("regenerate: removed AI-only pages", "job", job.ID, "count", n)
	}

	req := Request{JobID: job.ID, WikiID: wiki.ID, WikiName: wiki.Name, Mode: job.Mode}
	for _, c := range chats {
		req.Chats = append(req.Chats, Chat{ID: c.ID, FileName: c.FileName, MessageCount: c.MessageCount, Content: c.Content})
	}
	if err := d.client.Send(ctx, req); err != nil {
		return err
	}
	slog.Info("generation job dispatched", "job", job.ID, "wiki", job.WikiID)
	return d.jobs.MarkDispatched(ctx, job.ID)
}
