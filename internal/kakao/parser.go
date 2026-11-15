// Package kakao parses KakaoTalk chat export TXT files.
//
// Supported formats (Korean and English locales):
//
//	PC:       --------------- 2026년 11월 5일 목요일 ---------------
//	          [홍길동] [오후 1:23] 안녕하세요
//	Android:  2026년 11월 5일 오후 1:23, 홍길동 : 안녕하세요
//	iOS:      2026. 11. 5. 오후 1:23, 홍길동 : 안녕하세요
//
// Lines that do not start a new message are appended to the previous message
// (multi-line messages). Join/leave notices and date separators are skipped.
package kakao

import (
	"bufio"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

var KST = time.FixedZone("KST", 9*60*60)

type Message struct {
	Sender string
	SentAt time.Time
	Text   string
}

var (
	// PC date separator: "--------------- 2026년 11월 5일 목요일 ---------------"
	rePCDateKo = regexp.MustCompile(`^-{3,}\s*(\d{4})년\s*(\d{1,2})월\s*(\d{1,2})일.*?-{3,}\s*$`)
	// PC date separator (English): "--------------- Thursday, November 5, 2026 ---------------"
	rePCDateEn = regexp.MustCompile(`^-{3,}\s*(?:[A-Za-z]+,\s*)?([A-Za-z]+)\s+(\d{1,2}),\s*(\d{4})\s*-{3,}\s*$`)
	// PC message: "[홍길동] [오후 1:23] 내용" / "[Name] [1:23 PM] text"
	rePCMsg = regexp.MustCompile(`^\[(.+?)\] \[(오전|오후)?\s*(\d{1,2}):(\d{2})\s*(AM|PM)?\] ?(.*)$`)
	// Mobile message: "2026년 11월 5일 오후 1:23, 홍길동 : 내용" or "2026. 11. 5. 오후 1:23, 홍길동 : 내용"
	reMobileLine = regexp.MustCompile(`^(\d{4})(?:년\s*|\.\s*)(\d{1,2})(?:월\s*|\.\s*)(\d{1,2})(?:일|\.)\s*(오전|오후)\s*(\d{1,2}):(\d{2}),?\s*(.*)$`)
	// Mobile date separator: "2026년 11월 5일 목요일"
	reMobileDate = regexp.MustCompile(`^(\d{4})년\s*(\d{1,2})월\s*(\d{1,2})일\s*\S+요일\s*$`)
)

var systemSuffixes = []string{
	"님이 들어왔습니다.", "님이 나갔습니다.", "님을 초대했습니다.", "님을 내보냈습니다.",
	"초대했습니다.", "채팅방 관리자가 메시지를 가렸습니다.", "삭제된 메시지입니다.",
}

// Parse reads a KakaoTalk export and returns its messages in order.
func Parse(r io.Reader) ([]Message, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	var (
		msgs    []Message
		curDate time.Time
		first   = true
	)
	appendCont := func(line string) {
		if len(msgs) > 0 {
			msgs[len(msgs)-1].Text += "\n" + line
		}
	}

	for sc.Scan() {
		line := sc.Text()
		if first {
			line = strings.TrimPrefix(line, "\uFEFF")
			first = false
		}
		if !utf8.ValidString(line) {
			return nil, fmt.Errorf("file is not valid UTF-8")
		}
		trimmed := strings.TrimRight(line, "\r")

		if m := rePCDateKo.FindStringSubmatch(trimmed); m != nil {
			curDate = ymd(m[1], m[2], m[3])
			continue
		}
		if m := rePCDateEn.FindStringSubmatch(trimmed); m != nil {
			if t, err := time.ParseInLocation("January 2 2006", m[1]+" "+m[2]+" "+m[3], KST); err == nil {
				curDate = t
			}
			continue
		}
		if m := rePCMsg.FindStringSubmatch(trimmed); m != nil && !curDate.IsZero() {
			msgs = append(msgs, Message{
				Sender: m[1],
				SentAt: withClock(curDate, m[2]+m[5], m[3], m[4]),
				Text:   m[6],
			})
			continue
		}
		if reMobileDate.MatchString(trimmed) {
			continue
		}
		if m := reMobileLine.FindStringSubmatch(trimmed); m != nil {
			rest := m[7]
			idx := strings.Index(rest, " : ")
			if idx < 0 {
				// e.g. "2026년 11월 5일 오후 1:23, 홍길동님이 들어왔습니다." -> system notice
				continue
			}
			msgs = append(msgs, Message{
				Sender: strings.TrimSpace(rest[:idx]),
				SentAt: withClock(ymd(m[1], m[2], m[3]), m[4], m[5], m[6]),
				Text:   rest[idx+3:],
			})
			continue
		}
		if isSystemLine(trimmed) {
			continue
		}
		appendCont(trimmed)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return msgs, nil
}

func isSystemLine(s string) bool {
	for _, suf := range systemSuffixes {
		if strings.HasSuffix(s, suf) {
			return true
		}
	}
	return false
}

func ymd(y, m, d string) time.Time {
	yi, _ := strconv.Atoi(y)
	mi, _ := strconv.Atoi(m)
	di, _ := strconv.Atoi(d)
	return time.Date(yi, time.Month(mi), di, 0, 0, 0, 0, KST)
}

// withClock applies a 12-hour clock ("오전"/"오후"/"AM"/"PM") to a date.
func withClock(date time.Time, meridiem, hh, mm string) time.Time {
	h, _ := strconv.Atoi(hh)
	mi, _ := strconv.Atoi(mm)
	switch meridiem {
	case "오후", "PM":
		if h < 12 {
			h += 12
		}
	case "오전", "AM":
		if h == 12 {
			h = 0
		}
	}
	return time.Date(date.Year(), date.Month(), date.Day(), h, mi, 0, 0, KST)
}
