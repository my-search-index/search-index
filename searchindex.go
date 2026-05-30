// Package searchindex exposes a small file-based search index library.
package searchindex

import "github.com/Stacvirus/search-index/index"

type Index = index.Index
type Document = index.Document
type Result = index.Result
type Snippet = index.Snippet
type Match = index.Match
type Posting = index.Posting
type PostingList = index.PostingList

// New creates an empty in-memory search index.
func New() *Index {
	return index.NewIndex()
}

// Load reads an index from path, or returns an empty index if the file does not exist.
func Load(path string) (*Index, error) {
	return index.Load(path)
}
