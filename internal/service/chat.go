package service

import (
	"context"
	"errors"
	"path/filepath"
	"time"

	"gorm.io/gorm"

	"otp-server/internal/apperr"
	"otp-server/internal/kakao"
	"otp-server/internal/model"
)

type ChatService struct {
	db *gorm.DB
}

func NewChatService(db *gorm.DB) *ChatService { return &ChatService{db: db} }

func (s *ChatService) Upload(ctx context.Context, wikiID, fileName string, data []byte) (*model.Chat, error) {
	msgs, err := kakao.ParseFile(fileName, data)
	if err != nil || len(msgs) == 0 {
		return nil, apperr.ErrInvalidChatFile
	}
	chat := &model.Chat{
		ID:           newID("chat"),
		WikiID:       wikiID,
		FileName:     filepath.Base(fileName),
		MessageCount: len(msgs),
		Content:      string(data),
		UploadedAt:   time.Now().UTC(),
	}
	if err := s.db.WithContext(ctx).Create(chat).Error; err != nil {
		return nil, err
	}
	return chat, nil
}

// List returns chat metadata (without content), newest first.
func (s *ChatService) List(ctx context.Context, wikiID string) ([]model.Chat, error) {
	var chats []model.Chat
	err := s.db.WithContext(ctx).
		Select("id", "wiki_id", "file_name", "message_count", "uploaded_at").
		Where("wiki_id = ?", wikiID).
		Order("uploaded_at DESC").
		Find(&chats).Error
	return chats, err
}

// GetMany loads the given chats (including content) in the requested order.
func (s *ChatService) GetMany(ctx context.Context, wikiID string, chatIDs []string) ([]model.Chat, error) {
	out := make([]model.Chat, 0, len(chatIDs))
	for _, id := range chatIDs {
		var chat model.Chat
		err := s.db.WithContext(ctx).First(&chat, "id = ? AND wiki_id = ?", id, wikiID).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, apperr.ErrChatNotFound
		} else if err != nil {
			return nil, err
		}
		out = append(out, chat)
	}
	return out, nil
}

// CountExisting returns how many of ids belong to the wiki.
func (s *ChatService) CountExisting(ctx context.Context, wikiID string, ids []string) (int64, error) {
	var n int64
	err := s.db.WithContext(ctx).Model(&model.Chat{}).
		Where("wiki_id = ? AND id IN ?", wikiID, ids).Count(&n).Error
	return n, err
}
