package middleware

import (
	"crypto/subtle"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"

	"otp-server/internal/apperr"
	"otp-server/internal/model"
	"otp-server/internal/service"
)

const wikiKey = "wiki"

// SessionCookieName is per wiki so one browser can hold sessions for several wikis.
func SessionCookieName(wikiID string) string { return "wiki_session_" + wikiID }

func Abort(c *gin.Context, err error) {
	e := apperr.From(err)
	if e.Status >= 500 {
		_ = c.Error(err)
	}
	c.AbortWithStatusJSON(e.Status, e)
}

// LoadWiki resolves :wikiId and stores the wiki in the context (404 if missing).
func LoadWiki(wikis *service.WikiService) gin.HandlerFunc {
	return func(c *gin.Context) {
		w, err := wikis.Get(c.Request.Context(), c.Param("wikiId"))
		if err != nil {
			Abort(c, err)
			return
		}
		c.Set(wikiKey, w)
		c.Next()
	}
}

const editorKey = "editor"

// IsAgent reports whether the request carries the AI agent's bearer token.
func IsAgent(c *gin.Context, agentToken string) bool {
	if agentToken == "" {
		return false
	}
	got, ok := strings.CutPrefix(c.GetHeader("Authorization"), "Bearer ")
	return ok && subtle.ConstantTimeCompare([]byte(got), []byte(agentToken)) == 1
}

// RequireAccess lets through the AI agent (bearer token) and, for
// password-protected wikis, only requests with a valid session cookie.
func RequireAccess(wikis *service.WikiService, agentToken string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if IsAgent(c, agentToken) {
			c.Set(editorKey, model.EditorAI)
			c.Next()
			return
		}
		c.Set(editorKey, model.EditorUser)
		w := Wiki(c)
		if w.HasPassword() {
			token, _ := c.Cookie(SessionCookieName(w.ID))
			if !wikis.ValidSession(c.Request.Context(), w.ID, token) {
				Abort(c, apperr.ErrWikiLocked)
				return
			}
		}
		c.Next()
	}
}

// RequireAgent allows only the AI agent.
func RequireAgent(agentToken string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !IsAgent(c, agentToken) {
			Abort(c, apperr.New(http.StatusForbidden, "AGENT_ONLY", "AI 에이전트만 호출할 수 있습니다."))
			return
		}
		c.Next()
	}
}

func Wiki(c *gin.Context) *model.Wiki {
	return c.MustGet(wikiKey).(*model.Wiki)
}

// Editor is who is making the request: AI (agent token) or USER.
func Editor(c *gin.Context) model.Editor {
	if e, ok := c.Get(editorKey); ok {
		return e.(model.Editor)
	}
	return model.EditorUser
}

// RateLimit applies a token bucket per key (e.g. client IP, or IP+wiki).
func RateLimit(r rate.Limit, burst int, key func(*gin.Context) string) gin.HandlerFunc {
	type entry struct {
		lim  *rate.Limiter
		seen time.Time
	}
	var (
		mu      sync.Mutex
		entries = map[string]*entry{}
		lastGC  = time.Now()
	)
	return func(c *gin.Context) {
		k := key(c)
		now := time.Now()
		mu.Lock()
		if now.Sub(lastGC) > time.Minute {
			for k, e := range entries {
				if now.Sub(e.seen) > 10*time.Minute {
					delete(entries, k)
				}
			}
			lastGC = now
		}
		e, ok := entries[k]
		if !ok {
			e = &entry{lim: rate.NewLimiter(r, burst)}
			entries[k] = e
		}
		e.seen = now
		allowed := e.lim.Allow()
		mu.Unlock()

		if !allowed {
			c.Header("Retry-After", "60")
			Abort(c, apperr.ErrTooManyRequests)
			return
		}
		c.Next()
	}
}

func ByIP(c *gin.Context) string { return c.ClientIP() }

func ByIPAndWiki(c *gin.Context) string { return c.ClientIP() + "|" + c.Param("wikiId") }

// CORS allows credentialed requests from the configured origins.
func CORS(origins []string) gin.HandlerFunc {
	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		if origin != "" && slices.Contains(origins, origin) {
			h := c.Writer.Header()
			h.Set("Access-Control-Allow-Origin", origin)
			h.Set("Access-Control-Allow-Credentials", "true")
			h.Set("Access-Control-Allow-Methods", "GET,POST,PATCH,DELETE,OPTIONS")
			h.Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
			h.Add("Vary", "Origin")
			if c.Request.Method == http.MethodOptions {
				c.AbortWithStatus(http.StatusNoContent)
				return
			}
		}
		c.Next()
	}
}
