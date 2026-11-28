package kakao

import (
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

// ErrUnsupportedFile is returned by ParseFile for extensions other than .txt / .csv.
var ErrUnsupportedFile = errors.New("unsupported chat file type")

// ParseFile picks the parser by file extension (.txt or .csv).
func ParseFile(fileName string, data []byte) ([]Message, error) {
	switch strings.ToLower(filepath.Ext(fileName)) {
	case ".txt":
		return Parse(bytes.NewReader(data))
	case ".csv":
		return ParseCSV(bytes.NewReader(data))
	default:
		return nil, ErrUnsupportedFile
	}
}

// csvTimeLayouts are the accepted formats of the first column.
var csvTimeLayouts = []string{
	"2006-01-02 15:04:05",
	"2006-01-02 15:04",
	"2006-01-02T15:04:05",
	"2006/01/02 15:04:05",
	"2006. 1. 2. 15:04:05",
}

// ParseCSV reads a KakaoTalk CSV export:
//
//	Date,User,Message                          (optional header)
//	2026-11-25 19:59:58,"박건휘","저 집가는중이라..."
//
// Quoted fields may contain commas and line breaks. Rows whose first column is
// not a timestamp (headers, blanks) and rows without a sender (system notices)
// are skipped. Times are interpreted as KST.
func ParseCSV(r io.Reader) ([]Message, error) {
	cr := csv.NewReader(r)
	cr.FieldsPerRecord = -1 // tolerate rows with extra/missing columns
	cr.LazyQuotes = true
	cr.ReuseRecord = true

	var msgs []Message
	for first := true; ; first = false {
		rec, err := cr.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("csv: %w", err)
		}
		if first && len(rec) > 0 {
			rec[0] = strings.TrimPrefix(rec[0], "\uFEFF")
		}
		if len(rec) < 3 {
			continue
		}
		sentAt, ok := parseCSVTime(rec[0])
		if !ok {
			continue // header row or garbage
		}
		sender := strings.TrimSpace(rec[1])
		// A message containing an unquoted comma is split into extra columns; rejoin them.
		text := strings.Join(rec[2:], ",")
		if !utf8.ValidString(sender) || !utf8.ValidString(text) {
			return nil, fmt.Errorf("file is not valid UTF-8")
		}
		if sender == "" || isSystemLine(text) {
			continue
		}
		msgs = append(msgs, Message{Sender: sender, SentAt: sentAt, Text: text})
	}
	return msgs, nil
}

func parseCSVTime(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	for _, layout := range csvTimeLayouts {
		if t, err := time.ParseInLocation(layout, s, KST); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}
