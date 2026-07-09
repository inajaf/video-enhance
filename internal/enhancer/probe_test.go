package enhancer

import "testing"

func TestParseRate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  float64
	}{
		{name: "fraction", input: "30000/1001", want: 29.97002997002997},
		{name: "whole number", input: "30", want: 30},
		{name: "zero denominator", input: "1/0", want: 0},
		{name: "empty", input: "", want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assertFloat(t, parseRate(tt.input), tt.want)
		})
	}
}

func TestNormalizeRate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  string
	}{
		{input: "", want: "30"},
		{input: "0/0", want: "30"},
		{input: " 30000/1001 ", want: "30000/1001"},
	}

	for _, tt := range tests {
		if got := normalizeRate(tt.input); got != tt.want {
			t.Fatalf("normalizeRate(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
