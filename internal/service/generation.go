package service

import (
	"context"
	"errors"
	"net/http"
	"time"

	"gorm.io/gorm"

	"otp-server/internal/apperr"
	"otp-server/internal/model"
)

var ErrJobFinished = apperr.New(http.StatusConflict, "JOB_ALREADY_FINISHED", "이미 종료된 작업입니다.")

type GenerationService struct {
	db      *gorm.DB
	chats   *ChatService
	timeout time.Duration // jobs active longer than this are considered dead
}

func NewGenerationService(db *gorm.DB, chats *ChatService, timeout time.Duration) *GenerationService {
	return &GenerationService{db: db, chats: chats, timeout: timeout}
}

var activeStatuses = []model.JobStatus{model.JobPending, model.JobProcessing}

func (s *GenerationService) Create(ctx context.Context, wikiID string, chatIDs []string, mode model.GenerationMode) (*model.GenerationJob, error) {
	if mode == "" {
		mode = model.ModeMerge
	}
	if mode != model.ModeMerge && mode != model.ModeRegenerate {
		return nil, apperr.BadRequest("mode는 MERGE 또는 REGENERATE여야 합니다.")
	}
	chatIDs = dedupe(chatIDs)
	if len(chatIDs) == 0 {
		return nil, apperr.BadRequest("chatIds는 1개 이상이어야 합니다.")
	}
	n, err := s.chats.CountExisting(ctx, wikiID, chatIDs)
	if err != nil {
		return nil, err
	}
	if int(n) != len(chatIDs) {
		return nil, apperr.ErrChatNotFound
	}

	job := &model.GenerationJob{
		ID:      newID("job"),
		WikiID:  wikiID,
		Mode:    mode,
		Status:  model.JobPending,
		ChatIDs: chatIDs,
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// A job the agent never finished must not block the wiki forever.
		now := time.Now().UTC()
		if err := tx.Model(&model.GenerationJob{}).
			Where("wiki_id = ? AND status IN ? AND created_at < ?", wikiID, activeStatuses, now.Add(-s.timeout)).
			Updates(map[string]any{"status": model.JobFailed, "error": "작업 시간 초과", "finished_at": now}).Error; err != nil {
			return err
		}
		// Only one active job per wiki: concurrent AI runs would fight over the same pages.
		var active int64
		if err := tx.Model(&model.GenerationJob{}).
			Where("wiki_id = ? AND status IN ?", wikiID, activeStatuses).
			Count(&active).Error; err != nil {
			return err
		}
		if active > 0 {
			return apperr.ErrGenerationRunning
		}
		return tx.Create(job).Error
	})
	if err != nil {
		return nil, err
	}
	return job, nil
}

func (s *GenerationService) Get(ctx context.Context, wikiID, jobID string) (*model.GenerationJob, error) {
	var job model.GenerationJob
	err := s.db.WithContext(ctx).First(&job, "id = ? AND wiki_id = ?", jobID, wikiID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, apperr.ErrJobNotFound
	}
	return &job, err
}

func (s *GenerationService) GetByID(ctx context.Context, jobID string) (*model.GenerationJob, error) {
	var job model.GenerationJob
	err := s.db.WithContext(ctx).First(&job, "id = ?", jobID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, apperr.ErrJobNotFound
	}
	return &job, err
}

// UndispatchedIDs returns pending jobs that were never sent to the agent.
func (s *GenerationService) UndispatchedIDs(ctx context.Context) ([]string, error) {
	var ids []string
	err := s.db.WithContext(ctx).Model(&model.GenerationJob{}).
		Where("status = ? AND dispatched_at IS NULL", model.JobPending).
		Order("created_at ASC").
		Pluck("id", &ids).Error
	return ids, err
}

func (s *GenerationService) MarkDispatched(ctx context.Context, jobID string) error {
	return s.db.WithContext(ctx).Model(&model.GenerationJob{}).
		Where("id = ?", jobID).
		Update("dispatched_at", time.Now().UTC()).Error
}

// Fail marks an active job FAILED (no-op if it already finished).
func (s *GenerationService) Fail(ctx context.Context, jobID, reason string) error {
	return s.db.WithContext(ctx).Model(&model.GenerationJob{}).
		Where("id = ? AND status IN ?", jobID, activeStatuses).
		Updates(map[string]any{"status": model.JobFailed, "error": reason, "finished_at": time.Now().UTC()}).Error
}

// ReportInput is what the agent sends to update a job.
type ReportInput struct {
	Status         model.JobStatus
	CreatedPageIDs []string // nil = unchanged
	UpdatedPageIDs []string // nil = unchanged
	Error          string
}

// Report applies a status update from the agent.
// Allowed transitions: PENDING -> PROCESSING -> COMPLETED | FAILED.
func (s *GenerationService) Report(ctx context.Context, wikiID, jobID string, in ReportInput) (*model.GenerationJob, error) {
	switch in.Status {
	case model.JobProcessing, model.JobCompleted, model.JobFailed:
	default:
		return nil, apperr.BadRequest("status는 PROCESSING, COMPLETED, FAILED 중 하나여야 합니다.")
	}

	var job *model.GenerationJob
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var j model.GenerationJob
		if err := tx.First(&j, "id = ? AND wiki_id = ?", jobID, wikiID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return apperr.ErrJobNotFound
			}
			return err
		}
		if j.Status == model.JobCompleted || j.Status == model.JobFailed {
			return ErrJobFinished
		}

		now := time.Now().UTC()
		j.Status = in.Status
		if j.StartedAt == nil {
			j.StartedAt = &now
		}
		if j.DispatchedAt == nil {
			j.DispatchedAt = &now
		}
		if in.CreatedPageIDs != nil {
			j.CreatedPageIDs = dedupe(in.CreatedPageIDs)
		}
		if in.UpdatedPageIDs != nil {
			j.UpdatedPageIDs = dedupe(in.UpdatedPageIDs)
		}
		if in.Status != model.JobProcessing {
			j.FinishedAt = &now
		}
		if in.Status == model.JobFailed {
			j.Error = in.Error
		}
		job = &j
		return tx.Select("status", "started_at", "dispatched_at", "finished_at",
			"created_page_ids", "updated_page_ids", "error").Save(&j).Error
	})
	return job, err
}

func dedupe(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, v := range in {
		if v != "" && !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}
