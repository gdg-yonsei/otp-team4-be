package kakao

import (
	"strings"
	"testing"
	"time"
)

func TestParsePC(t *testing.T) {
	in := "\uFEFF우리들 님과 카카오톡 대화\n저장한 날짜 : 2026-11-05 12:00:00\n\n" +
		"--------------- 2026년 11월 5일 목요일 ---------------\n" +
		"[김철수] [오후 1:23] 부산 가자\n" +
		"12월 20일 어때?\n" +
		"[이영희] [오전 12:05] 좋아\n" +
		"박민수님이 들어왔습니다.\n"
	msgs, err := Parse(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 2 {
		t.Fatalf("want 2 messages, got %d: %+v", len(msgs), msgs)
	}
	if msgs[0].Sender != "김철수" || msgs[0].Text != "부산 가자\n12월 20일 어때?" {
		t.Errorf("unexpected first message: %+v", msgs[0])
	}
	want := time.Date(2026, 11, 5, 13, 23, 0, 0, KST)
	if !msgs[0].SentAt.Equal(want) {
		t.Errorf("time = %v, want %v", msgs[0].SentAt, want)
	}
	if msgs[1].SentAt.Hour() != 0 {
		t.Errorf("오전 12시는 0시여야 함: %v", msgs[1].SentAt)
	}
}

func TestParseMobile(t *testing.T) {
	in := "2026년 11월 5일 목요일\n" +
		"2026년 11월 5일 오후 1:23, 김철수 : 부산 가자\n" +
		"2026년 11월 5일 오후 1:24, 박민수님이 들어왔습니다.\n" +
		"2026. 11. 6. 오전 9:01, 이영희 : 좋아 : 진짜로\n"
	msgs, err := Parse(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 2 {
		t.Fatalf("want 2 messages, got %d: %+v", len(msgs), msgs)
	}
	if msgs[1].Sender != "이영희" || msgs[1].Text != "좋아 : 진짜로" {
		t.Errorf("unexpected message: %+v", msgs[1])
	}
	if msgs[1].SentAt.Day() != 6 || msgs[1].SentAt.Hour() != 9 {
		t.Errorf("unexpected time: %v", msgs[1].SentAt)
	}
}

func TestParseEnglishPC(t *testing.T) {
	in := "--------------- Thursday, November 5, 2026 ---------------\n[Chulsoo] [1:23 PM] hi\n"
	msgs, err := Parse(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 || msgs[0].SentAt.Hour() != 13 {
		t.Fatalf("unexpected: %+v", msgs)
	}
}
