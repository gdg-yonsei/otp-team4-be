package handler

import (
	"time"

	"otp-server/internal/model"
)

// Request/response bodies. They are named types (not anonymous structs or gin.H)
// so that swag can describe them in the OpenAPI document.

// ---------- wikis ----------

type CreateWikiRequest struct {
	Name     string  `json:"name" example:"우리들의 위키"`
	Password *string `json:"password,omitempty" example:"1234"` // 생략하면 비밀번호 없는 위키
}

type UnlockRequest struct {
	Password string `json:"password" example:"1234"`
}

type UpdateWikiRequest struct {
	Name *string `json:"name,omitempty" example:"새 이름"`
	// 생략/null: 그대로, "": 비밀번호 제거, 값: 새 비밀번호
	Password *string `json:"password,omitempty"`
}

type WikiResponse struct {
	ID          string    `json:"id" example:"550e8400-e29b-41d4-a716-446655440000"`
	Name        string    `json:"name" example:"우리들의 위키"`
	URL         string    `json:"url,omitempty" example:"https://katalk.wiki/w/550e8400-e29b-41d4-a716-446655440000"`
	HasPassword bool      `json:"hasPassword"`
	CreatedAt   time.Time `json:"createdAt"`
}

// ---------- chats ----------

type ChatResponse struct {
	ID           string    `json:"id" example:"chat_9f1c2a"`
	FileName     string    `json:"fileName" example:"KakaoTalk_Chat.txt"`
	MessageCount int       `json:"messageCount" example:"1523"`
	UploadedAt   time.Time `json:"uploadedAt"`
}

type ChatListResponse struct {
	Chats []ChatResponse `json:"chats"`
}

// ---------- generations ----------

type CreateGenerationRequest struct {
	ChatIDs []string             `json:"chatIds" example:"chat_9f1c2a"`
	Mode    model.GenerationMode `json:"mode" enums:"MERGE,REGENERATE" example:"MERGE"`
}

type CreateGenerationResponse struct {
	JobID  string          `json:"jobId" example:"job_9f1c2a"`
	Status model.JobStatus `json:"status" enums:"PENDING,PROCESSING,COMPLETED,FAILED" example:"PENDING"`
}

type ReportGenerationRequest struct {
	Status         model.JobStatus `json:"status" enums:"PROCESSING,COMPLETED,FAILED" example:"COMPLETED"`
	CreatedPageIDs []string        `json:"createdPageIds,omitempty"` // 생략하면 기존 값 유지
	UpdatedPageIDs []string        `json:"updatedPageIds,omitempty"` // 생략하면 기존 값 유지
	Error          string          `json:"error,omitempty"`          // FAILED일 때 사유
}

type JobResponse struct {
	JobID          string               `json:"jobId" example:"job_9f1c2a"`
	Status         model.JobStatus      `json:"status" enums:"PENDING,PROCESSING,COMPLETED,FAILED"`
	Mode           model.GenerationMode `json:"mode" enums:"MERGE,REGENERATE"`
	CreatedPages   int                  `json:"createdPages"`
	UpdatedPages   int                  `json:"updatedPages"`
	CreatedPageIDs []string             `json:"createdPageIds"`
	UpdatedPageIDs []string             `json:"updatedPageIds"`
	Error          string               `json:"error,omitempty"`
	CreatedAt      time.Time            `json:"createdAt"`
	FinishedAt     *time.Time           `json:"finishedAt,omitempty"`
}

// ---------- pages ----------

type PageSummary struct {
	ID       string             `json:"id" example:"page_1a2b3c"`
	Title    string             `json:"title" example:"김철수"`
	Category model.PageCategory `json:"category" enums:"PERSON,EVENT,INCIDENT,SCHEDULE,PLACE,GROUP,MEME,OTHER"`
}

type PageListResponse struct {
	Pages []PageSummary `json:"pages"`
}

type CreatePageRequest struct {
	Title    string             `json:"title" example:"김철수/논란 및 사건사고"`
	Category model.PageCategory `json:"category" enums:"PERSON,EVENT,INCIDENT,SCHEDULE,PLACE,GROUP,MEME,OTHER" example:"PERSON"`
	Content  string             `json:"content" example:"== 개요 ==\n..."`
}

type CreatePageResponse struct {
	ID    string `json:"id" example:"page_1a2b3c"`
	Title string `json:"title" example:"김철수/논란 및 사건사고"`
}

type PageResponse struct {
	ID        string             `json:"id" example:"page_1a2b3c"`
	Title     string             `json:"title" example:"김철수"`
	Category  model.PageCategory `json:"category" enums:"PERSON,EVENT,INCIDENT,SCHEDULE,PLACE,GROUP,MEME,OTHER"`
	Content   string             `json:"content"`
	Version   int                `json:"version" example:"3"`
	UpdatedAt time.Time          `json:"updatedAt"`
}

type UpdatePageRequest struct {
	Title    *string             `json:"title,omitempty"`
	Category *model.PageCategory `json:"category,omitempty" enums:"PERSON,EVENT,INCIDENT,SCHEDULE,PLACE,GROUP,MEME,OTHER"`
	Content  *string             `json:"content,omitempty"`
	Version  int                 `json:"version" example:"3"` // 조회 시 받은 version. 다르면 409
}

type UpdatePageResponse struct {
	ID        string    `json:"id" example:"page_1a2b3c"`
	Version   int       `json:"version" example:"4"`
	UpdatedAt time.Time `json:"updatedAt"`
}
