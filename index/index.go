package index

import (
	"bufio"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"github.com/Stacvirus/search-index/analysis"
)

// Index stores documents and their inverted posting lists.
type Index struct {
	Postings  map[string]PostingList
	Docs      map[int]Document
	NextDocID int
	TotalDocs int
}

// Document is the metadata stored for one indexed file.
type Document struct {
	ID       int
	FilePath string
	Length   int
}

type scoredDocs struct {
	doc   Document
	score float64
}

// Match is a query term occurrence inside Snippet.Text.
type Match struct {
	Start int
	End   int
	Term  string
}

// Snippet is the display text plus exact ranges the UI can highlight.
type Snippet struct {
	Text    string
	Matches []Match
}

// Result is one search hit plus the snippets that matched the query.
type Result struct {
	Doc      Document
	Snippets []Snippet
	Score    float64
}

// NewIndex creates an empty in-memory search index.
func NewIndex() *Index {
	return &Index{
		Postings:  make(map[string]PostingList),
		Docs:      make(map[int]Document),
		NextDocID: 1,
		TotalDocs: 0,
	}
}

// AddDocuments adds all documents in the specified directory to the index,
// filtering by the specified extensions.
func (idx *Index) AddDocuments(root string, extensions []string) error {
	extSet := make(map[string]bool)
	for _, ext := range extensions {
		extSet[ext] = true
	}

	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil // Skip directories, we only want to index files
		}
		if !extSet[filepath.Ext(path)] {
			return nil // Skip files that don't have the specified extensions
		}

		// store path relative to the root directory in the index, so that we can move
		// the index to a different location without breaking the file paths
		relPath, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}

		return idx.AddDocument(filepath.Join(root, relPath))
	})
}

// AddDocument reads one file and adds its analyzed tokens to the index.
// Posting positions are document-wide token offsets, which lets snippet
// extraction reopen the file and walk back to the matched line later.
func (idx *Index) AddDocument(filePath string) error {
	totalTokens := 0
	positionOffset := 0
	docID := idx.NextDocID

	// Get access to the file
	file, err := os.Open(filePath)
	if err != nil {
		return err
	}

	defer file.Close()

	scanner := bufio.NewScanner(file)

	// Read the file line by line and analyze the content
	for scanner.Scan() {
		line := scanner.Text()
		tokens := analysis.Analyze(line) // Get the tokens from the line
		for i := range tokens {
			tokens[i].Position += positionOffset
		}
		totalTokens += len(tokens)
		idx.buildIndex(tokens, docID)
		positionOffset += len(analysis.Tokenize(line))
	}

	if err := scanner.Err(); err != nil {
		return err
	}

	// Add the document to the index and increment the document ID
	idx.Docs[docID] = Document{
		ID:       docID,
		FilePath: filePath,
		Length:   totalTokens,
	}
	idx.NextDocID++
	idx.TotalDocs++

	return nil
}

// RemoveDocument removes one indexed file and all of its postings.
// It keeps NextDocID append-only, but decrements TotalDocs so ranking uses the
// current number of indexed documents.
func (idx *Index) RemoveDocument(filePath string) error {
	docID, ok := idx.findDocIDByPath(filePath)
	if !ok {
		return fmt.Errorf("document not indexed: %s", filePath)
	}

	delete(idx.Docs, docID)

	for term, list := range idx.Postings {
		filtered := list[:0]
		for _, posting := range list {
			if posting.DocID != docID {
				filtered = append(filtered, posting)
			}
		}

		if len(filtered) == 0 {
			delete(idx.Postings, term)
			continue
		}
		idx.Postings[term] = filtered
	}

	if idx.TotalDocs > 0 {
		idx.TotalDocs--
	}
	return nil
}

func (idx *Index) findDocIDByPath(filePath string) (int, bool) {
	for docID, doc := range idx.Docs {
		if doc.FilePath == filePath {
			return docID, true
		}
	}
	return 0, false
}

