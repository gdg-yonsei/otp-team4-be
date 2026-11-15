package model

import "time"

type Wiki struct {
	ID           string `gorm:"primaryKey;size:36"`
	Name         string `gorm:"size:100;not null"`
	PasswordHash string `gorm:"size:100"`
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

func (w *Wiki) HasPassword() bool { return w.PasswordHash != "" }

// WikiSession is issued after a successful unlock. Only the SHA-256 hash of
// the cookie token is stored.
type WikiSession struct {
	TokenHash string    `gorm:"primaryKey;size:64"`
	WikiID    string    `gorm:"size:36;index;not null"`
	ExpiresAt time.Time `gorm:"index"`
	CreatedAt time.Time
}

type Chat struct {
	ID           string `gorm:"primaryKey;size:64"`
	WikiID       string `gorm:"size:36;index;not null"`
	FileName     string `gorm:"size:255"`
	MessageCount int
	Content      string `gorm:"type:text"`
	UploadedAt   time.Time
}

type PageCategory string

const (
	CategoryPerson   PageCategory = "PERSON"
	CategoryEvent    PageCategory = "EVENT"
	CategoryIncident PageCategory = "INCIDENT"
	CategorySchedule PageCategory = "SCHEDULE"
	CategoryPlace    PageCategory = "PLACE"
	CategoryGroup    PageCategory = "GROUP"
	CategoryMeme     PageCategory = "MEME"
	CategoryOther    PageCategory = "OTHER"
)

var Categories = []PageCategory{
	CategoryPerson, CategoryEvent, CategoryIncident, CategorySchedule,
	CategoryPlace, CategoryGroup, CategoryMeme, CategoryOther,
}

func (c PageCategory) Valid() bool {
	for _, v := range Categories {
		if c == v {
			return true
		}
	}
	return false
}

// Editor identifies who last wrote a page.
type Editor string

const (
	EditorUser Editor = "USER"
	EditorAI   Editor = "AI"
)

type Page struct {
	ID         string       `gorm:"primaryKey;size:64"`
	WikiID     string       `gorm:"size:36;not null;uniqueIndex:idx_wiki_title"`
	Title      string       `gorm:"size:200;not null;uniqueIndex:idx_wiki_title"`
	Category   PageCategory `gorm:"size:20;not null;index"`
	Content    string       `gorm:"type:text"`
	Version    int          `gorm:"not null;default:1"`
	CreatedBy  Editor       `gorm:"size:10;not null"`
	LastEditor Editor       `gorm:"size:10;not null"`
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

type JobStatus string

const (
	JobPending    JobStatus = "PENDING"
	JobProcessing JobStatus = "PROCESSING"
	JobCompleted  JobStatus = "COMPLETED"
	JobFailed     JobStatus = "FAILED"
)

type GenerationMode string

const (
	ModeMerge      GenerationMode = "MERGE"
	ModeRegenerate GenerationMode = "REGENERATE"
)

type GenerationJob struct {
	ID             string         `gorm:"primaryKey;size:64"`
	WikiID         string         `gorm:"size:36;index;not null"`
	Mode           GenerationMode `gorm:"size:20;not null"`
	Status         JobStatus      `gorm:"size:20;index;not null"`
	ChatIDs        []string       `gorm:"serializer:json;type:text"`
	CreatedPageIDs []string       `gorm:"serializer:json;type:text"`
	UpdatedPageIDs []string       `gorm:"serializer:json;type:text"`
	Error          string         `gorm:"type:text"`
	CreatedAt      time.Time
	DispatchedAt   *time.Time
	StartedAt      *time.Time
	FinishedAt     *time.Time
}
