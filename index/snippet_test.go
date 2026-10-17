package index

import (
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/my-search-index/search-index/analysis"
)

// marked returns the text each highlight range of a snippet points at.
func marked(s Snippet) []string {
	var out []string
	for _, m := range s.Matches {
		out = append(out, s.Text[m.Start:m.End])
	}
	return out
}

func TestTokenSpans(t *testing.T) {
	tests := []struct {
		name  string
		line  string
		words []string
	}{
		{"empty line", "", nil},
		{"only whitespace", "   \t ", nil},
		{"only punctuation", "--- ... ???", nil},
		{"single word", "golang", []string{"golang"}},
		{"words and punctuation", "golang search, engines!", []string{"golang", "search", "engines"}},
		{"separators inside a word", "a-b_c.d", []string{"a", "b", "c", "d"}},
		{"digits are token characters", "v2 go1", []string{"v2", "go1"}},
		{"leading and trailing space", "  padded  ", []string{"padded"}},
		{"keeps original case", "GoLang", []string{"GoLang"}},
		{"non-ASCII letters separate tokens", "naïve café", []string{"na", "ve", "caf"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spans := tokenSpans(tt.line)

			var words []string
			for _, span := range spans {
				words = append(words, tt.line[span.start:span.end])
			}

			if !reflect.DeepEqual(words, tt.words) {
				t.Errorf("tokenSpans(%q) words = %q, want %q", tt.line, words, tt.words)
			}
		})
	}
}

// Token i of a line is document position offset+i, so tokenSpans must agree
// with analysis.Tokenize on how many tokens a line has and what they are.
// If the tokenizer's split rule ever changes, this test fails first.
func TestTokenSpansMatchTokenizer(t *testing.T) {
	lines := []string{
		"",
		"   ",
		"golang",
		"golang search, engines!",
		"golang-search_engines.v2",
		"naïve café 日本語 golang",
		"A1 b2   c3\tTab",
		"--- ... ???",
		`quote "inside" text`,
		"trailing token",
		"\xff\xfe invalid bytes golang",
	}

	for _, line := range lines {
		spans := tokenSpans(line)
		tokens := analysis.Tokenize(line)

		if len(spans) != len(tokens) {
			t.Errorf("line %q: %d spans vs %d tokens", line, len(spans), len(tokens))
			continue
		}

		for i, span := range spans {
			if got := line[span.start:span.end]; got != tokens[i].Word {
				t.Errorf("line %q token %d: span %q vs tokenizer %q", line, i, got, tokens[i].Word)
			}
		}
	}
}

func TestTargetPositions(t *testing.T) {
	t.Run("no postings", func(t *testing.T) {
		targets, last := targetPositions(nil)

		if len(targets) != 0 {
			t.Errorf("expected no targets, got %v", targets)
		}
		if last != -1 {
			t.Errorf("expected last = -1, got %d", last)
		}
	})

	t.Run("postings without positions", func(t *testing.T) {
		targets, last := targetPositions([]Posting{{DocID: 1}})

		if len(targets) != 0 || last != -1 {
			t.Errorf("expected nothing, got %v and %d", targets, last)
		}
	})

	t.Run("merges postings, deduplicates, reports the largest", func(t *testing.T) {
		targets, last := targetPositions([]Posting{
			{DocID: 1, Positions: []int{3, 1}},
			{DocID: 1, Positions: []int{7, 3}},
		})

		want := map[int]struct{}{1: {}, 3: {}, 7: {}}
		if !reflect.DeepEqual(targets, want) {
			t.Errorf("targets = %v, want %v", targets, want)
		}
		if last != 7 {
			t.Errorf("last = %d, want 7", last)
		}
	})
}

