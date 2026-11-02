package account

import (
	"errors"
	"testing"
	"time"

	"tg-sender/internal/config"
)

type failingRandomReader struct{}

func (failingRandomReader) Read([]byte) (int, error) {
	return 0, errors.New("random source unavailable")
}

func TestGuardJitterBounds(t *testing.T) {
	tests := []struct {
		name string
		min time.Duration
		max time.Duration
		wantMin time.Duration
		wantMax time.Duration
	}{
		{name: "configured bounds", min: 3 * time.Second, max: 8 * time.Second, wantMin: 3 * time.Second, wantMax: 8 * time.Second},
		{name: "invalid range uses defaults", min: 5 * time.Second, max: 5 * time.Second, wantMin: 0, wantMax: 2 * time.Second},
		{name: "negative minimum is normalized", min: -time.Second, max: time.Second, wantMin: 0, wantMax: time.Second},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := &Engine{cfg: &config.Config{GuardMinJitter: tt.min, GuardMaxJitter: tt.max}}
			gotMin, gotMax := e.guardJitterBounds()
			if gotMin != tt.wantMin || gotMax != tt.wantMax {
				t.Fatalf("guardJitterBounds() = [%v, %v), want [%v, %v)", gotMin, gotMax, tt.wantMin, tt.wantMax)
			}
		})
	}
}

func TestApplyGuardRailIncludesMinimumJitter(t *testing.T) {
	e := &Engine{cfg: &config.Config{
		GuardMinJitter: 3 * time.Second,
		GuardMaxJitter: 8 * time.Second,
		GuardMinSpread: 2 * time.Second,
		GuardMaxSpread: 5 * time.Second,
	}}
	min, _ := e.applyGuardRail(time.Second, 20*time.Second, 10*time.Second)
	if min < 13*time.Second || min >= 18*time.Second {
		t.Fatalf("minimum interval %v outside configured jitter range [13s, 18s)", min)
	}
}

func TestRandomDurationReturnsZeroWhenRandomSourceFails(t *testing.T) {
	if got := randomDurationFromReader(time.Second, failingRandomReader{}); got != 0 {
		t.Fatalf("randomDurationFromReader() = %v, want 0 on reader failure", got)
	}
}

func TestRandomDurationHandlesNonPositiveMaximum(t *testing.T) {
	for _, max := range []time.Duration{0, -time.Second} {
		if got := randomDurationFromReader(max, failingRandomReader{}); got != 0 {
			t.Fatalf("randomDurationFromReader(%v) = %v, want 0", max, got)
		}
	}
}
