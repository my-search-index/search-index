package tests

import (
	"reflect"
	"testing"

	"github.com/my-search-index/search-index/analysis"
)

func TestTokenize(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []analysis.Token
	}{
		{
			name:  "happy path",
			input: "Hello world",
			want:  []analysis.Token{{"Hello", 0}, {"world", 1}},
		},
		{
			name:  "leading delimiters",
			input: "  hello world",
			want:  []analysis.Token{{"hello", 0}, {"world", 1}},
		},
		{
			name:  "consecutive delimiters",
			input: "foo,,bar",
			want:  []analysis.Token{{"foo", 0}, {"bar", 1}},
		},
		{
			name:  "empty input",
			input: "",
			want:  nil,
		},
		{
			name:  "only delimiters",
			input: "---!!!",
			want:  nil,
		},
		{
			name:  "casing preserved",
			input: "Go Lang",
			want:  []analysis.Token{{"Go", 0}, {"Lang", 1}},
		},
		{
			name:  "mixed alphanumeric split",
			input: "go1.21",
			want:  []analysis.Token{{"go1", 0}, {"21", 1}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := analysis.Tokenize(tt.input)

			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Tokenize(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}
