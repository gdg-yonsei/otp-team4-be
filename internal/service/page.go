package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"

	"otp-server/internal/apperr"
	"otp-server/internal/model"
)

const maxContentBytes = 1 << 20 // 1 MiB per page

func newID(prefix string) string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return prefix + "_" + hex.EncodeToString(b)
}

type PageService struct {
	db *gorm.DB
}

func NewPageService(db *gorm.DB) *PageService { return &PageService{db: db} }

type PageFilter struct {
	Category model.PageCategory
	Query    string
}

func (s *PageService) List(ctx context.Context, wikiID string, f PageFilter) ([]model.Page, error) {
	q := s.db.WithContext(ctx).
		Select("id", "title", "category", "version", "updated_at").
		Where("wiki_id = ?", wikiID)
	if f.Category != "" {
		q = q.Where("category = ?", f.Category)
	}
	if f.Query != "" {
		q = q.Where(`title LIKE ? ESCAPE '\'`, "%"+escapeLike(f.Query)+"%")
	}
	var pages []model.Page
	err := q.Order("title ASC").Find(&pages).Error
	return pages, err
}

func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

func (s *PageService) Get(ctx context.Context, wikiID, pageID string) (*model.Page, error) {
	var p model.Page
	err := s.db.WithContext(ctx).First(&p, "id = ? AND wiki_id = ?", pageID, wikiID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, apperr.ErrPageNotFound
	}
	return &p, err
}

func (s *PageService) GetByTitle(ctx context.Context, wikiID, title string) (*model.Page, error) {
	var p model.Page
	err := s.db.WithContext(ctx).First(&p, "title = ? AND wiki_id = ?", strings.TrimSpace(title), wikiID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, apperr.ErrPageNotFound
	}
	return &p, err
}

func validateTitle(title string) (string, error) {
	title = strings.TrimSpace(title)
	if title == "" || utf8.RuneCountInString(title) > 200 {
		return "", apperr.BadRequest("문서 제목은 1~200자여야 합니다.")
	}
	return title, nil
}

func validateCategory(c model.PageCategory) error {
	if !c.Valid() {
		return apperr.BadRequest("알 수 없는 카테고리입니다: " + string(c))
	}
	return nil
}

func validateContent(c string) error {
	if len(c) > maxContentBytes {
		return apperr.BadRequest("문서 내용이 너무 깁니다.")
	}
	return nil
}

type CreatePageInput struct {
	Title    string
	Category model.PageCategory
	Content  string
}

func (s *PageService) Create(ctx context.Context, wikiID string, in CreatePageInput, by model.Editor) (*model.Page, error) {
	title, err := validateTitle(in.Title)
	if err != nil {
		return nil, err
	}
	if err := validateCategory(in.Category); err != nil {
		return nil, err
	}
	if err := validateContent(in.Content); err != nil {
		return nil, err
	}
	p := &model.Page{
		ID:         newID("page"),
		WikiID:     wikiID,
		Title:      title,
		Category:   in.Category,
		Content:    in.Content,
		Version:    1,
		CreatedBy:  by,
		LastEditor: by,
	}
	if err := s.db.WithContext(ctx).Create(p).Error; err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			return nil, apperr.ErrTitleDuplicate
		}
		return nil, err
	}
	return p, nil
}

type UpdatePageInput struct {
	Title    *string
	Category *model.PageCategory
	Content  *string
	Version  int
}

// Update applies a partial update guarded by optimistic locking: it only
// succeeds when in.Version equals the stored version.
func (s *PageService) Update(ctx context.Context, wikiID, pageID string, in UpdatePageInput, by model.Editor) (*model.Page, error) {
	if in.Version <= 0 {
		return nil, apperr.BadRequest("version은 필수입니다.")
	}
	updates := map[string]any{
		"version":     gorm.Expr("version + 1"),
		"last_editor": by,
		"updated_at":  time.Now().UTC(),
	}
	if in.Title != nil {
		title, err := validateTitle(*in.Title)
		if err != nil {
			return nil, err
		}
		updates["title"] = title
	}
	if in.Category != nil {
		if err := validateCategory(*in.Category); err != nil {
			return nil, err
		}
		updates["category"] = *in.Category
	}
	if in.Content != nil {
		if err := validateContent(*in.Content); err != nil {
			return nil, err
		}
		updates["content"] = *in.Content
	}
	if len(updates) == 3 {
		return nil, apperr.BadRequest("변경할 항목이 없습니다.")
	}

	res := s.db.WithContext(ctx).Model(&model.Page{}).
		Where("id = ? AND wiki_id = ? AND version = ?", pageID, wikiID, in.Version).
		Updates(updates)
	if res.Error != nil {
		if errors.Is(res.Error, gorm.ErrDuplicatedKey) {
			return nil, apperr.ErrTitleDuplicate
		}
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		// Either the page does not exist or the version is stale.
		if _, err := s.Get(ctx, wikiID, pageID); err != nil {
			return nil, err
		}
		return nil, apperr.ErrVersionConflict
	}
	return s.Get(ctx, wikiID, pageID)
}

func (s *PageService) Delete(ctx context.Context, wikiID, pageID string) error {
	res := s.db.WithContext(ctx).Where("id = ? AND wiki_id = ?", pageID, wikiID).Delete(&model.Page{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return apperr.ErrPageNotFound
	}
	return nil
}

// DeleteAIOnly removes pages that were created by the AI and never edited by
// a person. Used by REGENERATE mode so human edits are never lost.
func (s *PageService) DeleteAIOnly(ctx context.Context, wikiID string) (int64, error) {
	res := s.db.WithContext(ctx).
		Where("wiki_id = ? AND created_by = ? AND last_editor = ?", wikiID, model.EditorAI, model.EditorAI).
		Delete(&model.Page{})
	return res.RowsAffected, res.Error
}
