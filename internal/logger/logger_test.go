package logger

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestLogger_Info(t *testing.T) {
	var buf bytes.Buffer
	l := &Logger{service: "test", output: newTestLogger(&buf)}
	l.Info("test message", map[string]any{"key": "value"})

	var entry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if entry["service"] != "test" {
		t.Errorf("expected service=test, got %v", entry["service"])
	}
	if entry["level"] != "info" {
		t.Errorf("expected level=info, got %v", entry["level"])
	}
	if entry["message"] != "test message" {
		t.Errorf("expected message='test message', got %v", entry["message"])
	}
	if entry["key"] != "value" {
		t.Errorf("expected key=value, got %v", entry["key"])
	}
}

func TestLogger_Levels(t *testing.T) {
	var buf bytes.Buffer
	l := &Logger{service: "test", output: newTestLogger(&buf)}

	levels := []struct {
		fn   func(string, map[string]any)
		want string
	}{
		{l.Debug, "debug"},
		{l.Info, "info"},
		{l.Warn, "warn"},
		{l.Error, "error"},
	}

	for _, tc := range levels {
		buf.Reset()
		tc.fn("msg", nil)
		var entry map[string]any
		json.Unmarshal(buf.Bytes(), &entry)
		if entry["level"] != tc.want {
			t.Errorf("expected level=%s, got %v", tc.want, entry["level"])
		}
	}
}

func newTestLogger(buf *bytes.Buffer) *testLogger {
	return &testLogger{buf: buf}
}

type testLogger struct {
	buf *bytes.Buffer
}

func (tl *testLogger) Println(v ...any) {
	for i, val := range v {
		if i > 0 {
			tl.buf.WriteByte(' ')
		}
		tlv, _ := val.(string)
		tlv = strings.TrimSuffix(tlv, "\n")
		tl.buf.WriteString(tlv)
	}
	tl.buf.WriteByte('\n')
}
