package task

import "testing"

func TestExtractFloodSeconds(t *testing.T) {
	cases := []struct {
		name string
		err  string
		want int
	}{
		// 正则 flood_wait_(\d+) 匹配的是 Telegram 原生格式 FLOOD_WAIT_<秒数>
		{"telegram native format", "FLOOD_WAIT_420", 420},
		{"embedded in sentence", "rpc error: FLOOD_WAIT_75: too many requests", 75},
		{"lowercase", "flood_wait_30", 30},
		{"colon but underscore missing", "flood wait 420", 0},
		{"unrelated error", "connection reset by peer", 0},
		{"empty", "", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := extractFloodSeconds(tc.err); got != tc.want {
				t.Errorf("extractFloodSeconds(%q) = %d, want %d", tc.err, got, tc.want)
			}
		})
	}
}

func TestIsInviteRequestSent(t *testing.T) {
	if isInviteRequestSent(nil) {
		t.Error("nil error must not be treated as invite-sent")
	}
	if !isInviteRequestSent(errString("INVITE_REQUEST_SENT")) {
		t.Error("INVITE_REQUEST_SENT should be detected")
	}
	if isInviteRequestSent(errString("random failure")) {
		t.Error("unrelated error must not match")
	}
}

type errString string

func (e errString) Error() string { return string(e) }
