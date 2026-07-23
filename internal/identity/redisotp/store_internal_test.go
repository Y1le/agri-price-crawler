package redisotp

import (
	"testing"
	"time"
)

func TestDurationMillisecondsCeilNeverShortensAcceptedDuration(t *testing.T) {
	tests := []struct {
		name     string
		duration time.Duration
		want     int64
	}{
		{name: "whole millisecond", duration: time.Millisecond, want: 1},
		{name: "fractional millisecond", duration: time.Millisecond + 1, want: 2},
		{name: "fractional second", duration: time.Second + time.Nanosecond, want: 1001},
		{name: "maximum duration", duration: time.Duration(1<<63 - 1), want: 9_223_372_036_855},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := durationMillisecondsCeil(test.duration); got != test.want {
				t.Fatalf("durationMillisecondsCeil(%v) = %d, want %d", test.duration, got, test.want)
			}
		})
	}
}
