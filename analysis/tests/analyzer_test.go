package tests

import (
	"reflect"
	"testing"

	"github.com/my-search-index/search-index/analysis"
)

func TestAnalyze(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []analysis.Token
	}{
		{
			name:  "happy path",
			input: "The quick brown fox jumps over the lazy dog",
			want: []analysis.Token{
				{"quick", 1},
				{"brown", 2},
				{"fox", 3},
				{"jumps", 4},
				{"lazy", 7},
				{"dog", 8},
			},
		},
		{
			name:  "with punctuation",
			input: "Hello, world! This is a test.",
			want: []analysis.Token{
				{"hello", 0},
				{"world", 1},
				{"test", 5},
			},
		},
		{
			name:  "only stop words",
			input: "the and is in",
			want:  nil,
		},
		{
			name:  "empty input",
			input: "",
			want:  nil,
		},
		{
			name:  "mixed case and stop words",
			input: "Go is great, but it can be tricky.",
			want: []analysis.Token{
				{"go", 0},
				{"great", 2},
				{"tricky", 7},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := analysis.Analyze(tt.input)

			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Analyze(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}
