// Package apperr defines API errors that map to the common error response
// format: {"code": "...", "message": "..."}.
package apperr

import (
	"errors"
	"net/http"
)

type Error struct {
	Status  int    `json:"-"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

func New(status int, code, message string) *Error {
	return &Error{Status: status, Code: code, Message: message}
}

func BadRequest(message string) *Error {
	return New(http.StatusBadRequest, "INVALID_REQUEST", message)
}

var (
	ErrWikiLocked        = New(http.StatusUnauthorized, "WIKI_LOCKED", "비밀번호 인증이 필요합니다.")
	ErrPasswordInvalid   = New(http.StatusForbidden, "WIKI_PASSWORD_INVALID", "비밀번호가 올바르지 않습니다.")
	ErrWikiNotFound      = New(http.StatusNotFound, "WIKI_NOT_FOUND", "위키를 찾을 수 없습니다.")
	ErrPageNotFound      = New(http.StatusNotFound, "PAGE_NOT_FOUND", "문서를 찾을 수 없습니다.")
	ErrChatNotFound      = New(http.StatusNotFound, "CHAT_NOT_FOUND", "대화 내역을 찾을 수 없습니다.")
	ErrJobNotFound       = New(http.StatusNotFound, "JOB_NOT_FOUND", "작업을 찾을 수 없습니다.")
	ErrVersionConflict   = New(http.StatusConflict, "PAGE_VERSION_CONFLICT", "다른 사용자가 먼저 문서를 수정했습니다. 최신 버전을 다시 불러와 주세요.")
	ErrTitleDuplicate    = New(http.StatusConflict, "PAGE_TITLE_DUPLICATE", "같은 제목의 문서가 이미 존재합니다.")
	ErrGenerationRunning = New(http.StatusConflict, "GENERATION_IN_PROGRESS", "이미 진행 중인 AI 생성 작업이 있습니다.")
	ErrFileTooLarge      = New(http.StatusRequestEntityTooLarge, "FILE_TOO_LARGE", "업로드 파일 크기가 제한을 초과했습니다.")
	ErrInvalidChatFile   = New(http.StatusBadRequest, "INVALID_CHAT_FILE", "카카오톡 대화 내보내기 TXT 파일 형식이 아닙니다.")
	ErrTooManyRequests   = New(http.StatusTooManyRequests, "TOO_MANY_REQUESTS", "요청 횟수를 초과했습니다. 잠시 후 다시 시도해 주세요.")
	ErrInternal          = New(http.StatusInternalServerError, "INTERNAL_ERROR", "서버 내부 오류가 발생했습니다.")
)

// From converts any error into an *Error, defaulting to 500.
func From(err error) *Error {
	var e *Error
	if errors.As(err, &e) {
		return e
	}
	return ErrInternal
}
