package index

import (
	"bufio"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"github.com/my-search-index/search-index/analysis"
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
	doc      Document
	score    float64
	postings []Posting
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

// Search returns ranked documents for query.
//
// The query is parsed exactly once here; the parsed form drives candidate
// selection, ranking and snippet extraction. Parse failures
// (ErrUnterminatedQuote, ErrEmptyPhrase) are returned unchanged so callers can
// use errors.Is. An empty or all-stopword query is not an error: it returns
// no results.
//
// Candidate rules:
//   - With phrases, a document must contain EVERY phrase. Bare terms never add
//     candidates; they only raise the score of documents that already qualify.
//     If no document contains all phrases the result is empty (there is no
//     fallback to matching the words separately).
//   - Without phrases, any document containing at least one bare term
//     qualifies (OR).
//
// Snippets are only extracted for the top snippetResultLimit results, because
// each one reopens and scans a source file.
func (idx *Index) Search(query string) ([]Result, error) {
	q, err := parseQuery(query)
	if err != nil {
		return nil, err
	}

	var candidates PostingList
	var phraseMatches []PostingList // phraseMatches[i] belongs to q.Phrases[i]
	if q.hasPhrases() {
		phraseMatches = make([]PostingList, len(q.Phrases))
		for i, p := range q.Phrases {
			phraseMatches[i] = idx.matchPhrase(p)
		}
		candidates = phraseCandidates(phraseMatches)
	} else {
		candidates = idx.unionPostings(idx.getPostings(q.Terms))
	}
	ranked := idx.rankDocuments(candidates, q, phraseMatches)

	var results []Result
	for i, scored := range ranked {
		result := Result{
			Doc:   scored.doc,
			Score: scored.score,
		}
		if i < snippetResultLimit {
			result.Snippets = extractSnippets(scored.doc, scored.postings)
		}
		results = append(results, result)
	}
	return results, nil
}

// getPostings returns the posting list of every query word that exists in the
// index. Unknown words are skipped, which is what makes the search an OR.
func (idx *Index) getPostings(words []string) []PostingList {
	if len(words) == 0 {
		return nil
	}

	var result []PostingList

	for _, word := range words {
		list := idx.Postings[word]
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

// rankDocuments scores every candidate and returns them best first.
//
// Score = TF-IDF of every query word (phrase words and bare terms) plus, for
// each phrase, the phrase's own TF-IDF treated as a single pseudo-term: its
// frequency is the number of times the phrase occurs in the document and its
// document frequency is the number of documents containing the phrase. That
// bonus needs no tuning constant, rewards documents that repeat the phrase,
// rewards rarer phrases more, and keeps working if Score becomes BM25.
//
// While scoring it also keeps the postings snippets should highlight: the bare
// terms' postings, plus each phrase's posting, whose positions cover only the
// phrase occurrences. Phrase words are deliberately not highlighted outside
// the phrase.
//
// phraseMatches[i] belongs to q.Phrases[i]; it is nil when there are no phrases.
func (idx *Index) rankDocuments(candidates PostingList, q parsedQuery, phraseMatches []PostingList) []scoredDocs {
	var scoredList []scoredDocs

	for _, candidate := range candidates {
		docID := candidate.DocID
		doc := idx.Docs[docID]
		totalScore := 0.0
		var highlight []Posting

		addWord := func(word string, mark bool) {
			list := idx.Postings[word]
			posting := searchPosting(list, docID)
			if posting.DocID == 0 {
				return // this word does not occur in this document, zero contribution
			}
			totalScore += Score(posting, doc, len(list), idx.TotalDocs)
			if mark {
				highlight = append(highlight, posting)
			}
		}

		for _, p := range q.Phrases {
			for _, term := range p.Terms {
				addWord(term.Word, false)
			}
		}
		for _, term := range q.Terms {
			addWord(term, true)
		}

		for i := range q.Phrases {
			phrasePosting := searchPosting(phraseMatches[i], docID)
			totalScore += Score(phrasePosting, doc, len(phraseMatches[i]), idx.TotalDocs)
			highlight = append(highlight, phrasePosting)
		}

		scoredList = append(scoredList, scoredDocs{doc: doc, score: totalScore, postings: highlight})
	}

	// Sort by score, best first. Stable, so ties keep a deterministic order.
	sort.SliceStable(scoredList, func(i, j int) bool {
		return scoredList[i].score > scoredList[j].score
	})

	return scoredList
}

// phraseCandidates returns the documents that match every phrase. matches[i]
// is the result of matchPhrase for phrase i, so each list is sorted by doc ID.
// The returned postings are the first phrase's; callers only use their DocID.
// Bare terms do not take part: they are used later for scoring only.
func phraseCandidates(matches []PostingList) PostingList {
	if len(matches) == 0 {
		return nil
	}

	var candidates PostingList // always a new slice, never an alias of an index list

	for _, posting := range matches[0] {
		inAll := true
		for _, other := range matches[1:] {
			if searchPosting(other, posting.DocID).DocID == 0 {
				inAll = false
				break
			}
		}
		if inAll {
			candidates = append(candidates, posting)
		}
	}
	return candidates
}

// matchPhrase finds the documents containing the phrase at the required
// relative positions. It starts with the shortest term posting list to avoid
// checking documents that cannot match.
//
// It returns one POSTING PER MATCHING DOCUMENT, sorted by doc ID, with:
//   - Frequency: how many times the phrase occurs in the document
//     (overlapping occurrences count: "go go" occurs twice in "go go go").
//   - Positions: every token position the occurrences cover, so snippets can
//     highlight exactly the phrase words. Not necessarily sorted or unique.
func (idx *Index) matchPhrase(p phrase) PostingList {
	if len(p.Terms) == 0 {
		return nil
	}

	lists := make([]PostingList, len(p.Terms))
	shortest := 0

	for i, term := range p.Terms {
		lists[i] = idx.Postings[term.Word]
		if len(lists[i]) == 0 {
			return nil // Every phrase word is required.
		}
		if len(lists[i]) < len(lists[shortest]) {
			shortest = i
		}
	}

	var matches PostingList
	for _, anchor := range lists[shortest] {
		postings := make([]Posting, len(lists))
		postings[shortest] = anchor

		allWordsPresent := true
		for i, list := range lists {
			if i == shortest {
				continue
			}
			postings[i] = searchPosting(list, anchor.DocID)
			if postings[i].DocID == 0 {
				allWordsPresent = false
				break
			}
		}
		if !allWordsPresent {
			continue
		}

		starts := phraseStarts(p, postings, shortest)
		if len(starts) == 0 {
			continue
		}
		matches = append(matches, Posting{
			DocID:     anchor.DocID,
			Frequency: len(starts),
			Positions: phrasePositions(p, starts),
		})
	}
	return matches
}

// phraseStarts returns the start position of every occurrence of the phrase
// in one document, ascending. postings[i] is the document's posting for
// p.Terms[i]; the anchor may be any phrase term, so its offset is subtracted
// to find each possible start. Posting positions must be sorted.
func phraseStarts(p phrase, postings []Posting, anchorIndex int) []int {
	var starts []int

	for _, anchorPos := range postings[anchorIndex].Positions {
		start := anchorPos - p.Terms[anchorIndex].Offset
		if start < 0 {
			continue
		}

		matchesHere := true
		for i, term := range p.Terms {
			wanted := start + term.Offset
			positions := postings[i].Positions
			at := sort.SearchInts(positions, wanted)

			if at == len(positions) || positions[at] != wanted {
				matchesHere = false
				break
			}
		}
		if matchesHere {
			starts = append(starts, start)
		}
	}
	return starts
}

// phrasePositions expands start positions into every token position the
// occurrences cover (start + each term's offset).
func phrasePositions(p phrase, starts []int) []int {
	positions := make([]int, 0, len(starts)*len(p.Terms))
	for _, start := range starts {
		for _, term := range p.Terms {
			positions = append(positions, start+term.Offset)
		}
	}
	return positions
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
