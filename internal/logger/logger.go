package logger

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"
)

// Level represents log severity.
type Level string

const (
	DebugLevel Level = "debug"
	InfoLevel  Level = "info"
	WarnLevel  Level = "warn"
	ErrorLevel Level = "error"
)

// printer is the minimal interface needed by Logger.
type printer interface {
	Println(v ...any)
}

// Logger outputs structured JSON logs for ELK integration.
type Logger struct {
	service string
	output  printer
}

// New creates a structured logger.
func New(service string) *Logger {
	return &Logger{
		service: service,
		output:  log.New(os.Stdout, "", 0),
	}
}

// NewWithWriter creates a logger with a custom writer.
// P2-8 审计清理: NewWithWriter 死代码已删——全项目无调用点。
func (l *Logger) log(level Level, msg string, fields map[string]any) {
	entry := map[string]any{
		"timestamp": time.Now().UTC().Format(time.RFC3339),
		"service":   l.service,
		"level":     string(level),
		"message":   msg,
	}
	for k, v := range fields {
		entry[k] = v
	}
	b, _ := json.Marshal(entry)
	l.output.Println(string(b))
}

func (l *Logger) Debug(msg string, fields map[string]any) {
	l.log(DebugLevel, msg, fields)
}

func (l *Logger) Info(msg string, fields map[string]any) {
	l.log(InfoLevel, msg, fields)
}

func (l *Logger) Warn(msg string, fields map[string]any) {
	l.log(WarnLevel, msg, fields)
}

func (l *Logger) Error(msg string, fields map[string]any) {
	l.log(ErrorLevel, msg, fields)
}

// Fatal logs and exits.
func (l *Logger) Fatal(msg string, fields map[string]any) {
	l.log(ErrorLevel, msg, fields)
	os.Exit(1)
}

// Printf is a compatibility wrapper for standard log.Printf.
func (l *Logger) Printf(format string, v ...any) {
	l.Info(fmt.Sprintf(format, v...), nil)
}

// Write implements io.Writer so Logger can be used as Echo's log output.
func (l *Logger) Write(p []byte) (n int, err error) {
	l.Info(string(p), nil)
	return len(p), nil
}
