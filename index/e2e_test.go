package index

import "testing"

func TestEndToEndSearch(t *testing.T) {

	t.Run("one document contains term", func(t *testing.T) {
		idx := NewIndex()
		path := makeTempFile(t, "golang is fast")

		_ = idx.AddDocument(path)

		results := mustSearch(t, idx, "golang")

		if len(results) != 1 {
			t.Fatalf("expected 1 result, got %d", len(results))
		}
	})

	t.Run("one document does not contain term", func(t *testing.T) {
		idx := NewIndex()
		path := makeTempFile(t, "golang is fast")

		_ = idx.AddDocument(path)

		results := mustSearch(t, idx, "python")

		if len(results) != 0 {
			t.Fatalf("expected 0 results, got %d", len(results))
		}
	})

	t.Run("two documents, term in only one", func(t *testing.T) {
		idx := NewIndex()

		p1 := makeTempFile(t, "golang is fast")
		p2 := makeTempFile(t, "python is popular")

		_ = idx.AddDocument(p1)
		_ = idx.AddDocument(p2)

		results := mustSearch(t, idx, "golang")

		if len(results) != 1 {
			t.Fatalf("expected 1 result, got %d", len(results))
		}

		if results[0].Doc.ID != 1 {
			t.Errorf("expected doc 1, got %d", results[0].Doc.ID)
		}
	})

	t.Run("two documents, both contain term, higher score first", func(t *testing.T) {
		idx := NewIndex()

		// doc1: 1 occurrence. It is indexed FIRST, so a correct ranking has
		// to actively move doc2 ahead of it.
		p1 := makeTempFile(t, "golang is great")
		// doc2: 2 occurrences
		p2 := makeTempFile(t, "golang golang fast")
		// doc3 does not contain the term. Without it the term is in every
		// document, its IDF is 0, and every score ties at 0, which would make
		// this test pass without ranking anything.
		p3 := makeTempFile(t, "python is popular")

		_ = idx.AddDocument(p1)
		_ = idx.AddDocument(p2)
		_ = idx.AddDocument(p3)

		results := mustSearch(t, idx, "golang")

		if len(results) != 2 {
			t.Fatalf("expected 2 results, got %d", len(results))
		}

		if results[0].Doc.ID != 2 {
			t.Errorf("expected doc 2 (higher TF) ranked first, got %d", results[0].Doc.ID)
		}

		if results[0].Score <= results[1].Score {
			t.Errorf("expected strictly higher score first, got %v then %v", results[0].Score, results[1].Score)
		}
	})

	t.Run("multi-term query, return all docs having atleast one term", func(t *testing.T) {
		idx := NewIndex()

		p1 := makeTempFile(t, "golang is fast")
		p2 := makeTempFile(t, "golang is popular")

		_ = idx.AddDocument(p1)
		_ = idx.AddDocument(p2)

		results := mustSearch(t, idx, "golang fast")

		if len(results) != 2 {
			t.Fatalf("expected 2 results, got %d", len(results))
		}

		if results[0].Doc.ID != 1 {
			t.Errorf("expected doc 1, got %d", results[0].Doc.ID)
		}
	})

	t.Run("multi-term query, at least one term present in doc", func(t *testing.T) {
		idx := NewIndex()

		p1 := makeTempFile(t, "golang fast")
		p2 := makeTempFile(t, "python slow")

		_ = idx.AddDocument(p1)
		_ = idx.AddDocument(p2)

		results := mustSearch(t, idx, "golang slow")

		if len(results) != 2 {
			t.Fatalf("expected 2 results, got %d", len(results))
		}
	})

	t.Run("empty query returns no results", func(t *testing.T) {
		idx := NewIndex()

		p1 := makeTempFile(t, "golang fast")
		_ = idx.AddDocument(p1)

		results := mustSearch(t, idx, "")

		if len(results) != 0 {
			t.Fatalf("expected 0 results, got %d", len(results))
		}
	})

	t.Run("index directory and search term", func(t *testing.T) {
		idx := NewIndex()

		root := makeTempDir(t, map[string]string{
			"a.txt": "golang is fast",
			"b.txt": "python is slow",
		})

		err := idx.AddDocuments(root, []string{".txt"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		results := mustSearch(t, idx, "golang")

		if len(results) != 1 {
			t.Fatalf("expected 1 result, got %d", len(results))
		}
	})

	t.Run("ignore non matching extensions", func(t *testing.T) {
		idx := NewIndex()

		root := makeTempDir(t, map[string]string{
			"a.txt": "golang",
			"b.md":  "python",
		})

		err := idx.AddDocuments(root, []string{".txt"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		results := mustSearch(t, idx, "python")

		if len(results) != 0 {
			t.Fatalf("expected 0 results, got %d", len(results))
		}
	})

	t.Run("nested directories indexing", func(t *testing.T) {
		idx := NewIndex()

		root := makeTempDir(t, map[string]string{
			"dir1/a.txt": "golang",
			"dir2/b.txt": "fast",
		})

		err := idx.AddDocuments(root, []string{".txt"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		results := mustSearch(t, idx, "fast")

		if len(results) != 1 {
			t.Fatalf("expected 1 result, got %d", len(results))
		}
	})

	t.Run("ranking works across directory documents", func(t *testing.T) {
		idx := NewIndex()

		// Files are walked in lexical order: a.txt is doc 1, b.txt doc 2,
		// c.txt doc 3. The higher-TF document is NOT first, and c.txt keeps
		// the term's IDF above zero (see the two-document ranking test).
		root := makeTempDir(t, map[string]string{
			"a.txt": "golang is great",
			"b.txt": "golang golang fast", // higher TF
			"c.txt": "python is popular",
		})

		err := idx.AddDocuments(root, []string{".txt"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		results := mustSearch(t, idx, "golang")

		if len(results) != 2 {
			t.Fatalf("expected 2 results, got %d", len(results))
		}

		if results[0].Doc.ID != 2 {
			t.Errorf("expected higher TF doc first, got %d", results[0].Doc.ID)
		}
	})

	t.Run("empty directory", func(t *testing.T) {
		idx := NewIndex()

		root := makeTempDir(t, map[string]string{})

		err := idx.AddDocuments(root, []string{".txt"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		results := mustSearch(t, idx, "golang")

		if len(results) != 0 {
			t.Fatalf("expected 0 results, got %d", len(results))
		}
	})

	t.Run("quoted query surfaces a parse error to the caller", func(t *testing.T) {
		idx := NewIndex()

		p1 := makeTempFile(t, "golang is fast")
		_ = idx.AddDocument(p1)

		if _, err := idx.Search(`"golang is`); err == nil {
			t.Fatalf("expected an error for an unterminated quote, got nil")
		}
	})
}
