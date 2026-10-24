package main

import (
	"testing"
	"time"
)

func TestHumanDuration(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{-time.Second, "0s"}, // clock skew
		{0, "0s"},
		{42 * time.Second, "42s"},
		{5*time.Minute + 30*time.Second, "5m"},
		{2*time.Hour + 14*time.Minute, "2h 14m"},
		{3*24*time.Hour + 4*time.Hour + 59*time.Minute, "3d 4h"},
	}
	for _, tt := range tests {
		if got := humanDuration(tt.d); got != tt.want {
			t.Errorf("humanDuration(%v) = %q, want %q", tt.d, got, tt.want)
		}
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		s    string
		n    int
		want string
	}{
		{"vite", 10, "vite"},
		{"python3 -m http.server", 10, "python3 -…"},
		{"héllo wörld", 6, "héllo…"}, // counts runes, not bytes
	}
	for _, tt := range tests {
		if got := truncate(tt.s, tt.n); got != tt.want {
			t.Errorf("truncate(%q, %d) = %q, want %q", tt.s, tt.n, got, tt.want)
		}
	}
}
