package index

import (
	"bufio"
	"os"
	"sort"
	"strings"

	"github.com/my-search-index/search-index/analysis"
)

const snippetContext = 60

// ExtractSnippets finds the source lines that contain this result's query
// matches. It uses Posting.Positions as document-wide token offsets, reopens
// the document, and returns short snippets with highlight ranges.
func (idx *Index) ExtractSnippets(result Result, query string) Result {
	tokens := analysis.Analyze(query)
	targetPositions := make(map[int]bool)
	terms := uniqueTerms(tokens)

	for _, token := range tokens {
		posting := searchPosting(idx.Postings[token.Word], result.Doc.ID)
		if posting.DocID == 0 {
			continue
		}
		for _, pos := range posting.Positions {
			targetPositions[pos] = true
		}
	}

	if len(targetPositions) == 0 {
		return result
	}

	file, err := os.Open(result.Doc.FilePath)
	if err != nil {
		return result
	}
	defer file.Close()

	positionOffset := 0
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		lineTokens := analysis.Analyze(line)

		for _, lineToken := range lineTokens {
			if targetPositions[positionOffset+lineToken.Position] {
				result.Snippets = append(result.Snippets, buildSnippet(line, terms))
				break
			}
		}
		positionOffset += len(analysis.Tokenize(line))
	}

	return result
}

func uniqueTerms(tokens []analysis.Token) []string {
	seen := make(map[string]bool)
	var terms []string
	for _, token := range tokens {
		if !seen[token.Word] {
			terms = append(terms, token.Word)
			seen[token.Word] = true
		}
	}
	return terms
}

func buildSnippet(line string, terms []string) Snippet {
	matches := findMatches(line, terms)
	if len(matches) == 0 {
		return Snippet{Text: line}
	}

	start, end := snippetWindow(line, matches[0])
	snippet := Snippet{Text: line[start:end]}
	for _, match := range matches {
		if match.Start < start || match.End > end {
			continue
		}
		snippet.Matches = append(snippet.Matches, Match{
			Start: match.Start - start,
			End:   match.End - start,
			Term:  match.Term,
		})
	}
	return snippet
}

func findMatches(line string, terms []string) []Match {
	lowerLine := strings.ToLower(line)
	var matches []Match

	for _, term := range terms {
		startAt := 0
		for {
			relativeStart := strings.Index(lowerLine[startAt:], term)
			if relativeStart == -1 {
				break
			}

			start := startAt + relativeStart
			end := start + len(term)
			if isTokenBoundary(line, start, end) {
				matches = append(matches, Match{
					Start: start,
					End:   end,
					Term:  term,
				})
			}
			startAt = end
		}
	}

	sort.Slice(matches, func(i, j int) bool {
		return matches[i].Start < matches[j].Start
	})
	return matches
}

func snippetWindow(line string, firstMatch Match) (int, int) {
	start := firstMatch.Start - snippetContext
	if start < 0 {
		start = 0
	}
	end := firstMatch.End + snippetContext
	if end > len(line) {
		end = len(line)
	}
	return start, end
}

func isTokenBoundary(line string, start, end int) bool {
	beforeOK := start == 0 || !isTokenChar(line[start-1])
	afterOK := end == len(line) || !isTokenChar(line[end])
	return beforeOK && afterOK
}

func isTokenChar(char byte) bool {
	return (char >= 'a' && char <= 'z') ||
		(char >= 'A' && char <= 'Z') ||
		(char >= '0' && char <= '9')
}