func TestSnippetWindow(t *testing.T) {
	long := strings.Repeat("a", 300)

	tests := []struct {
		name      string
		line      string
		match     Match
		wantStart int
		wantEnd   int
	}{
		{"short line is kept whole", "golang is fast", Match{Start: 0, End: 6}, 0, 14},
		{"context on both sides", long, Match{Start: 100, End: 106}, 100 - snippetContext, 106 + snippetContext},
		{"clamped at the start", long, Match{Start: 5, End: 11}, 0, 11 + snippetContext},
		{"clamped at the end", long[:100], Match{Start: 90, End: 96}, 90 - snippetContext, 100},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start, end := snippetWindow(tt.line, tt.match)

			if start != tt.wantStart || end != tt.wantEnd {
				t.Errorf("snippetWindow() = (%d, %d), want (%d, %d)", start, end, tt.wantStart, tt.wantEnd)
			}
		})
	}

	t.Run("edges never split a multi-byte character", func(t *testing.T) {
		// "日" is 3 bytes. The match starts at byte 122 and ends at 128, so
		// the raw window edges (62 and 188) land inside a character.
		line := strings.Repeat("日", 40) + "x golang x" + strings.Repeat("日", 40)
		match := Match{Start: strings.Index(line, "golang")}
		match.End = match.Start + len("golang")

		start, end := snippetWindow(line, match)

		if !utf8.RuneStart(line[start]) {
			t.Errorf("window start %d splits a character", start)
		}
		if end < len(line) && !utf8.RuneStart(line[end]) {
			t.Errorf("window end %d splits a character", end)
		}
		if start > match.Start-snippetContext || end < match.End+snippetContext {
			t.Errorf("window (%d, %d) shrank instead of moving outward", start, end)
		}
		if !utf8.ValidString(line[start:end]) {
			t.Errorf("window text is not valid UTF-8")
		}
	})
}

func TestBuildSnippet(t *testing.T) {
	t.Run("highlight ranges are relative to the window", func(t *testing.T) {
		line := strings.Repeat("a", 100) + " golang " + strings.Repeat("b", 100)
		first := strings.Index(line, "golang")
		m := Match{Start: first, End: first + 6, Term: "golang"}

		s := buildSnippet(line, []Match{m})

		wantStart := first - snippetContext
		if s.Text != line[wantStart:first+6+snippetContext] {
			t.Fatalf("unexpected window text: %q", s.Text)
		}
		if len(s.Matches) != 1 || s.Text[s.Matches[0].Start:s.Matches[0].End] != "golang" {
			t.Errorf("matches = %+v do not point at the term in %q", s.Matches, s.Text)
		}
		if s.Matches[0].Term != "golang" {
			t.Errorf("term = %q", s.Matches[0].Term)
		}
	})

	t.Run("matches outside the window are dropped", func(t *testing.T) {
		line := "golang " + strings.Repeat("x", 300) + " golang"
		second := strings.LastIndex(line, "golang")

		s := buildSnippet(line, []Match{
			{Start: 0, End: 6, Term: "golang"},
			{Start: second, End: second + 6, Term: "golang"},
		})

		if len(s.Matches) != 1 {
			t.Errorf("expected only the first match to fit the window, got %+v", s.Matches)
		}
	})

	t.Run("several matches inside one window are all kept", func(t *testing.T) {
		line := "golang loves search and search loves golang"
		var matches []Match
		for _, tt := range tokenSpans(line) {
			word := line[tt.start:tt.end]
			if word == "golang" || word == "search" {
				matches = append(matches, Match{Start: tt.start, End: tt.end, Term: word})
			}
		}

		s := buildSnippet(line, matches)

		want := []string{"golang", "search", "search", "golang"}
		if got := marked(s); !reflect.DeepEqual(got, want) {
			t.Errorf("marked = %q, want %q", got, want)
		}
	})
}

