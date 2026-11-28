package kakao

import (
	"testing"
	"time"
)

func TestParseCSV(t *testing.T) {
	in := "\uFEFFDate,User,Message\n" +
		`2026-11-25 19:59:58,"박건휘","저 집가는중이라 통화로 참여하다 옮기겠습니다ㅜㅜ"` + "\n" +
		`2026-11-25 20:00:24,"김민정","저도... 조금 있다가 옮기겠습니다"` + "\n" +
		`2026-11-25 20:01:00,"김민정","여러 줄` + "\n" + `메시지, 쉼표 포함"` + "\n" +
		`2026-11-25 20:02:00,"","박민수님이 들어왔습니다."` + "\n"
	msgs, err := ParseFile("KakaoTalk_Chat.csv", []byte(in))
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 3 {
		t.Fatalf("want 3 messages, got %d: %+v", len(msgs), msgs)
	}
	if msgs[0].Sender != "박건휘" || !msgs[0].SentAt.Equal(time.Date(2026, 11, 25, 19, 59, 58, 0, KST)) {
		t.Errorf("unexpected first message: %+v", msgs[0])
	}
	if msgs[2].Text != "여러 줄\n메시지, 쉼표 포함" {
		t.Errorf("multi-line text = %q", msgs[2].Text)
	}
}

func TestParseFileUnsupported(t *testing.T) {
	if _, err := ParseFile("chat.json", []byte("{}")); err != ErrUnsupportedFile {
		t.Fatalf("err = %v, want ErrUnsupportedFile", err)
	}
}
