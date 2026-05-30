package tests

import (
	"reflect"
	"testing"

	"github.com/my-search-index/search-index/analysis"
)

func TestStopWords(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"common stop word", "the", true},
		{"another common stop word", "and", true},
		{"non-stop word", "hello", false},
		{"empty string", "", false},
		{"capitalized", "THE", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := analysis.IsStopWord(tt.input)
			if got != tt.want {
				t.Errorf("IsStopWord(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestFilterStopWords(t *testing.T) {
	tests := []struct {
		name  string
		input []analysis.Token
		want  []analysis.Token
	}{
		{
			name:  "mixed tokens",
			input: []analysis.Token{{"the", 0}, {"hello", 1}, {"and", 2}, {"world", 3}},
			want:  []analysis.Token{{"hello", 1}, {"world", 3}},
		},
		{
			name:  "all stop words",
			input: []analysis.Token{{"the", 0}, {"and", 1}},
			want:  nil,
		},
		{
			name:  "no stop words",
			input: []analysis.Token{{"hello", 0}, {"world", 1}},
			want:  []analysis.Token{{"hello", 0}, {"world", 1}},
		},
		{
			name:  "unchanged tokens positions",
			input: []analysis.Token{{"golang", 2}, {"golang", 1}},
			want:  []analysis.Token{{"golang", 2}, {"golang", 1}},
		},
		{
			name:  "Capitalised tokens must survive confirming that stop word check is case-sensitive",
			input: []analysis.Token{{"GOLANG", 0}, {"GOLANG", 1}},
			want:  []analysis.Token{{"GOLANG", 0}, {"GOLANG", 1}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := analysis.FilterStopWords(tt.input)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("FilterStopWords(%v) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}
