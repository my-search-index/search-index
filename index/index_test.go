package index

import (
	"errors"
	"math"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// mustSearch runs a query that is expected to parse and fails the test if
// Search returns an error. Tests that expect an error call idx.Search directly.
func mustSearch(t *testing.T, idx *Index, query string) []Result {
	t.Helper()

	results, err := idx.Search(query)
	if err != nil {
		t.Fatalf("Search(%q) unexpected error: %v", query, err)
	}
	return results
}

// addAll indexes the given contents in order, so document IDs are 1..n.
func addAll(t *testing.T, contents ...string) *Index {
	t.Helper()

	idx := NewIndex()
	for _, content := range contents {
		if err := idx.AddDocument(makeTempFile(t, content)); err != nil {
			t.Fatalf("AddDocument: %v", err)
		}
	}
	return idx
}

// docIDs returns the sorted document IDs of results, for order-insensitive
// comparisons.
func docIDs(results []Result) []int {
	ids := make([]int, 0, len(results))
	for _, r := range results {
		ids = append(ids, r.Doc.ID)
	}
	sort.Ints(ids)
	return ids
}

func TestAddDocument_FileAccess(t *testing.T) {
	idx := NewIndex()

	t.Run("file does not exist", func(t *testing.T) {
		err := idx.AddDocument("non-existent-file.txt")
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("empty file", func(t *testing.T) {
		path := makeTempFile(t, "")
		err := idx.AddDocument(path)

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		doc := idx.Docs[1]
		if doc.Length != 0 {
			t.Errorf("expected length 0, got %d", doc.Length)
		}

		if len(idx.Postings) != 0 {
			t.Errorf("expected no postings, got %v", idx.Postings)
		}
	})

	t.Run("only delimiters", func(t *testing.T) {
		path := makeTempFile(t, "   \n!!!,,,\n")
		err := idx.AddDocument(path)

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		doc := idx.Docs[2]
		if doc.Length != 0 {
			t.Errorf("expected length 0, got %d", doc.Length)
		}
	})
}

func TestAddDocument_Postings(t *testing.T) {
	t.Run("single term single document", func(t *testing.T) {
		idx := NewIndex()
		path := makeTempFile(t, "golang")

		_ = idx.AddDocument(path)

		postings := idx.Postings["golang"]

		if len(postings) != 1 {
			t.Fatalf("expected 1 posting, got %d", len(postings))
		}

		p := postings[0]
		if p.DocID != 1 || p.Frequency != 1 || len(p.Positions) != 1 || p.Positions[0] != 0 {
			t.Errorf("unexpected posting: %+v", p)
		}
	})

	t.Run("same term multiple times", func(t *testing.T) {
		idx := NewIndex()
		path := makeTempFile(t, "golang is golang")

		_ = idx.AddDocument(path)

		postings := idx.Postings["golang"]
		p := postings[0]

		if p.Frequency != 2 {
			t.Errorf("expected frequency 2, got %d", p.Frequency)
		}

		expected := []int{0, 2}
		if len(p.Positions) != 2 || p.Positions[0] != expected[0] || p.Positions[1] != expected[1] {
			t.Errorf("expected positions %v, got %v", expected, p.Positions)
		}
	})

	t.Run("multiple distinct terms", func(t *testing.T) {
		idx := NewIndex()
		path := makeTempFile(t, "go fast")

		_ = idx.AddDocument(path)

		if len(idx.Postings) != 2 {
			t.Errorf("expected 2 terms, got %d", len(idx.Postings))
		}
	})

	t.Run("terms across multiple lines", func(t *testing.T) {
		idx := NewIndex()
		path := makeTempFile(t, "golang\nsearch")

		_ = idx.AddDocument(path)

		if _, ok := idx.Postings["golang"]; !ok {
			t.Errorf("missing term golang")
		}

		if _, ok := idx.Postings["search"]; !ok {
			t.Errorf("missing term search")
		}
	})

	t.Run("positions continue across lines", func(t *testing.T) {
		idx := NewIndex()
		path := makeTempFile(t, "the golang\nand search")

		_ = idx.AddDocument(path)

		if got := idx.Postings["golang"][0].Positions[0]; got != 1 {
			t.Errorf("expected golang at position 1, got %d", got)
		}

		if got := idx.Postings["search"][0].Positions[0]; got != 3 {
			t.Errorf("expected search at position 3, got %d", got)
		}
	})
}

func TestAddDocument_MultipleDocs(t *testing.T) {
	idx := NewIndex()

	path1 := makeTempFile(t, "golang")
	path2 := makeTempFile(t, "golang")

	_ = idx.AddDocument(path1)
	_ = idx.AddDocument(path2)

	postings := idx.Postings["golang"]

	if len(postings) != 2 {
		t.Fatalf("expected 2 postings, got %d", len(postings))
	}

	if postings[0].DocID != 1 || postings[1].DocID != 2 {
		t.Errorf("docIDs incorrect: %+v", postings)
	}
}

func TestAddDocument_DocStore(t *testing.T) {
	idx := NewIndex()

	path := makeTempFile(t, "golang is fast")
	_ = idx.AddDocument(path)

	doc := idx.Docs[1]

	if doc.FilePath != path {
		t.Errorf("expected filepath %s, got %s", path, doc.FilePath)
	}

	if doc.Length != 2 {
		t.Errorf("expected length 2, got %d", doc.Length)
	}

	if idx.NextDocID != 2 {
		t.Errorf("expected nextDocID 2, got %d", idx.NextDocID)
	}

	if idx.TotalDocs != 1 {
		t.Errorf("expected totalDocs 1, got %d", idx.TotalDocs)
	}
}

func TestRemoveDocument(t *testing.T) {
	t.Run("removes doc, postings, and unique terms", func(t *testing.T) {
		idx := NewIndex()

		path1 := makeTempFile(t, "golang fast")
		path2 := makeTempFile(t, "python slow")
		_ = idx.AddDocument(path1)
		_ = idx.AddDocument(path2)

		err := idx.RemoveDocument(path1)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if _, ok := idx.Docs[1]; ok {
			t.Fatalf("expected doc 1 to be removed")
		}

		if idx.TotalDocs != 1 {
			t.Fatalf("expected TotalDocs 1, got %d", idx.TotalDocs)
		}

		if idx.NextDocID != 3 {
			t.Fatalf("expected NextDocID to remain 3, got %d", idx.NextDocID)
		}

		if _, ok := idx.Postings["golang"]; ok {
			t.Fatalf("expected unique term golang to be removed")
		}

		results := mustSearch(t, idx, "golang python")
		if len(results) != 1 {
			t.Fatalf("expected 1 result after removal, got %d", len(results))
		}

		if results[0].Doc.FilePath != path2 {
			t.Fatalf("expected remaining result %s, got %s", path2, results[0].Doc.FilePath)
		}
	})

	t.Run("removes only target doc from shared posting lists", func(t *testing.T) {
		idx := NewIndex()

		path1 := makeTempFile(t, "golang fast")
		path2 := makeTempFile(t, "golang slow")
		_ = idx.AddDocument(path1)
		_ = idx.AddDocument(path2)

		err := idx.RemoveDocument(path1)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		postings := idx.Postings["golang"]
		if len(postings) != 1 {
			t.Fatalf("expected 1 golang posting, got %d", len(postings))
		}

		if postings[0].DocID != 2 {
			t.Fatalf("expected remaining posting for doc 2, got doc %d", postings[0].DocID)
		}

		results := mustSearch(t, idx, "golang")
		if len(results) != 1 || results[0].Doc.ID != 2 {
			t.Fatalf("expected only doc 2 in search results, got %v", results)
		}
	})

	t.Run("returns error when path is not indexed", func(t *testing.T) {
		idx := NewIndex()

		err := idx.RemoveDocument("missing.txt")
		if err == nil {
			t.Fatalf("expected error, got nil")
		}
	})
}

func TestLoad_BackfillsTotalDocs(t *testing.T) {
	path := makeTempFile(t, "")
	idx := NewIndex()

	docPath := makeTempFile(t, "golang")
	_ = idx.AddDocument(docPath)
	idx.TotalDocs = 0

	if err := idx.Save(path); err != nil {
		t.Fatalf("unexpected save error: %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected load error: %v", err)
	}

	if loaded.TotalDocs != 1 {
		t.Fatalf("expected TotalDocs 1, got %d", loaded.TotalDocs)
	}
}

func TestListDocuments(t *testing.T) {
	idx := NewIndex()
	idx.Docs[3] = Document{ID: 3, FilePath: "../web-crawler", Length: 3797}
	idx.Docs[1] = Document{ID: 1, FilePath: "../information-retrieval", Length: 2834}
	idx.Docs[2] = Document{ID: 2, FilePath: "../search-engines", Length: 1598}

	documents := idx.ListDocuments()

	if len(documents) != 3 {
		t.Fatalf("expected 3 documents, got %d", len(documents))
	}

	for i, doc := range documents {
		wantID := i + 1
		if doc.ID != wantID {
			t.Fatalf("expected doc ID %d at index %d, got %d", wantID, i, doc.ID)
		}
	}
}

func TestSearch(t *testing.T) {

	t.Run("single term query", func(t *testing.T) {
		idx := NewIndex()

		path := makeTempFile(t, "golang is fast")
		_ = idx.AddDocument(path)

		results := mustSearch(t, idx, "golang")

		if len(results) != 1 {
			t.Fatalf("expected 1 result, got %d", len(results))
		}

		if results[0].Doc.ID != 1 {
			t.Errorf("expected docID 1, got %d", results[0].Doc.ID)
		}
	})

	t.Run("two term query both exist", func(t *testing.T) {
		idx := NewIndex()

		path := makeTempFile(t, "golang is fast")
		_ = idx.AddDocument(path)

		results := mustSearch(t, idx, "golang fast")

		if len(results) != 1 {
			t.Fatalf("expected 1 result, got %d", len(results))
		}

		if results[0].Doc.ID != 1 {
			t.Errorf("expected docID 1, got %d", results[0].Doc.ID)
		}
	})

	t.Run("two term query one missing", func(t *testing.T) {
		idx := NewIndex()

		path := makeTempFile(t, "golang is fast")
		_ = idx.AddDocument(path)

		results := mustSearch(t, idx, "golang python")

		if len(results) != 1 {
			t.Errorf("expected 1 result, got %v", results)
		}
	})

	t.Run("unknown term does not produce NaN scores", func(t *testing.T) {
		idx := NewIndex()

		path := makeTempFile(t, "golang is fast")
		_ = idx.AddDocument(path)

		results := mustSearch(t, idx, "golang zzzunknown")

		if len(results) != 1 {
			t.Fatalf("expected 1 result, got %d", len(results))
		}

		if math.IsNaN(results[0].Score) || math.IsInf(results[0].Score, 0) {
			t.Errorf("expected a finite score, got %v", results[0].Score)
		}
	})

	t.Run("empty query", func(t *testing.T) {
		idx := NewIndex()

		path := makeTempFile(t, "golang is fast")
		_ = idx.AddDocument(path)

		results := mustSearch(t, idx, "")

		if len(results) != 0 {
			t.Errorf("expected empty result, got %v", results)
		}
	})

	t.Run("stopword-only query returns no results and no error", func(t *testing.T) {
		idx := NewIndex()

		path := makeTempFile(t, "golang is fast")
		_ = idx.AddDocument(path)

		results := mustSearch(t, idx, "the of and")

		if len(results) != 0 {
			t.Errorf("expected empty result, got %v", results)
		}
	})

	t.Run("unterminated quote returns an error", func(t *testing.T) {
		idx := NewIndex()

		path := makeTempFile(t, "golang is fast")
		_ = idx.AddDocument(path)

		results, err := idx.Search(`"golang fast`)

		if !errors.Is(err, ErrUnterminatedQuote) {
			t.Fatalf("expected ErrUnterminatedQuote, got %v", err)
		}

		if results != nil {
			t.Errorf("expected nil results on error, got %v", results)
		}
	})

	t.Run("empty phrase returns an error", func(t *testing.T) {
		idx := NewIndex()

		path := makeTempFile(t, "golang is fast")
		_ = idx.AddDocument(path)

		for _, query := range []string{`""`, `"the of"`, `golang ""`} {
			results, err := idx.Search(query)

			if !errors.Is(err, ErrEmptyPhrase) {
				t.Errorf("Search(%q) expected ErrEmptyPhrase, got %v", query, err)
			}

			if results != nil {
				t.Errorf("Search(%q) expected nil results on error, got %v", query, results)
			}
		}
	})

}

// TestPhraseSearch checks WHICH documents a query returns.
func TestPhraseSearch(t *testing.T) {
	tests := []struct {
		name  string
		docs  []string
		query string
		want  []int
	}{
		{"adjacent words", []string{"golang search", "golang quick search", "search golang"}, `"golang search"`, []int{1}},
		{"reversed order does not match", []string{"golang search", "search golang"}, `"search golang"`, []int{2}},
		{"stopword gap", []string{"search the golang", "search a golang", "search golang"}, `"search the golang"`, []int{1, 2}},
		{"any single word fills the gap slot", []string{"search the golang", "search a golang", "search golang", "search big golang"}, `"search the golang"`, []int{1, 2, 4}},
		{"repeated word", []string{"go go", "go fast go"}, `"go go"`, []int{1}},
		{"case and punctuation are ignored", []string{"GoLang Search rocks", "golang-search tools", "golang, search!", "golang x search"}, `"golang search"`, []int{1, 2, 3}},
		{"multiple phrases", []string{"red fox and blue whale", "red fox", "blue whale"}, `"red fox" "blue whale"`, []int{1}},
		{"later documents still match after an earlier one fails", []string{"red fox only", "blue whale only", "red fox and blue whale", "red fox and blue whale again"}, `"red fox" "blue whale"`, []int{3, 4}},
		{"bare term only boosts", []string{"golang search tutorial", "golang search", "tutorial"}, `"golang search" tutorial`, []int{1, 2}},
		{"bare terms never add candidates", []string{"golang search fundamentals", "golang search tutorial", "tutorial only here", "golang x search tutorial"}, `"golang search" tutorial`, []int{1, 2}},
		{"single word phrase is a required term", []string{"golang", "python"}, `"golang" python`, []int{1}},
		{"unknown phrase word", []string{"golang search"}, `"golang missing"`, nil},
		{"unknown phrase word is not rescued by a bare term", []string{"golang tutorial", "tutorial only"}, `"golang zzzunknown" tutorial`, nil},
		{"no document has the phrase: no fallback to separate words", []string{"golang is great", "search is fast"}, `"golang search"`, nil},
		{"across lines", []string{"golang\nsearch"}, `"golang search"`, []int{1}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			idx := NewIndex()
			for _, content := range tt.docs {
				if err := idx.AddDocument(makeTempFile(t, content)); err != nil {
					t.Fatal(err)
				}
			}

			results := mustSearch(t, idx, tt.query)
			if len(results) != len(tt.want) {
				t.Fatalf("got %d results, want %d: %+v", len(results), len(tt.want), results)
			}
			wanted := make(map[int]bool, len(tt.want))
			for _, id := range tt.want {
				wanted[id] = true
			}
			for _, result := range results {
				if !wanted[result.Doc.ID] {
					t.Errorf("unexpected document %d", result.Doc.ID)
				}
			}
		})
	}
}

// TestPhraseRanking checks the ORDER and SCORES of phrase results.
func TestPhraseRanking(t *testing.T) {

	t.Run("a bare term lifts a document that already matches the phrase", func(t *testing.T) {
		idx := addAll(t,
			"golang search fundamentals", // 1: phrase
			"golang search tutorial",     // 2: phrase + bare term
			"tutorial only here",         // 3: bare term without the phrase
			"golang x search tutorial",   // 4: bare term, phrase split
		)

		results := mustSearch(t, idx, `"golang search" tutorial`)

		if len(results) != 2 {
			t.Fatalf("expected 2 results, got %v", docIDs(results))
		}
		if results[0].Doc.ID != 2 || results[1].Doc.ID != 1 {
			t.Errorf("order = [%d %d], want [2 1]: the bare term should lift doc 2",
				results[0].Doc.ID, results[1].Doc.ID)
		}
		if results[0].Score <= results[1].Score {
			t.Errorf("expected strictly higher score for doc 2, got %v then %v", results[0].Score, results[1].Score)
		}
	})

	t.Run("more occurrences of the phrase rank higher", func(t *testing.T) {
		// Docs 1 and 2 have the same length and the same word frequencies, so
		// only the number of phrase occurrences differs. Doc 3 keeps IDF above 0.
		// Doc 1 is first so a correct ranking has to move doc 2 ahead of it.
		idx := addAll(t,
			"golang search golang pad search pad",    // 1: phrase once
			"golang search golang search filler pad", // 2: phrase twice
			"python rocks",                           // 3
		)

		results := mustSearch(t, idx, `"golang search"`)

		if len(results) != 2 {
			t.Fatalf("expected 2 results, got %v", docIDs(results))
		}
		if results[0].Doc.ID != 2 {
			t.Errorf("doc 2 (phrase twice) should rank first, got doc %d", results[0].Doc.ID)
		}
		if results[0].Score <= results[1].Score {
			t.Errorf("expected strictly higher score, got %v then %v", results[0].Score, results[1].Score)
		}
	})

	t.Run("a rarer phrase scores higher than a common one", func(t *testing.T) {
		idx := addAll(t,
			"common words rare pair here",
			"common words only",
			"common words again",
			"python rocks",
		)

		rare := mustSearch(t, idx, `"rare pair"`)
		common := mustSearch(t, idx, `"common words"`)

		if len(rare) != 1 || len(common) != 3 {
			t.Fatalf("unexpected result sizes: %d rare, %d common", len(rare), len(common))
		}
		if rare[0].Score <= common[0].Score {
			t.Errorf("rare phrase score %v should exceed common phrase score %v", rare[0].Score, common[0].Score)
		}
	})

	t.Run("scores stay finite when the phrase is in every document", func(t *testing.T) {
		idx := addAll(t, "golang search", "golang search again")

		results := mustSearch(t, idx, `"golang search"`)

		if len(results) != 2 {
			t.Fatalf("expected 2 results, got %d", len(results))
		}
		for _, r := range results {
			if math.IsNaN(r.Score) || math.IsInf(r.Score, 0) {
				t.Errorf("doc %d has a non-finite score %v", r.Doc.ID, r.Score)
			}
		}
	})
}

// TestPhraseSnippets checks that snippets point at the phrase and nowhere else.
func TestPhraseSnippets(t *testing.T) {

	t.Run("only phrase occurrences are highlighted", func(t *testing.T) {
		// The first line holds the document's very first token. A stray
		// position 0 in the phrase positions would wrongly highlight it.
		idx := addAll(t, "golang alone here\nthe golang search phrase\nsearch alone")

		results := mustSearch(t, idx, `"golang search"`)

		if len(results) != 1 {
			t.Fatalf("expected 1 result, got %d", len(results))
		}
		snippets := results[0].Snippets
		if len(snippets) != 1 {
			t.Fatalf("expected a snippet only for the phrase line, got %+v", snippets)
		}
		if got := marked(snippets[0]); !reflect.DeepEqual(got, []string{"golang", "search"}) {
			t.Errorf("marked = %q, want [golang search]", got)
		}
	})

	t.Run("a stopword slot inside the phrase is not highlighted", func(t *testing.T) {
		idx := addAll(t, "we love search the golang docs")

		results := mustSearch(t, idx, `"search the golang"`)

		got := marked(results[0].Snippets[0])
		if !reflect.DeepEqual(got, []string{"search", "golang"}) {
			t.Errorf("marked = %q, want [search golang]", got)
		}
	})

	t.Run("bare terms are highlighted next to the phrase", func(t *testing.T) {
		idx := addAll(t, "golang search tutorial")

		results := mustSearch(t, idx, `"golang search" tutorial`)

		got := marked(results[0].Snippets[0])
		if !reflect.DeepEqual(got, []string{"golang", "search", "tutorial"}) {
			t.Errorf("marked = %q, want [golang search tutorial]", got)
		}
	})

	t.Run("every occurrence of the phrase is highlighted", func(t *testing.T) {
		idx := addAll(t, "golang search and again golang search")

		results := mustSearch(t, idx, `"golang search"`)

		got := marked(results[0].Snippets[0])
		if len(got) != 4 {
			t.Errorf("marked = %q, want both occurrences (4 words)", got)
		}
	})

	t.Run("a phrase spanning a line break gets a snippet per line", func(t *testing.T) {
		idx := addAll(t, "this ends with golang\nsearch begins the next line")

		results := mustSearch(t, idx, `"golang search"`)

		if len(results) != 1 {
			t.Fatalf("expected 1 result, got %d", len(results))
		}
		if len(results[0].Snippets) != 2 {
			t.Errorf("expected one snippet per line, got %+v", results[0].Snippets)
		}
	})
}

// handBuiltIndex returns an index whose postings are written out by hand, so
// the phrase matcher is tested on exact positions with no files or analysis.
//
//	doc 1  golang search fundamentals      golang@0 search@1
//	doc 2  search the golang docs          search@0 golang@2   (the@1 is a stopword)
//	doc 3  search golang                   search@0 golang@1   (reversed)
//	doc 4  golang x y search               golang@0 search@3   (split apart)
//	doc 5  golang search ... golang search golang@0,4 search@1,5 (repeated)
//	doc 6  golang golang                   golang@0,1
//	doc 7  go go go                        go@0,1,2
func handBuiltIndex() *Index {
	idx := NewIndex()
	idx.Postings["golang"] = PostingList{
		{DocID: 1, Frequency: 1, Positions: []int{0}},
		{DocID: 2, Frequency: 1, Positions: []int{2}},
		{DocID: 3, Frequency: 1, Positions: []int{1}},
		{DocID: 4, Frequency: 1, Positions: []int{0}},
		{DocID: 5, Frequency: 2, Positions: []int{0, 4}},
		{DocID: 6, Frequency: 2, Positions: []int{0, 1}},
	}
	idx.Postings["search"] = PostingList{
		{DocID: 1, Frequency: 1, Positions: []int{1}},
		{DocID: 2, Frequency: 1, Positions: []int{0}},
		{DocID: 3, Frequency: 1, Positions: []int{0}},
		{DocID: 4, Frequency: 1, Positions: []int{3}},
		{DocID: 5, Frequency: 2, Positions: []int{1, 5}},
	}
	idx.Postings["go"] = PostingList{
		{DocID: 7, Frequency: 3, Positions: []int{0, 1, 2}},
	}
	return idx
}

func phraseOf(terms ...phraseTerm) phrase { return phrase{Terms: terms} }

// phraseHit is the part of a phrase posting the tests care about.
type phraseHit struct {
	Frequency int
	Positions []int
}

// hits turns matchPhrase output into docID -> phraseHit, with positions
// sorted so the comparison does not depend on their order.
func hits(list PostingList) map[int]phraseHit {
	out := make(map[int]phraseHit, len(list))
	for _, posting := range list {
		positions := append([]int(nil), posting.Positions...)
		sort.Ints(positions)
		out[posting.DocID] = phraseHit{Frequency: posting.Frequency, Positions: positions}
	}
	return out
}

func TestMatchPhrase(t *testing.T) {
	idx := handBuiltIndex()

	tests := []struct {
		name   string
		phrase phrase
		want   map[int]phraseHit
	}{
		{
			name:   "adjacent terms, with frequency and covered positions",
			phrase: phraseOf(pt("golang", 0), pt("search", 1)),
			want: map[int]phraseHit{
				1: {Frequency: 1, Positions: []int{0, 1}},
				5: {Frequency: 2, Positions: []int{0, 1, 4, 5}},
			},
		},
		{
			name:   "reversed order does not match",
			phrase: phraseOf(pt("search", 0), pt("golang", 1)),
			want:   map[int]phraseHit{3: {Frequency: 1, Positions: []int{0, 1}}},
		},
		{
			name:   "stopword gap: only the real words are covered, not the slot",
			phrase: phraseOf(pt("search", 0), pt("golang", 2)),
			want:   map[int]phraseHit{2: {Frequency: 1, Positions: []int{0, 2}}},
		},
		{
			name:   "larger gap matches only that distance",
			phrase: phraseOf(pt("golang", 0), pt("search", 3)),
			want:   map[int]phraseHit{4: {Frequency: 1, Positions: []int{0, 3}}},
		},
		{
			name:   "single word phrase returns every occurrence",
			phrase: phraseOf(pt("golang", 0)),
			want: map[int]phraseHit{
				1: {Frequency: 1, Positions: []int{0}},
				2: {Frequency: 1, Positions: []int{2}},
				3: {Frequency: 1, Positions: []int{1}},
				4: {Frequency: 1, Positions: []int{0}},
				5: {Frequency: 2, Positions: []int{0, 4}},
				6: {Frequency: 2, Positions: []int{0, 1}},
			},
		},
		{
			name:   "repeated word inside a phrase",
			phrase: phraseOf(pt("golang", 0), pt("golang", 1)),
			want:   map[int]phraseHit{6: {Frequency: 1, Positions: []int{0, 1}}},
		},
		{
			name:   "overlapping occurrences are all counted",
			phrase: phraseOf(pt("go", 0), pt("go", 1)),
			want:   map[int]phraseHit{7: {Frequency: 2, Positions: []int{0, 1, 1, 2}}},
		},
		{
			name:   "term missing from the index",
			phrase: phraseOf(pt("golang", 0), pt("zzzunknown", 1)),
			want:   map[int]phraseHit{},
		},
		{
			name:   "all terms indexed but never at that distance",
			phrase: phraseOf(pt("search", 0), pt("golang", 5)),
			want:   map[int]phraseHit{},
		},
		{
			name:   "empty phrase",
			phrase: phrase{},
			want:   map[int]phraseHit{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := idx.matchPhrase(tt.phrase)

			if !reflect.DeepEqual(hits(got), tt.want) {
				t.Errorf("matchPhrase()\n got: %v\nwant: %v", hits(got), tt.want)
			}

			// phraseCandidates and rankDocuments binary-search these lists.
			if !sort.SliceIsSorted(got, func(i, j int) bool { return got[i].DocID < got[j].DocID }) {
				t.Errorf("matchPhrase() result is not sorted by DocID: %v", got)
			}
		})
	}
}

// The anchor is the rarest term. The result must not depend on which term
// that turns out to be.
func TestMatchPhrase_SameResultWhicheverTermIsRarest(t *testing.T) {
	want := map[int]phraseHit{
		1: {Frequency: 1, Positions: []int{0, 1}},
	}

	idx := handBuiltIndex()
	idx.Postings["search"] = idx.Postings["search"][:2] // "search" is now the rarest term
	if got := hits(idx.matchPhrase(phraseOf(pt("golang", 0), pt("search", 1)))); !reflect.DeepEqual(got, want) {
		t.Errorf("rarest = search: got %v, want %v", got, want)
	}

	idx = handBuiltIndex()
	idx.Postings["golang"] = idx.Postings["golang"][:2] // "golang" is now the rarest term
	if got := hits(idx.matchPhrase(phraseOf(pt("golang", 0), pt("search", 1)))); !reflect.DeepEqual(got, want) {
		t.Errorf("rarest = golang: got %v, want %v", got, want)
	}
}

// Matching must never write into the index's own posting lists.
func TestMatchPhrase_DoesNotModifyIndex(t *testing.T) {
	idx := handBuiltIndex()

	idx.matchPhrase(phraseOf(pt("golang", 0), pt("search", 1)))
	idx.matchPhrase(phraseOf(pt("go", 0), pt("go", 1)))

	if !reflect.DeepEqual(idx.Postings, handBuiltIndex().Postings) {
		t.Errorf("matchPhrase changed the index postings")
	}
}

func TestPhraseCandidates(t *testing.T) {
	a := PostingList{{DocID: 1}, {DocID: 3}, {DocID: 5}}
	b := PostingList{{DocID: 3}, {DocID: 5}, {DocID: 7}}

	ids := func(list PostingList) []int {
		var out []int
		for _, p := range list {
			out = append(out, p.DocID)
		}
		return out
	}

	tests := []struct {
		name    string
		matches []PostingList
		want    []int
	}{
		{"no phrases", nil, nil},
		{"one phrase keeps its documents", []PostingList{a}, []int{1, 3, 5}},
		{"two phrases keep the intersection", []PostingList{a, b}, []int{3, 5}},
		{"a document missing early does not hide later matches", []PostingList{{{DocID: 1}, {DocID: 2}, {DocID: 3}}, {{DocID: 3}}}, []int{3}},
		{"a phrase with no matches empties the result", []PostingList{a, nil}, nil},
		{"the first phrase with no matches empties the result", []PostingList{nil, a}, nil},
		{"disjoint phrases", []PostingList{{{DocID: 1}}, {{DocID: 2}}}, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ids(phraseCandidates(tt.matches))

			if len(got) == 0 && len(tt.want) == 0 {
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("phraseCandidates() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPhraseCandidates_DoesNotModifyInput(t *testing.T) {
	a := PostingList{{DocID: 1}, {DocID: 2}, {DocID: 3}}
	b := PostingList{{DocID: 2}}

	phraseCandidates([]PostingList{a, b})

	if !reflect.DeepEqual(a, PostingList{{DocID: 1}, {DocID: 2}, {DocID: 3}}) {
		t.Errorf("phraseCandidates modified its input: %v", a)
	}
}

func TestPhrasePositions(t *testing.T) {
	p := phraseOf(pt("search", 0), pt("golang", 2))

	got := phrasePositions(p, []int{0, 10})

	// Exactly the covered positions: no leading zeros, no extra entries.
	want := []int{0, 2, 10, 12}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("phrasePositions() = %v, want %v", got, want)
	}

	if got := phrasePositions(p, nil); len(got) != 0 {
		t.Errorf("phrasePositions() with no starts = %v, want empty", got)
	}
}

func TestAverageDocLength(t *testing.T) {
	t.Run("empty index", func(t *testing.T) {
		if got := NewIndex().averageDocLength(); got != 0 {
			t.Errorf("averageDocLength() = %f, want 0", got)
		}
	})

	t.Run("mean of the stored document lengths", func(t *testing.T) {
		// Lengths count indexed tokens, so stopwords do not count.
		idx := addAll(t,
			"golang fast",                         // 2
			"golang is very fast and small",       // 4 (is, and are stopwords)
			"alpha beta gamma delta epsilon zeta", // 6
		)

		if got := idx.averageDocLength(); got != 4 {
			t.Errorf("averageDocLength() = %f, want 4", got)
		}
	})

	t.Run("stays correct after a removal", func(t *testing.T) {
		idx := NewIndex()
		short := makeTempFile(t, "golang fast")                        // 2
		medium := makeTempFile(t, "golang is very fast and small")     // 4
		long := makeTempFile(t, "alpha beta gamma delta epsilon zeta") // 6
		for _, path := range []string{short, medium, long} {
			if err := idx.AddDocument(path); err != nil {
				t.Fatal(err)
			}
		}

		if err := idx.RemoveDocument(long); err != nil {
			t.Fatal(err)
		}

		if got := idx.averageDocLength(); got != 3 {
			t.Errorf("averageDocLength() = %f, want 3", got)
		}
	})

	t.Run("survives a save and load", func(t *testing.T) {
		idx := addAll(t, "golang fast", "golang is very fast and small")
		path := makeTempFile(t, "")
		if err := idx.Save(path); err != nil {
			t.Fatal(err)
		}

		loaded, err := Load(path)
		if err != nil {
			t.Fatal(err)
		}

		if got, want := loaded.averageDocLength(), idx.averageDocLength(); got != want {
			t.Errorf("averageDocLength() after load = %f, want %f", got, want)
		}
	})
}

// TestBM25Ranking checks the two behaviors BM25 adds, end to end through Search.
func TestBM25Ranking(t *testing.T) {

	t.Run("same term count, shorter document ranks first", func(t *testing.T) {
		// The long document is indexed first so a correct ranking has to move
		// the short one ahead of it. Doc 3 keeps the term's IDF meaningful.
		idx := addAll(t,
			"golang alpha beta gamma delta epsilon zeta eta theta iota", // 1: 10 tokens, 1 match
			"golang fast",  // 2: 2 tokens, 1 match
			"python rocks", // 3
		)

		results := mustSearch(t, idx, "golang")

		if len(results) != 2 {
			t.Fatalf("expected 2 results, got %v", docIDs(results))
		}
		if results[0].Doc.ID != 2 {
			t.Errorf("short document should rank first, got doc %d", results[0].Doc.ID)
		}
		if results[0].Score <= results[1].Score {
			t.Errorf("expected strictly higher score, got %v then %v", results[0].Score, results[1].Score)
		}
	})

	t.Run("repeating a term has diminishing returns", func(t *testing.T) {
		// Docs 1 and 2 are the same length, so only the repeat count differs:
		// 20 occurrences against 4. Plain TF-IDF would score doc 1 five times
		// higher; BM25 must keep the gap small.
		idx := addAll(t,
			strings20("golang"), // 1: 20 occurrences
			strings20("golang", "golang", "golang", "golang"), // 2: 4 occurrences, 16 filler
			"python rocks", // 3: short document that keeps the average low
		)

		results := mustSearch(t, idx, "golang")

		if len(results) != 2 {
			t.Fatalf("expected 2 results, got %v", docIDs(results))
		}
		if results[0].Doc.ID != 1 {
			t.Fatalf("more occurrences should still rank first, got doc %d", results[0].Doc.ID)
		}
		if ratio := results[0].Score / results[1].Score; ratio >= 2 {
			t.Errorf("score ratio = %f, want < 2 (plain TF-IDF would give 5)", ratio)
		}
	})

	t.Run("a term in every document still produces a positive, ordered score", func(t *testing.T) {
		// With the old ln(N/df) IDF every score here was exactly 0.
		idx := addAll(t, "golang is great", "golang golang fast")

		results := mustSearch(t, idx, "golang")

		if len(results) != 2 {
			t.Fatalf("expected 2 results, got %d", len(results))
		}
		for _, r := range results {
			if r.Score <= 0 {
				t.Errorf("doc %d score = %f, want > 0", r.Doc.ID, r.Score)
			}
		}
		if results[0].Doc.ID != 2 {
			t.Errorf("doc 2 (two occurrences) should rank first, got doc %d", results[0].Doc.ID)
		}
	})
}

// strings20 builds a 20-token document: the given words first, padded with
// "pad" up to 20 tokens. With a single word it repeats that word 20 times.
func strings20(words ...string) string {
	tokens := make([]string, 0, 20)
	if len(words) == 1 {
		for len(tokens) < 20 {
			tokens = append(tokens, words[0])
		}
	} else {
		tokens = append(tokens, words...)
		for len(tokens) < 20 {
			tokens = append(tokens, "pad")
		}
	}
	return strings.Join(tokens, " ")
}

func TestAddDocuments(t *testing.T) {
	tests := []struct {
		name         string
		files        map[string]string
		extensions   []string
		wantDocs     int
		wantPostings bool
		expectError  bool
	}{
		{
			name: "single valid file",
			files: map[string]string{
				"doc1.txt": "golang is fast",
			},
			extensions:   []string{".txt"},
			wantDocs:     1,
			wantPostings: true,
		},
		{
			name: "ignore non-matching extensions",
			files: map[string]string{
				"doc1.txt": "golang",
				"doc2.md":  "python",
			},
			extensions:   []string{".txt"},
			wantDocs:     1,
			wantPostings: true,
		},
		{
			name: "multiple valid files",
			files: map[string]string{
				"doc1.txt": "golang",
				"doc2.txt": "python",
			},
			extensions:   []string{".txt"},
			wantDocs:     2,
			wantPostings: true,
		},
		{
			name: "nested directories",
			files: map[string]string{
				"a/doc1.txt": "golang",
				"b/doc2.txt": "python",
			},
			extensions:   []string{".txt"},
			wantDocs:     2,
			wantPostings: true,
		},
		{
			name: "no matching files",
			files: map[string]string{
				"doc1.md": "golang",
			},
			extensions:   []string{".txt"},
			wantDocs:     0,
			wantPostings: false,
		},
		{
			name:         "empty directory",
			files:        map[string]string{},
			extensions:   []string{".txt"},
			wantDocs:     0,
			wantPostings: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			idx := NewIndex()
			root := makeTempDir(t, tt.files)

			err := idx.AddDocuments(root, tt.extensions)

			if tt.expectError {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if len(idx.Docs) != tt.wantDocs {
				t.Errorf("expected %d docs, got %d", tt.wantDocs, len(idx.Docs))
			}

			if idx.TotalDocs != tt.wantDocs {
				t.Errorf("expected TotalDocs %d, got %d", tt.wantDocs, idx.TotalDocs)
			}

			if tt.wantPostings && len(idx.Postings) == 0 {
				t.Errorf("expected postings to be populated")
			}

			if !tt.wantPostings && len(idx.Postings) != 0 {
				t.Errorf("expected no postings, got %v", idx.Postings)
			}
		})
	}
}
