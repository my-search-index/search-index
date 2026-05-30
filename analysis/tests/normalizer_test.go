package tests

import (
	"reflect"
	"testing"

	"github.com/my-search-index/search-index/analysis"
)

func TestNormalize(t *testing.T) {
	tests := []struct {
		name  string
		input analysis.Token
		want  analysis.Token
	}{
		{
			name:  "happy path",
			input: analysis.Token{Word: "Hello World"},
			want:  analysis.Token{Word: "hello world"},
		},
		{
			name:  "mixed case",
			input: analysis.Token{Word: "Go Lang"},
			want:  analysis.Token{Word: "go lang"},
		},
		{
			name:  "already lowercase",
			input: analysis.Token{Word: "search index"},
			want:  analysis.Token{Word: "search index"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := analysis.Normalize(tt.input)

			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Normalize(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}

}

func TestNormalizeAll(t *testing.T) {
	tests := []struct {
		name  string
		input []analysis.Token
		want  []analysis.Token
	}{
		{
			name:  "happy path",
			input: []analysis.Token{{Word: "Hello World"}, {Word: "Go Lang"}},
			want:  []analysis.Token{{Word: "hello world"}, {Word: "go lang"}},
		},
		{
			name:  "already lowercase",
			input: []analysis.Token{{Word: "search index"}, {Word: "go lang"}},
			want:  []analysis.Token{{Word: "search index"}, {Word: "go lang"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := analysis.NormalizeAll(tt.input)

			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("NormalizeAll(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}

}
