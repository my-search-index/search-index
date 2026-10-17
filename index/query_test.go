package index

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

// pt is a shorthand for building expected phrase terms.
func pt(word string, offset int) phraseTerm { return phraseTerm{Word: word, Offset: offset} }

func TestParseQuery(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  parsedQuery
	}{
		{
			name:  "bare terms only",
			input: "golang search",
			want:  parsedQuery{Terms: []string{"golang", "search"}},
		},
		{
			name:  "single phrase",
			input: `"golang search"`,
			want:  parsedQuery{Phrases: []phrase{{Terms: []phraseTerm{pt("golang", 0), pt("search", 1)}}}},
		},
		{
			name:  "phrase mixed with bare term",
			input: `"golang search" tutorial`,
			want: parsedQuery{
				Phrases: []phrase{{Terms: []phraseTerm{pt("golang", 0), pt("search", 1)}}},
				Terms:   []string{"tutorial"},
			},
		},
		{
			name:  "bare term before phrase",
			input: `tutorial "golang search"`,
			want: parsedQuery{
				Phrases: []phrase{{Terms: []phraseTerm{pt("golang", 0), pt("search", 1)}}},
				Terms:   []string{"tutorial"},
			},
		},
		{
			name:  "two phrases with text between",
			input: `"red fox" jumps "blue whale"`,
			want: parsedQuery{
				Phrases: []phrase{
					{Terms: []phraseTerm{pt("red", 0), pt("fox", 1)}},
					{Terms: []phraseTerm{pt("blue", 0), pt("whale", 1)}},
				},
				Terms: []string{"jumps"},
			},
		},
		{
			name:  "two adjacent phrases",
			input: `"red fox""blue whale"`,
			want: parsedQuery{
				Phrases: []phrase{
					{Terms: []phraseTerm{pt("red", 0), pt("fox", 1)}},
					{Terms: []phraseTerm{pt("blue", 0), pt("whale", 1)}},
				},
			},
		},
		{
			name:  "interior stopword keeps its gap",
			input: `"search the golang"`,
			want:  parsedQuery{Phrases: []phrase{{Terms: []phraseTerm{pt("search", 0), pt("golang", 2)}}}},
		},
		{
			name:  "two interior stopwords keep a gap of three",
			input: `"search of the golang"`,
			want:  parsedQuery{Phrases: []phrase{{Terms: []phraseTerm{pt("search", 0), pt("golang", 3)}}}},
		},
		{
			name:  "leading stopword is trimmed",
			input: `"the search"`,
			want:  parsedQuery{Phrases: []phrase{{Terms: []phraseTerm{pt("search", 0)}}}},
		},
		{
			name:  "trailing stopword is trimmed",
			input: `"search the"`,
			want:  parsedQuery{Phrases: []phrase{{Terms: []phraseTerm{pt("search", 0)}}}},
		},
		{
			name:  "single word phrase stays a phrase",
			input: `"golang"`,
			want:  parsedQuery{Phrases: []phrase{{Terms: []phraseTerm{pt("golang", 0)}}}},
		},
		{
			name:  "quote in the middle of a word is a delimiter",
			input: `foo"bar baz"`,
			want: parsedQuery{
				Phrases: []phrase{{Terms: []phraseTerm{pt("bar", 0), pt("baz", 1)}}},
				Terms:   []string{"foo"},
			},
		},
		{
			name:  "case and punctuation are normalized",
			input: `"Golang, Search!"`,
			want:  parsedQuery{Phrases: []phrase{{Terms: []phraseTerm{pt("golang", 0), pt("search", 1)}}}},
		},
		{
			name:  "hyphenated bare word splits into terms",
			input: `golang-search`,
			want:  parsedQuery{Terms: []string{"golang", "search"}},
		},
		{
			name:  "bare stopwords are dropped without error",
			input: `the of`,
			want:  parsedQuery{},
		},
		{
			name:  "empty query",
			input: ``,
			want:  parsedQuery{},
		},
		{
			name:  "whitespace only query",
			input: "   \t ",
			want:  parsedQuery{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseQuery(tt.input)
			if err != nil {
				t.Fatalf("parseQuery(%q) unexpected error: %v", tt.input, err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("parseQuery(%q)\n got: %+v\nwant: %+v", tt.input, got, tt.want)
			}
		})
	}
}

func TestParseQueryErrors(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr error
		wantMsg string // substring expected in the message (position info)
	}{
		{"unterminated at start", `"golang search`, ErrUnterminatedQuote, "position 0"},
		{"unterminated after bare text", `golang "search`, ErrUnterminatedQuote, "position 7"},
		{"odd number of quotes", `"a b" "c d`, ErrUnterminatedQuote, "position 6"},
		{"lone quote", `"`, ErrUnterminatedQuote, "position 0"},
		{"empty phrase", `""`, ErrEmptyPhrase, ""},
		{"whitespace only phrase", `"   "`, ErrEmptyPhrase, ""},
		{"stopword only phrase", `"to be or not to be"`, ErrEmptyPhrase, ""},
		{"punctuation only phrase", `"!!!"`, ErrEmptyPhrase, ""},
		{"empty phrase among valid ones", `"red fox" "" "blue whale"`, ErrEmptyPhrase, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseQuery(tt.input)
			if err == nil {
				t.Fatalf("parseQuery(%q) expected error, got %+v", tt.input, got)
			}
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("error = %v, want it to wrap %v", err, tt.wantErr)
			}
			if tt.wantMsg != "" && !strings.Contains(err.Error(), tt.wantMsg) {
				t.Errorf("error %q does not mention %q", err.Error(), tt.wantMsg)
			}
			// A failed parse must never hand back a half-built query.
			if !reflect.DeepEqual(got, parsedQuery{}) {
				t.Errorf("expected zero parsedQuery on error, got %+v", got)
			}
		})
	}
}

// Byte-wise scanning must leave multi-byte UTF-8 text untouched.
func TestSplitSegmentsPreservesNonASCII(t *testing.T) {
	segs, err := splitSegments(`日本語 "golang search" naïve`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(segs) != 3 {
		t.Fatalf("got %d segments, want 3: %+v", len(segs), segs)
	}
	if got := strings.TrimSpace(segs[0].text); got != "日本語" || segs[0].quoted {
		t.Errorf("segment 0 = %+v", segs[0])
	}
	if segs[1].text != "golang search" || !segs[1].quoted {
		t.Errorf("segment 1 = %+v", segs[1])
	}
	if got := strings.TrimSpace(segs[2].text); got != "naïve" || segs[2].quoted {
		t.Errorf("segment 2 = %+v", segs[2])
	}
}

func TestParsedQueryHelpers(t *testing.T) {
	q, err := parseQuery(`"search the golang" tutorial`)
	if err != nil {
		t.Fatal(err)
	}
	if !q.hasPhrases() {
		t.Error("hasPhrases() = false, want true")
	}
	want := []string{"search", "golang", "tutorial"}
	if got := q.words(); !reflect.DeepEqual(got, want) {
		t.Errorf("words() = %v, want %v", got, want)
	}

	bare, _ := parseQuery("golang")
	if bare.hasPhrases() {
		t.Error("hasPhrases() = true for a bare query")
	}
}
