# Search Index

A small Go search engine built from scratch. It indexes local `.txt` and `.md`
files, persists the inverted index to disk, ranks matches with TF-IDF, and
returns snippets with exact highlight ranges for the matching query terms.

## High-Level Architecture

```text
                    +------------------+
                    |  Library + CLI   |
                    | searchindex.go   |
                    +--------+---------+
                             |
              +--------------+--------------+
        add <path>         remove <path>           list              search <query>
            |                   |                   |                      |
            v                   v                   v                      v
   +----------------+   +----------------+   +----------------+   +----------------+
   | Indexing       |   | Remove         |   | List           |   | Search         |
   | AddDocument(s) |   | RemoveDocument |   | ListDocuments  |   | Search         |
   +-------+--------+   +-------+--------+   +-------+--------+   +-------+--------+
           |                    |                    |                    |
           v                    v                    v                    v
   +----------------+   +----------------+   +----------------+   +----------------+
   | Text Analysis  |   | Clean Docs map |   | Sort doc IDs   |   | Query Analysis |
   | Build postings |   | Clean postings |   | Print docs     |   | Lookup terms   |
   +-------+--------+   +-------+--------+   +----------------+   +-------+--------+
           |                    |                                         |
           v                    v                                         v
   +----------------+   +----------------+                       +----------------+
   | TotalDocs++    |   | TotalDocs--    |                       | Rank + Snippet |
   | Save search.idx|   | Save search.idx|                       | []Result       |
   +----------------+   +----------------+                       +----------------+
```

The project is intentionally simple: no database, no background workers, and no
external search service. Everything lives in memory while the command runs, then
the index is saved to `search.idx`.

## Using It As A Library

The root module exposes the public package API. Another Go backend can import
the project and call the search index directly.

```go
import searchindex "github.com/my-search-index/search-index"
```

Example:

```go
idx, err := searchindex.Load("search.idx")
if err != nil {
	return err
}

results := idx.Search("distributed computing")
documents := idx.ListDocuments()
_ = results
_ = documents

if err := idx.AddDocument("../data/web-crawler.txt"); err != nil {
	return err
}

if err := idx.Save("search.idx"); err != nil {
	return err
}
```

The root package is a small facade over the internal implementation package, so
callers only need one import path. The main API is:

```go
searchindex.New()
searchindex.Load(path)
idx.Save(path)
idx.AddDocument(path)
idx.AddDocuments(root, extensions)
idx.RemoveDocument(path)
idx.ListDocuments()
idx.Search(query)
```

## Main Concepts

### Document

`Document` stores the file metadata needed after indexing.

```go
type Document struct {
	ID       int
	FilePath string
	Length   int
}
```

`Length` is the number of analyzed, non-stop-word tokens. It is used for term
frequency scoring.

### Index

`Index` is the top-level in-memory data structure.

```go
type Index struct {
	Postings  map[string]PostingList
	Docs      map[int]Document
	NextDocID int
	TotalDocs int
}
```

`NextDocID` is append-only and never decrements. `TotalDocs` tracks the current
number of indexed documents and is used by IDF scoring.

### Posting

`Posting` stores how one term appears in one document.

```go
type Posting struct {
	DocID     int
	Frequency int
	Positions []int
}
```

`Positions` are document-wide token offsets. They are important because snippets
use them later to reopen the original file and find the matching source line.

### Inverted Index

The index maps each normalized term to the documents that contain it.

```text
Postings:
  "golang" ->
    [{ DocID: 1, Frequency: 2, Positions: [1, 8] }]

Docs:
  1 -> { ID: 1, FilePath: "../data/web-crawler.txt", Length: 1200 }
```

## Indexing Flow

```text
file path
   |
   v
open file
   |
   v
scan line by line
   |
   v
analysis.Analyze(line)
   |
   +--> Tokenize      "Go is great" -> [{Go 0}, {is 1}, {great 2}]
   +--> Normalize     [{go 0}, {is 1}, {great 2}]
   +--> Stop words    [{go 0}, {great 2}]
   |
   v
add document-wide position offset
   |
   v
build inverted index
   |
   v
save to search.idx
```

The document-wide offset matters. Each line starts with local token positions
from `Tokenize`, so `AddDocument` adds `positionOffset` before storing positions
in postings.

Example:

```text
Line 1: "the golang"  -> raw positions: the=0, golang=1
Line 2: "and search"  -> raw positions: and=0, search=1

After offsets:
golang -> position 1
search -> position 3
```

Stop-word filtering preserves original positions instead of renumbering them.
That keeps the index and snippet extractor speaking the same language.

## Search Flow

```text
query: "distributed computing"
   |
   v
analysis.Analyze(query)
   |
   v
lookup posting lists
   |
   v
union postings
   |
   v
score each document with TF-IDF
   |
   v
sort by score descending
   |
   v
extract snippets from Posting.Positions
   |
   v
return []Result
```

