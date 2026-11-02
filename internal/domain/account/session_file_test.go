package account

import (
	"bytes"
	"strings"
	"testing"
)

func TestCopyTelethonSessionBounded(t *testing.T) {
	t.Run("accepts maximum size", func(t *testing.T) {
		var dst bytes.Buffer
		src := strings.NewReader(strings.Repeat("x", maxTelethonSessionFileSize))
		if err := copyTelethonSessionBounded(&dst, src); err != nil {
			t.Fatalf("expected maximum-size file to pass: %v", err)
		}
		if dst.Len() != maxTelethonSessionFileSize {
			t.Fatalf("copied %d bytes, want %d", dst.Len(), maxTelethonSessionFileSize)
		}
	})

	t.Run("rejects oversized file after bounded read", func(t *testing.T) {
		var dst bytes.Buffer
		src := strings.NewReader(strings.Repeat("x", maxTelethonSessionFileSize+1024))
		if err := copyTelethonSessionBounded(&dst, src); err == nil {
			t.Fatal("expected oversized session file to be rejected")
		}
		if dst.Len() != maxTelethonSessionFileSize+1 {
			t.Fatalf("copied %d bytes; expected limit+1 guard read", dst.Len())
		}
	})
}