// extractSnippets takes positions straight from postings, so it can be tested
// without building an index.
func TestExtractSnippets(t *testing.T) {
	// Raw token positions:
	//   line 1: alpha=0 beta=1 gamma=2
	//   line 2: delta=3 epsilon=4
	//   line 3: zeta=5 eta=6 theta=7
	//   line 4: iota=8
	path := makeTempFile(t, "alpha beta gamma\ndelta epsilon\nzeta eta theta\niota")
	doc := Document{ID: 1, FilePath: path}

	tests := []struct {
		name     string
		postings []Posting
		want     []Snippet
	}{
		{
			name:     "no postings",
			postings: nil,
			want:     nil,
		},
		{
			name:     "postings without positions",
			postings: []Posting{{DocID: 1}},
			want:     nil,
		},
		{
			name:     "single match",
			postings: []Posting{{DocID: 1, Positions: []int{1}}},
			want: []Snippet{
				{Text: "alpha beta gamma", Matches: []Match{{Start: 6, End: 10, Term: "beta"}}},
			},
		},
		{
			name:     "two matches on one line give one snippet",
			postings: []Posting{{DocID: 1, Positions: []int{0, 2}}},
			want: []Snippet{
				{Text: "alpha beta gamma", Matches: []Match{
					{Start: 0, End: 5, Term: "alpha"},
					{Start: 11, End: 16, Term: "gamma"},
				}},
			},
		},
		{
			name:     "matches on different lines give one snippet each",
			postings: []Posting{{DocID: 1, Positions: []int{4, 6}}},
			want: []Snippet{
				{Text: "delta epsilon", Matches: []Match{{Start: 6, End: 13, Term: "epsilon"}}},
				{Text: "zeta eta theta", Matches: []Match{{Start: 5, End: 8, Term: "eta"}}},
			},
		},
		{
			name:     "match on the last line",
			postings: []Posting{{DocID: 1, Positions: []int{8}}},
			want: []Snippet{
				{Text: "iota", Matches: []Match{{Start: 0, End: 4, Term: "iota"}}},
			},
		},
		{
			name:     "first and last position",
			postings: []Posting{{DocID: 1, Positions: []int{0, 8}}},
			want: []Snippet{
				{Text: "alpha beta gamma", Matches: []Match{{Start: 0, End: 5, Term: "alpha"}}},
				{Text: "iota", Matches: []Match{{Start: 0, End: 4, Term: "iota"}}},
			},
		},
		{
			name: "positions from several postings are merged",
			postings: []Posting{
				{DocID: 1, Positions: []int{1}},
				{DocID: 1, Positions: []int{5}},
			},
			want: []Snippet{
				{Text: "alpha beta gamma", Matches: []Match{{Start: 6, End: 10, Term: "beta"}}},
				{Text: "zeta eta theta", Matches: []Match{{Start: 0, End: 4, Term: "zeta"}}},
			},
		},
		{
			name:     "position past the end of the file",
			postings: []Posting{{DocID: 1, Positions: []int{99}}},
			want:     nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractSnippets(doc, tt.postings)

			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("extractSnippets()\n got: %+v\nwant: %+v", got, tt.want)
			}
		})
	}
}

