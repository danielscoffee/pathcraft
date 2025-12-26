package time_test

import (
	"testing"

	"github.com/danielscoffee/pathcraft/internal/domain/time"
)

func TestParseTime(t *testing.T) {
	tests := []struct {
		input    string
		expected time.Time
		wantErr  bool
	}{
		{"08:30:00", 8*3600 + 30*60, false},
		{"00:00:00", 0, false},
		{"23:59:59", 23*3600 + 59*60 + 59, false},
		{"25:00:00", 25 * 3600, false},
		{"invalid", 0, true},
		{"08:30", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := time.ParseTime(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseTime(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.expected {
				t.Errorf("ParseTime(%q) = %v, want %v", tt.input, got, tt.expected)
			}
		})
	}
}

func TestTimeString(t *testing.T) {
	tests := []struct {
		time     time.Time
		expected string
	}{
		{8*3600 + 30*60, "08:30:00"},
		{0, "00:00:00"},
		{25 * 3600, "25:00:00"},
	}

	for _, tt := range tests {
		got := tt.time.String()
		if got != tt.expected {
			t.Errorf("Time(%d).String() = %q, want %q", tt.time, got, tt.expected)
		}
	}
}
