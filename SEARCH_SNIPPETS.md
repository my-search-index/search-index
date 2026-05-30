# Search Snippets

Search now returns ranked `Result` values instead of plain `Document` values. A
result contains the document metadata, the score, short snippet text, and exact
match ranges the UI can highlight.

```go
type Result struct {
	Doc      Document
	Snippets []Snippet
	Score    float64
}

type Snippet struct {
	Text    string
	Matches []Match
}

type Match struct {
	Start int
	End   int
	Term  string
}
```

## Why the snippets were wrong

`Posting.Positions` is the key to snippets, but positions must mean one thing
everywhere. The snippet extractor walks through the original file and compares
the current document-wide token offset with the offsets stored in the posting.

Before this change, two things made those offsets unreliable:

1. Stop-word filtering renumbered tokens after removing words like `the` and
   `and`.
2. Each indexed line started positions from zero again.

That made many lines look like matches at position `0`, which is why snippets
could show generic page text such as `Talk`, `Read`, or `View source`.

## Implementation steps

1. Preserve original token positions when removing stop words.

   Code: `analysis/stopwords.go`

   `FilterStopWords` now appends the original token instead of creating a new
   token with a new position.

2. Store document-wide positions while indexing.

   Code: `index/index.go`

   `AddDocument` keeps a `positionOffset` while scanning the file line by line.
   It adds that offset to each analyzed token before calling `buildIndex`, then
   advances the offset by the raw token count from `analysis.Tokenize(line)`.

3. Return structured snippets directly from search.

   Code: `index/index.go`

   `Search` builds a `Result` for each ranked document and calls
   `ExtractSnippets` before returning it.

4. Extract matched source lines from posting positions.

   Code: `index/snippet.go`

   `ExtractSnippets` collects all matched positions for the result document,
   reopens the source file, walks line by line, and builds a short snippet when
   one of the line tokens has a document-wide position in the match set.

   The snippet keeps about 60 characters of context around the first match.
   `Matches` contains `Start` and `End` offsets inside `Snippet.Text`, so the UI
   can wrap only those ranges in a highlight element.

5. Print snippets from the returned results.

   Code: `cmd/searchcli/main.go`

   The CLI now prints `result.Doc.FilePath`, `result.Score`, and each
   `snippet.Text`. A frontend can use `snippet.Matches` to highlight individual
   words without highlighting the full snippet.

## Reindexing required

Existing `search.idx` files keep the old posting positions. Rebuild the index
after this change so snippets use the corrected document-wide offsets.

Example:

```sh
rm search.idx
go run ./cmd/searchcli add ../web-crawler
go run ./cmd/searchcli add ../search-engines
go run ./cmd/searchcli add ../information-retrieval
go run ./cmd/searchcli search "distributed computing"
```

## Verification

The focused tests cover:

1. Positions continuing across lines: `index/index_test.go`
2. Search returning snippet text and match ranges: `index/index_test.go`
3. Stop-word filtering preserving positions: `analysis/tests/stopwords_test.go`
4. Analyzer output preserving original offsets: `analysis/tests/analyzer_test.go`

Full verification:

```sh
go test ./...
```