// Stopwords are not indexed but still occupy a position, so line accounting
// has to count them.
func TestExtractSnippets_StopwordsAndBlankLines(t *testing.T) {
	// the=0 golang=1 | and=2 search=3 | (blank) | - - - | tail=4
	path := makeTempFile(t, "the golang\nand search\n\n- - -\ntail")
	doc := Document{ID: 1, FilePath: path}

	got := extractSnippets(doc, []Posting{
		{DocID: 1, Positions: []int{1}},
		{DocID: 1, Positions: []int{3, 4}},
	})

	want := []Snippet{
		{Text: "the golang", Matches: []Match{{Start: 4, End: 10, Term: "golang"}}},
		{Text: "and search", Matches: []Match{{Start: 4, End: 10, Term: "search"}}},
		{Text: "tail", Matches: []Match{{Start: 0, End: 4, Term: "tail"}}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("extractSnippets()\n got: %+v\nwant: %+v", got, want)
	}
}

func TestExtractSnippets_TermIsLowercased(t *testing.T) {
	path := makeTempFile(t, "Use GoLang daily")

	got := extractSnippets(Document{ID: 1, FilePath: path}, []Posting{{DocID: 1, Positions: []int{1}}})

	if len(got) != 1 || len(got[0].Matches) != 1 {
		t.Fatalf("unexpected snippets: %+v", got)
	}
	m := got[0].Matches[0]
	if got[0].Text[m.Start:m.End] != "GoLang" || m.Term != "golang" {
		t.Errorf("highlight should keep the original text and report the normalized term, got %+v", m)
	}
}

func TestExtractSnippets_MissingFile(t *testing.T) {
	doc := Document{ID: 1, FilePath: "does-not-exist.txt"}

	if got := extractSnippets(doc, []Posting{{DocID: 1, Positions: []int{0}}}); got != nil {
		t.Errorf("expected no snippets for an unreadable file, got %+v", got)
	}
}

// The tests below go through Search, so they also cover how ranking hands
// its postings to snippet extraction.
func TestSearchSnippets(t *testing.T) {

	t.Run("snippets include match ranges", func(t *testing.T) {
		idx := NewIndex()

		path := makeTempFile(t, "first line\nthis line mentions golang\nlast line")
		_ = idx.AddDocument(path)

		results := mustSearch(t, idx, "golang")

		if len(results) != 1 {
			t.Fatalf("expected 1 result, got %d", len(results))
		}

		want := "this line mentions golang"
		if len(results[0].Snippets) != 1 || results[0].Snippets[0].Text != want {
			t.Fatalf("expected snippet %q, got %v", want, results[0].Snippets)
		}

		matches := results[0].Snippets[0].Matches
		if len(matches) != 1 {
			t.Fatalf("expected 1 match, got %v", matches)
		}

		match := matches[0]
		if got := results[0].Snippets[0].Text[match.Start:match.End]; got != "golang" {
			t.Fatalf("expected match to select golang, got %q", got)
		}
	})

	t.Run("snippets highlight each query term", func(t *testing.T) {
		idx := NewIndex()

		path := makeTempFile(t, "a search engine runs on a distributed computing system")
		_ = idx.AddDocument(path)

		results := mustSearch(t, idx, "distributed computing")

		if len(results) != 1 {
			t.Fatalf("expected 1 result, got %d", len(results))
		}

		matches := results[0].Snippets[0].Matches
		if len(matches) != 2 {
			t.Fatalf("expected 2 matches, got %v", matches)
		}

		text := results[0].Snippets[0].Text
		if got := text[matches[0].Start:matches[0].End]; got != "distributed" {
			t.Fatalf("expected first match to select distributed, got %q", got)
		}
		if got := text[matches[1].Start:matches[1].End]; got != "computing" {
			t.Fatalf("expected second match to select computing, got %q", got)
		}
	})

	t.Run("every occurrence in a line is highlighted", func(t *testing.T) {
		idx := NewIndex()

		_ = idx.AddDocument(makeTempFile(t, "golang loves GOLANG and golang"))

		results := mustSearch(t, idx, "golang")

		snippets := results[0].Snippets
		if len(snippets) != 1 {
			t.Fatalf("expected 1 snippet, got %d", len(snippets))
		}

		want := []string{"golang", "GOLANG", "golang"}
		if got := marked(snippets[0]); !reflect.DeepEqual(got, want) {
			t.Errorf("marked = %q, want %q", got, want)
		}
	})

	t.Run("a longer word containing the term is not highlighted", func(t *testing.T) {
		idx := NewIndex()

		_ = idx.AddDocument(makeTempFile(t, "golang and golangs and agolang"))

		results := mustSearch(t, idx, "golang")

		want := []string{"golang"}
		if got := marked(results[0].Snippets[0]); !reflect.DeepEqual(got, want) {
			t.Errorf("marked = %q, want %q", got, want)
		}
	})

	t.Run("stopwords in the query are not highlighted", func(t *testing.T) {
		idx := NewIndex()

		_ = idx.AddDocument(makeTempFile(t, "the golang and the search"))

		results := mustSearch(t, idx, "the golang")

		want := []string{"golang"}
		if got := marked(results[0].Snippets[0]); !reflect.DeepEqual(got, want) {
			t.Errorf("marked = %q, want %q", got, want)
		}
	})

	t.Run("only lines with a match produce snippets", func(t *testing.T) {
		idx := NewIndex()

		content := strings.Join([]string{
			"the of and, a an!",
			"",
			"first mention of golang here",
			"- - -",
			"filler words between",
			"and then golang again",
			"tail",
		}, "\n")
		_ = idx.AddDocument(makeTempFile(t, content))

		results := mustSearch(t, idx, "golang")

		snippets := results[0].Snippets
		if len(snippets) != 2 {
			t.Fatalf("expected 2 snippets, got %d: %+v", len(snippets), snippets)
		}
		if !strings.Contains(snippets[0].Text, "first mention") || !strings.Contains(snippets[1].Text, "then golang again") {
			t.Errorf("snippets are on the wrong lines: %+v", snippets)
		}
	})

	t.Run("each result only gets snippets from its own file", func(t *testing.T) {
		idx := NewIndex()

		_ = idx.AddDocument(makeTempFile(t, "golang in the first file"))
		_ = idx.AddDocument(makeTempFile(t, "no relevant words\nbut golang on line two"))

		results := mustSearch(t, idx, "golang")

		if len(results) != 2 {
			t.Fatalf("expected 2 results, got %d", len(results))
		}
		for _, r := range results {
			if len(r.Snippets) != 1 {
				t.Fatalf("doc %d: expected 1 snippet, got %+v", r.Doc.ID, r.Snippets)
			}
			want := "golang in the first file"
			if r.Doc.ID == 2 {
				want = "but golang on line two"
			}
			if r.Snippets[0].Text != want {
				t.Errorf("doc %d snippet = %q, want %q", r.Doc.ID, r.Snippets[0].Text, want)
			}
		}
	})

	t.Run("only the top results get snippets", func(t *testing.T) {
		idx := NewIndex()

		total := snippetResultLimit + 2
		for i := 0; i < total; i++ {
			_ = idx.AddDocument(makeTempFile(t, fmt.Sprintf("golang appears in document %d", i)))
		}

		results := mustSearch(t, idx, "golang")

		if len(results) != total {
			t.Fatalf("expected %d results (the limit must not drop results), got %d", total, len(results))
		}
		for i, r := range results {
			hasSnippets := len(r.Snippets) > 0
			if i < snippetResultLimit && !hasSnippets {
				t.Errorf("result %d should have snippets", i)
			}
			if i >= snippetResultLimit && hasSnippets {
				t.Errorf("result %d is past the limit and should not have snippets", i)
			}
		}
	})

	t.Run("a result whose file disappeared is still returned without snippets", func(t *testing.T) {
		idx := NewIndex()

		path := makeTempFile(t, "golang is fast")
		_ = idx.AddDocument(path)
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}

		results := mustSearch(t, idx, "golang")

		if len(results) != 1 {
			t.Fatalf("expected 1 result, got %d", len(results))
		}
		if len(results[0].Snippets) != 0 {
			t.Errorf("expected no snippets, got %+v", results[0].Snippets)
		}
	})

	t.Run("snippet text stays valid UTF-8", func(t *testing.T) {
		idx := NewIndex()

		line := strings.Repeat("日", 40) + "x golang x" + strings.Repeat("日", 40)
		_ = idx.AddDocument(makeTempFile(t, line))

		results := mustSearch(t, idx, "golang")

		s := results[0].Snippets[0]
		if !utf8.ValidString(s.Text) {
			t.Errorf("snippet is not valid UTF-8: %q", s.Text)
		}
		if got := marked(s); !reflect.DeepEqual(got, []string{"golang"}) {
			t.Errorf("marked = %q, want [golang]", got)
		}
	})
}
