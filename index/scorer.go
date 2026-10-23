package index

import "math"

// BM25 tuning parameters. They are constants so they can be tuned in one place
// without touching the scoring logic below.
const (
	// bm25K1 controls how quickly repeated occurrences of a term stop adding
	// score (term-frequency saturation). Low values flatten the curve after
	// very few occurrences; high values behave more like plain counting.
	// A single term can never contribute more than (bm25K1 + 1) times its IDF.
	bm25K1 = 1.2

	// bm25B controls how strongly document length is normalized: 0 ignores
	// length entirely, 1 applies it fully. Documents longer than the corpus
	// average are discounted, shorter ones get a small boost.
	bm25B = 0.75
)

// Score calculates the relevance of one term (or phrase pseudo-term) in one
// document using BM25.
//
//	score = IDF * freq*(k1+1) / (freq + k1*(1 - b + b*docLength/avgDocLen))
//
// posting.Frequency is the raw number of occurrences, NOT divided by length:
// length normalization happens inside the formula. docListLen is the number of
// documents containing the term, totalDocs the number of indexed documents and
// avgDocLen the average Document.Length across them.
func Score(posting Posting, doc Document, docListLen int, totalDocs int, avgDocLen float64) float64 {
	if posting.Frequency == 0 || docListLen == 0 || totalDocs == 0 {
		return 0.0
	}

	idf := inverseDocumentFrequency(docListLen, totalDocs)
	return idf * termWeight(posting.Frequency, doc.Length, avgDocLen)
}

// termWeight is the saturation, length-normalized term-frequency part of BM25.
// At an average-length document a single occurrence weights exactly 1, and the
// weight approaches (bm25K1 + 1) as frequency grows.
func termWeight(frequecy int, docLenght int, avgDocLen float64) float64 {
	freq := float64(frequecy)

	// Without a usable average (empty index) there is nothing to compare the
	// document against, so skip length normalization instead of dividing by 0.
	lengthNorm := 1.0
	if avgDocLen > 0 {
		lengthNorm = 1 - bm25B + bm25B*float64(docLenght)/avgDocLen
	}
	return freq * (bm25K1 + 1) / (freq + bm25K1*lengthNorm)
}

// inverseDocumentFrequency is the BM25 IDF variant: ln(1 + (N-df+0.5)/(df+0.5)).
// Rarer terms score higher, and unlike ln(N/df) the result is always positive.
// so a term present in every document still contributes a little instead of
// exactly zero (and the textbook form without the "1 +" can go negative).
func inverseDocumentFrequency(docListLen int, totalDocs int) float64 {
	n := float64(totalDocs)
	df := float64(docListLen)
	if df > n {
		df = n // a stale total can't make the IDF negative
	}
	return math.Log(1 + (n-df+0.5)/(df+0.5))
}