// ListDocuments returns indexed documents sorted by document ID.
func (idx *Index) ListDocuments() []Document {
	docIDs := make([]int, 0, len(idx.Docs))
	for docID := range idx.Docs {
		docIDs = append(docIDs, docID)
	}
	sort.Ints(docIDs)

	documents := make([]Document, 0, len(docIDs))
	for _, docID := range docIDs {
		documents = append(documents, idx.Docs[docID])
	}
	return documents
}

// Update the index with the tokens from the document
func (idx *Index) buildIndex(tokens []analysis.Token, docID int) {
	for _, token := range tokens {
		// Update the posting list for the token
		postingList := idx.Postings[token.Word] // If the token doesn't exist, this will return an empty list

		// Create a new posting for the document
		posting := Posting{
			DocID:     docID,
			Frequency: 1,
			Positions: []int{token.Position},
		}

		// Check if the document already exists in the posting list
		postingListLength := len(postingList)
		if postingListLength > 0 && postingList[postingListLength-1].DocID == docID {
			last := postingListLength - 1
			postingList[last].Frequency++
			postingList[last].Positions = append(postingList[last].Positions, token.Position)
		} else {
			postingList = append(postingList, posting)
		}
		idx.Postings[token.Word] = postingList
	}
}

// Search returns ranked documents for query, including matching snippets.
// The index already stores posting positions, so no index schema changes are
// needed to attach snippets to each result.
func (idx *Index) Search(query string) []Result {
	// Analyze the query to get the tokens
	tokens := analysis.Analyze(query)
	lists := idx.getPostings(tokens)
	result := idx.unionPostings(lists)
	scoredList := idx.rankDocuments(result, tokens)

	var results []Result
	for _, scored := range scoredList {
		result := Result{
			Doc:      scored.doc,
			Snippets: nil,
			Score:    scored.score,
		}
		results = append(results, idx.ExtractSnippets(result, query))
	}
	return results
}

func (idx *Index) getPostings(tokens []analysis.Token) []PostingList {
	if len(tokens) == 0 {
		return nil
	}

	var result []PostingList

	for _, token := range tokens {
		list := idx.Postings[token.Word]
		if len(list) == 0 {
			continue // We can skip tokens that don't exist in the index, as they won't contribute to the search results
		}
		result = append(result, list)
	}
	return result
}

// Union posting lists merges all the postings from the lists,
// while intersection only keeps the postings that are present in all lists.
// Union is used for OR queries, while intersection is used for AND queries.
func (idx *Index) unionPostings(lists []PostingList) PostingList {
	if len(lists) == 0 {
		return nil
	}
	seen := make(map[int]struct{})
	var result PostingList
	for _, list := range lists {
		for _, posting := range list {
			if _, exists := seen[posting.DocID]; !exists {
				result = append(result, posting)
				seen[posting.DocID] = struct{}{}
			}
		}
	}
	return result
}

func (idx *Index) rankDocuments(postings PostingList, tokens []analysis.Token) []scoredDocs {
	var scoredList []scoredDocs

	for _, posting := range postings {
		doc := idx.Docs[posting.DocID]
		totalScore := 0.0
		for _, token := range tokens {
			list := idx.Postings[token.Word]
			totalScore += Score(searchPosting(list, posting.DocID), doc, len(list), idx.TotalDocs)
		}
		scoredList = append(scoredList, scoredDocs{doc, totalScore})
	}

	// Sort the scored documents by score in descending order
	sort.Slice(scoredList, func(i, j int) bool {
		return scoredList[i].score > scoredList[j].score
	})

	return scoredList
}

// Binary search posting algo impl
func searchPosting(list PostingList, docID int) Posting {
	last, first := len(list), 0
	for first < last {
		mid := (first + last) / 2
		if list[mid].DocID == docID {
			return list[mid]
		} else if list[mid].DocID < docID {
			first = mid + 1
		} else {
			last = mid
		}
	}
	return Posting{} // Branch suppose to never be reached if the document is guaranteed to be in the list
}
