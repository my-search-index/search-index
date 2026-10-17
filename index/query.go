package index

import (
	"errors"
	"fmt"
	"strings"

	"github.com/my-search-index/search-index/analysis"
)

var (
	// Sentinel errors returned (wrapped) by parseQuery. Callers can detect the
	// kind of failure with errors.Is while still getting a descriptive message.
	ErrUnterminatedQuote = errors.New("unterminated quote")

	// ErrEmptyPhrase means a quoted phrase contains nothing searchable:
	// it is empty (""), only punctuation, or made up entirely of stopwords.
	ErrEmptyPhrase = errors.New("phrase contains no searchable terms")
)

// phraseTerm is one analyzed word of a phrase plus where it must sit.
//
// Offset is the distance (in raw tokens, stopwords included) from the first
// searchable word of the phrase, which always has Offset 0. Keeping the gap
// means `"search the golang"` yields search@0 and golang@2: the stopword slot
// acts as a wildcard, mirroring how stopwords are removed from documents
// without renumbering their positions.
type phraseTerm struct {
	Word   string
	Offset int
}

// phrase is an ordered sequence of terms that must at the given
// relative offsets inside a single document.
type phrase struct {
	Terms []phraseTerm
}

// parsedQuery is the structured form of a raw query string.
//
//   - Phrases are hard constraints: a document must contain every phrase.
//   - Terms are bare words. They contribute to scoring and, when the query
//     has no phrases at all, define the candidate set (plain OR search).
type parsedQuery struct {
	Phrases []phrase
	Terms   []string
}

// hasPhrases reports whether the query contains at least one phrase.
func (q parsedQuery) hasPhrases() bool {
	return len(q.Phrases) > 0
}

// words returns every analyzed word in the query, phrase words first and in
// query order, then bare terms. Scoring uses this so phrase words count as
// ordinary matched terms.
func (q parsedQuery) words() []string {
	var out []string
	for _, p := range q.Phrases {
		for _, t := range p.Terms {
			out = append(out, t.Word)
		}
	}
	return append(out, q.Terms...)
}

// segment is a raw slice of the query: either quoted (a phrase) or bare.
type segment struct {
	text   string
	quoted bool
	pos    int // byte offset in the raw query where the segment starts (for the opening quote when quoted)
}

// parseQuery turns a raw query string into a parsedQuery.
//
// Phase 1 (splitSegments) separates quoted phrases from bare text. This has
// to happen BEFORE analysis because the tokenizer treats '"' as a separator
// and would silently erase the quotes.
// Phase 2 analyzes each segment with the same pipeline used at index time,
// so query tokens line up with the positions stored in postings.
//
// An empty query (or one that analyzes to nothing, like "the of") is not an
// error: it returns an empty parsedQuery and search yields no results.
func parseQuery(raw string) (parsedQuery, error) {
	segments, err := splitSegments(raw)
	if err != nil {
		return parsedQuery{}, err
	}

	var q parsedQuery
	for _, seg := range segments {
		if seg.quoted {
			p, err := analyzePhrase(seg)
			if err != nil {
				return parsedQuery{}, err
			}
			q.Phrases = append(q.Phrases, p)
			continue
		}
		q.Terms = append(q.Terms, analyzeBare(seg.text)...)
	}
	return q, nil
}

// splitSegments scans the raw query once and cuts it at every double quote.
//
// It is a two-state machine (outside / inside quotes):
//   - '"' outside quotes: flush the buffer as a bare segment, start a phrase.
//   - '"' inside quotes:  flush the buffer as a phrase segment, end the phrase.
//   - anything else:      append to the buffer.
//
// Notes on behaviour:
//   - It walks bytes, not runes. '"' is ASCII and never appears inside a
//     multi-byte UTF-8 sequence, so this is safe and keeps non-ASCII text
//     intact.
//   - A quote is a delimiter wherever it appears: foo"bar baz" gives the bare
//     segment "foo" and the phrase "bar baz". Adjacent phrases ("a b""c d")
//     also work.
//   - Blank bare segments (just whitespace between phrases) are dropped.
//     Blank PHRASE segments ("") are kept so analyzePhrase can reject them.
//   - There is no escape syntax in v3: a literal quote cannot be searched.
func splitSegments(raw string) ([]segment, error) {
	var (
		segments   []segment
		buf        strings.Builder
		bufStart   int  // where the current buffer began
		inQuote    bool // are we between an opening and closing quote:
		quoteStart int  // byte offset of the currently open quote
	)

	flushBare := func() {
		if strings.TrimSpace(buf.String()) != "" {
			segments = append(segments, segment{
				text: buf.String(),
				pos:  bufStart,
			})
		}
		buf.Reset()
	}

	for i := 0; i < len(raw); i++ {
		c := raw[i]

		if c != '"' {
			if buf.Len() == 0 {
				bufStart = i
			}
			buf.WriteByte(c)
			continue
		}

		if inQuote {
			// Closing quote: whatever we buffered is a complete phrase.
			segments = append(segments, segment{
				text:   buf.String(),
				quoted: true,
				pos:    quoteStart,
			})
			buf.Reset()
			inQuote = false
		} else {
			// Opening quote: close off any bare text collected so far.
			flushBare()
			inQuote = true
			quoteStart = i
			bufStart = i + 1
		}
	}

	// Reaching the end while still inside quotes means the user forgot the
	// closing quote. Fail loudly instead of guessing what they meant.
	if inQuote {
		return nil, fmt.Errorf("%w: quote opened at position %d is never closed", ErrUnterminatedQuote, quoteStart)
	}

	flushBare()
	return segments, nil
}

// analyzeBare analyzes unquoted text into plain terms.
//
// It analyzes whitespace-separated words one at a time. The result is the same
// as analyzing the whole string today, but it keeps a seam for term exclusion
// (-term): a leading '-' is only detectable per word, because the tokenizer
// discards hyphens. A word may yield zero tokens (a stopword) or several
// (golang-search -> golang, search); both are fine.
func analyzeBare(text string) []string {
	var terms []string
	for _, word := range strings.Fields(text) {
		for _, token := range analysis.Analyze(word) {
			terms = append(terms, token.Word)
		}
	}
	return terms
}

// analyzePhrase analyzes a quoted segment into a phrase.
//
// The whole segment is analyzed as one unit so token positions reflect the
// real distance between words, stopwords included. Offsets are then made
// relative to the first surviving word.
//
// Consequences worth knowing:
//   - Leading and trailing stopwords are trimmed: "the golang" == "golang".
//     They would not constrain matching anyway.
//   - Interior stopwords keep their gap: "search the golang" -> search@0,
//     golang@2.
//   - A phrase with no surviving words is an error rather than something we
//     silently ignore or treat as "match everything".
func analyzePhrase(seg segment) (phrase, error) {
	tokens := analysis.Analyze(seg.text)
	if len(tokens) == 0 {
		return phrase{}, fmt.Errorf("%w: %q (quote at position %d)", ErrEmptyPhrase, seg.text, seg.pos)
	}

	first := tokens[0].Position
	terms := make([]phraseTerm, len(tokens))
	for i, token := range tokens {
		terms[i] = phraseTerm{Word: token.Word, Offset: token.Position - first}
	}
	return phrase{Terms: terms}, nil
}
