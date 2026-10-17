package index

import (
	"errors"
	"math"
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

func TestPhraseSearch(t *testing.T) {
	tests := []struct {
		name  string
		docs  []string
		query string
		want  []int
	}{
		{"adjacent words", []string{"golang search", "golang quick search", "search golang"}, `"golang search"`, []int{1}},
		{"stopword gap", []string{"search the golang", "search a golang", "search golang"}, `"search the golang"`, []int{1, 2}},
		{"repeated word", []string{"go go", "go fast go"}, `"go go"`, []int{1}},
		{"multiple phrases", []string{"red fox and blue whale", "red fox", "blue whale"}, `"red fox" "blue whale"`, []int{1}},
		{"bare term only boosts", []string{"golang search tutorial", "golang search", "tutorial"}, `"golang search" tutorial`, []int{1, 2}},
		{"unknown phrase word", []string{"golang search"}, `"golang missing"`, nil},
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

func TestTermFrequency(t *testing.T) {
	tests := []struct {
		name    string
		posting Posting
		doc     Document
		want    float64
	}{
		{
			name:    "basic tf",
			posting: Posting{Frequency: 2},
			doc:     Document{Length: 4},
			want:    0.5,
		},
		{
			name:    "higher frequency",
			posting: Posting{Frequency: 5},
			doc:     Document{Length: 10},
			want:    0.5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := termFrequency(tt.posting, tt.doc)

			if got != tt.want {
				t.Errorf("termFrequency() = %f, want %f", got, tt.want)
			}
		})
	}
}

func TestInverseDocumentFrequency(t *testing.T) {
	tests := []struct {
		name      string
		listLen   int
		totalDocs int
		want      float64
	}{
		{
			name:      "basic idf",
			listLen:   2,
			totalDocs: 10,
			want:      math.Log(5),
		},
		{
			name:      "term in all docs",
			listLen:   10,
			totalDocs: 10,
			want:      0.0, // log(1)
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := inverseDocumentFrequency(tt.listLen, tt.totalDocs)

			if math.Abs(got-tt.want) > 1e-9 {
				t.Errorf("idf() = %f, want %f", got, tt.want)
			}
		})
	}
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