Search currently uses OR semantics. If a query has multiple terms, a document is
returned when it contains at least one query term. Ranking then decides which
matching documents appear first.

## Scoring

Each matched document receives a TF-IDF score.

```text
score = term frequency * inverse document frequency

term frequency = term frequency in document / document length
idf = log(total documents / documents containing term)
```

For multi-term queries, the document score is the sum of the score for each
query term.

## Remove Flow

Removing a document has to clean up three pieces of state.

```text
file path
   |
   v
find matching Document.FilePath
   |
   v
delete idx.Docs[docID]
   |
   v
for every term in idx.Postings
   |
   v
remove postings where Posting.DocID == docID
   |
   v
delete term if its PostingList is now empty
   |
   v
TotalDocs--
   |
   v
save to search.idx
```

`NextDocID` stays unchanged after removal. That keeps document IDs append-only,
while `TotalDocs` keeps ranking correct for the documents still in the index.

## List Flow

List reads from the `Docs` map and prints the indexed files. Go map iteration is
randomized, so `ListDocuments` sorts document IDs before returning anything.

```text
idx.Docs map
   |
   v
collect doc IDs into []int
   |
   v
sort IDs ascending
   |
   v
append docs in sorted ID order
   |
   v
print ID, file path, token count
```

## Snippets and Highlighting

Search returns structured results:

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

The CLI prints only `Snippet.Text`, but a frontend can use `Matches` to
highlight only the matching words.

```text
Snippet.Text:
"a search engine runs on a distributed computing system"

Snippet.Matches:
[
  { Start: 26, End: 37, Term: "distributed" },
  { Start: 38, End: 47, Term: "computing" }
]
```

Frontend rendering can then wrap only those ranges:

```html
a search engine runs on a <mark>distributed</mark> <mark>computing</mark> system
```

### Snippet Extraction Diagram

```text
Result.Doc.FilePath
   |
   v
reopen source file
   |
   v
scan each line
   |
   v
analyze line and add line position offset
   |
   v
does any line token position exist in Posting.Positions?
   |
   +-- no  -> continue scanning
   |
   +-- yes -> build short snippet around first match
              find query term character ranges
              return Snippet{Text, Matches}
```

Snippets keep about 60 characters of context around the first match on the
line. The stored `Match.Start` and `Match.End` offsets are relative to the final
snippet text, not the full source line.

## Persistence

The index is stored in `search.idx` using Go's `encoding/gob`.

```text
Load("search.idx")
   |
   +-- file exists     -> decode Index
   +-- file not found  -> NewIndex()

Save("search.idx")
   |
   +-- encode Index to disk
```

Because `search.idx` stores posting positions, rebuild it after changing
tokenization, stop-word filtering, or position logic.

## CLI Usage

Index one file:

```sh
go run ./cmd/searchcli add ../data/web-crawler.txt
```

Index a directory of `.txt` and `.md` files:

```sh
go run ./cmd/searchcli add ../data
```

Remove one indexed file:

```sh
go run ./cmd/searchcli remove ../data/web-crawler.txt
```

List indexed files:

```sh
go run ./cmd/searchcli list
```

Search:

```sh
go run ./cmd/searchcli search "distributed computing"
```

Example output:

```text
3 documents indexed:
  1. ../information-retrieval (2834 tokens)
  2. ../search-engines (1598 tokens)
  3. ../web-crawler (3797 tokens)

Results for "distributed computing":
 1. ../data/search-engines.txt (score: 0.0005)
    ...its engine is part of a distributed computing system that can encompass...
```

## Project Layout

```text
.
├── analysis/
│   ├── analyzer.go     # Tokenize -> normalize -> remove stop words
│   ├── tokenizer.go    # Splits raw text into positioned tokens
│   ├── normalizer.go   # Lowercases tokens
│   └── stopwords.go    # Removes low-value terms without changing positions
├── index/
│   ├── index.go        # Index structure, add/remove documents, search, ranking
│   ├── posting.go      # Posting and PostingList types
│   ├── scorer.go       # TF-IDF scoring
│   ├── snippet.go      # Snippet extraction and highlight ranges
│   └── persist.go      # gob save/load
├── cmd/searchcli/
│   └── main.go         # CLI wrapper around the library
├── searchindex.go      # Root package facade for external imports
└── search.idx          # persisted local index, generated at runtime
```

## Current Limitations

- Query matching uses union/OR behavior, not phrase matching.
- Remove finds documents by exact file path, so use the same path style you used
  when adding the document.
- Snippet highlighting marks each query term independently.
- Tokenization is ASCII-oriented: letters and numbers are token characters.
- The index is loaded and saved as one file, so it is best for learning and
  small local datasets.
- There is no stemming or lemmatization yet.

## Rebuilding the Index

If snippets look stale or point at strange lines, rebuild `search.idx`.

```sh
rm search.idx
go run ./cmd/searchcli add ../data
go run ./cmd/searchcli search "distributed computing"
```
