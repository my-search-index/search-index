package index

import (
	"bufio"
	"os"
	"strings"
	"unicode/utf8"
)

// snippetContext is how many bytes of the line are kept on each side of
// the first match in a snippet.
const (
	snippetContext = 60

	// snippetResultLimit caps how many results (best first) get snippets.
	// Every snippet reopens and scans a source file, so extracting them for a
	// broad OR query over hundreds of documents would dominate the cost of a
	// search. Results beyond the limit are still returned, without snippets.
	snippetResultLimit = 20
)

// tokenSpan is the byte range [start, end] of one raw token inside a line.
type tokenSpan struct {
	start int
	end   int
}

// extractSnippets returns one snippet per source line that contains a match.
//
// postings are the postings of the query words for this document, already
// fetched by ranking. Their Positions are document-wide raw-token offsets (the
// same numbering AddDocument used). Rather than searching the line for the
// query text again, we walk the line's tokens, number them the way indexing
// did, and highlight exactly the tokens whose position is a match. That is
// what makes highlighting precise: it can never mark a word that was not
// matched, and it does not depend on the matched word looking like the query
// word (so it keeps working if stemming is added to analysis).
//
// Failures to read the file simply produce no snippets; the result itself is
// still valid.
func extractSnippets(doc Document, postings []Posting) []Snippet {
	targets, last := targetPositions(postings)
	if len(targets) == 0 {
		return nil
	}

	file, err := os.Open(doc.FilePath)
	if err != nil {
		return nil
	}
	defer file.Close()

	var snippets []Snippet
	offset := 0 // raw tokens seen before the current line
	scanner := bufio.NewScanner(file)

	// Once offset passes the last target position, every remaining line is
	// behind us: stop reading instead of scanning to the end of the file.
	for offset <= last && scanner.Scan() {
		line := scanner.Text()
		spans := tokenSpans(line)

		var matches []Match
		for i, span := range spans {
			if _, hit := targets[offset+i]; hit {
				matches = append(matches, Match{
					Start: span.start,
					End:   span.end,
					Term:  strings.ToLower(line[span.start:span.end]),
				})
			}
		}
		offset += len(spans)

		if len(matches) > 0 {
			snippets = append(snippets, buildSnippet(line, matches))
		}
	}
	return snippets
}

// targetPositions collects every matched position into a set and also returns
// the largest one, which is the early-stop point for the file scan
func targetPositions(postings []Posting) (map[int]struct{}, int) {
	targets := make(map[int]struct{})
	last := -1
	for _, posting := range postings {
		for _, pos := range posting.Positions {
			targets[pos] = struct{}{}
			if pos > last {
				last = pos
			}
		}
	}
	return targets, last
}

// tokenSpans splits a line into raw tokens (runs of ASCII letters and digits)
// and reports where each one sits. The count and order MUST match
// analysis.Tokenize, because token i of the line is document position
// offset+i. TestTokenSpansMatchTokenizer guards that invariant.
func tokenSpans(line string) []tokenSpan {
	var spans []tokenSpan
	start := -1
	for i := 0; i < len(line); i++ {
		if isTokenChar(line[i]) {
			if start < 0 {
				start = i
			}
		} else if start >= 0 {
			spans = append(spans, tokenSpan{start: start, end: i})
			start = -1
		}
	}
	if start >= 0 {
		spans = append(spans, tokenSpan{start: start, end: len(line)})
	}
	return spans
}

// buildSnippet cuts a window around the first match of the line and keeps
// the highlight ranges (relative to the window) of every match inside it.
// matches must be in ascending order, which tokenSpans guarantees.
func buildSnippet(line string, matches []Match) Snippet {
	start, end := snippetWindow(line, matches[0])
	snippet := Snippet{Text: line[start:end]}

	for _, match := range matches {
		if match.Start < start || match.End > end {
			continue // match falls outside the window
		}
		snippet.Matches = append(snippet.Matches, Match{
			Start: match.Start - start,
			End:   match.End - start,
			Term:  match.Term,
		})
	}
	return snippet
}

// snippetWindow returns the byte range to display around the first match.
// The edges are moved outward to rune boundaries so a multi-byte character is
// never cut in half (which would produce invalid UTF-8 in the output).
func snippetWindow(line string, first Match) (int, int) {
	start := first.Start - snippetContext
	if start < 0 {
		start = 0
	}
	end := first.End + snippetContext
	if end > len(line) {
		end = len(line)
	}

	for start > 0 && !utf8.RuneStart(line[start]) {
		start--
	}
	for end < len(line) && !utf8.RuneStart(line[end]) {
		end++
	}
	return start, end
}

// isTokenChar reports whether char is part of a token. Keep in sync with the
// tokenizer's split pattern in the analysis package.
func isTokenChar(char byte) bool {
	return (char >= 'a' && char <= 'z') ||
		(char >= 'A' && char <= 'Z') ||
		(char >= '0' && char <= '9')
}
