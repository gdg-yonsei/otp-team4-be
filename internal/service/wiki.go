package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"otp-server/internal/apperr"
	"otp-server/internal/model"
)

type WikiService struct {
	db         *gorm.DB
	sessionTTL time.Duration
}

func NewWikiService(db *gorm.DB, sessionTTL time.Duration) *WikiService {
	return &WikiService{db: db, sessionTTL: sessionTTL}
}

func validateWikiName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > 100 {
		return "", apperr.BadRequest("위키 이름은 1~100자여야 합니다.")
	}
	return name, nil
}

func hashPassword(pw string) (string, error) {
	if utf8.RuneCountInString(pw) < 4 || len(pw) > 72 {
		return "", apperr.BadRequest("비밀번호는 4자 이상, 72바이트 이하여야 합니다.")
	}
	h, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
	return string(h), err
}

func (s *WikiService) Create(ctx context.Context, name string, password *string) (*model.Wiki, error) {
	name, err := validateWikiName(name)
	if err != nil {
		return nil, err
	}
	w := &model.Wiki{ID: uuid.NewString(), Name: name}
	if password != nil && *password != "" {
		if w.PasswordHash, err = hashPassword(*password); err != nil {
			return nil, err
		}
	}
	if err := s.db.WithContext(ctx).Create(w).Error; err != nil {
		return nil, err
	}
	return w, nil
}

func (s *WikiService) Get(ctx context.Context, id string) (*model.Wiki, error) {
	if _, err := uuid.Parse(id); err != nil {
		return nil, apperr.ErrWikiNotFound
	}
	var w model.Wiki
	if err := s.db.WithContext(ctx).First(&w, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, apperr.ErrWikiNotFound
		}
		return nil, err
	}
	return &w, nil
}

// Delete removes the wiki and everything in it (pages, chats, generation jobs,
// sessions) in one transaction. A job still running at the agent will fail on
// its next API call with 404.
func (s *WikiService) Delete(ctx context.Context, id string) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, m := range []any{&model.Page{}, &model.Chat{}, &model.GenerationJob{}, &model.WikiSession{}} {
			if err := tx.Where("wiki_id = ?", id).Delete(m).Error; err != nil {
				return err
			}
		}
		res := tx.Where("id = ?", id).Delete(&model.Wiki{})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return apperr.ErrWikiNotFound
		}
		return nil
	})
}

type UpdateWikiInput struct {
	Name *string
	// Password: nil = unchanged, "" = remove password, otherwise set new password.
	Password *string
}

// Update changes wiki settings. It reports whether the password changed, in
// which case all existing sessions have been revoked.
func (s *WikiService) Update(ctx context.Context, w *model.Wiki, in UpdateWikiInput) (passwordChanged bool, err error) {
	updates := map[string]any{}
	if in.Name != nil {
		name, err := validateWikiName(*in.Name)
		if err != nil {
			return false, err
		}
		updates["name"] = name
	}
	if in.Password != nil {
		hash := ""
		if *in.Password != "" {
			if hash, err = hashPassword(*in.Password); err != nil {
				return false, err
			}
		}
		updates["password_hash"] = hash
		passwordChanged = true
	}
	if len(updates) == 0 {
		return false, apperr.BadRequest("변경할 항목이 없습니다.")
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(w).Updates(updates).Error; err != nil {
			return err
		}
		if passwordChanged {
			return tx.Where("wiki_id = ?", w.ID).Delete(&model.WikiSession{}).Error
		}
		return nil
	})
	if err != nil {
		return false, err
	}
	return passwordChanged, s.db.WithContext(ctx).First(w, "id = ?", w.ID).Error
}

// Unlock verifies the password and returns a new session token.
func (s *WikiService) Unlock(ctx context.Context, w *model.Wiki, password string) (string, time.Time, error) {
	if !w.HasPassword() {
		return "", time.Time{}, nil
	}
	if bcrypt.CompareHashAndPassword([]byte(w.PasswordHash), []byte(password)) != nil {
		return "", time.Time{}, apperr.ErrPasswordInvalid
	}
	return s.IssueSession(ctx, w.ID)
}

func (s *WikiService) IssueSession(ctx context.Context, wikiID string) (string, time.Time, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", time.Time{}, err
	}
	token := base64.RawURLEncoding.EncodeToString(buf)
	exp := time.Now().Add(s.sessionTTL)
	err := s.db.WithContext(ctx).Create(&model.WikiSession{
		TokenHash: tokenHash(token),
		WikiID:    wikiID,
		ExpiresAt: exp,
	}).Error
	return token, exp, err
}

func (s *WikiService) ValidSession(ctx context.Context, wikiID, token string) bool {
	if token == "" {
		return false
	}
	var count int64
	s.db.WithContext(ctx).Model(&model.WikiSession{}).
		Where("token_hash = ? AND wiki_id = ? AND expires_at > ?", tokenHash(token), wikiID, time.Now()).
		Count(&count)
	return count > 0
}

func (s *WikiService) PurgeExpiredSessions(ctx context.Context) {
	s.db.WithContext(ctx).Where("expires_at <= ?", time.Now()).Delete(&model.WikiSession{})
}

func tokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
