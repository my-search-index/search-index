package index

import (
	"math"
	"testing"
)

func almostEqual(a, b float64) bool { return math.Abs(a-b) < 1e-5 }

func TestInverseDocumentFrequency(t *testing.T) {
	tests := []struct {
		name      string
		listLen   int
		totalDocs int
		want      float64
	}{
		{"rare term", 1, 10, 1.99243},
		{"fairly rare term", 2, 10, 1.481605},
		{"term in half the documents", 5, 10, math.Log(2)},
		{"term in every document is small but positive", 10, 10, 0.04652},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := inverseDocumentFrequency(tt.listLen, tt.totalDocs)

			if !almostEqual(got, tt.want) {
				t.Errorf("idf(%d, %d) = %f, want %f", tt.listLen, tt.totalDocs, got, tt.want)
			}
		})
	}

	t.Run("always positive and decreasing as the term gets more common", func(t *testing.T) {
		previous := math.Inf(1)
		for df := 1; df <= 100; df++ {
			got := inverseDocumentFrequency(df, 100)

			if got <= 0 {
				t.Fatalf("idf(%d, 100) = %f, want > 0", df, got)
			}
			if got >= previous {
				t.Fatalf("idf(%d, 100) = %f should be below idf(%d, 100) = %f", df, got, df-1, previous)
			}
			previous = got
		}
	})

	t.Run("a stale total never produces a negative or NaN value", func(t *testing.T) {
		got := inverseDocumentFrequency(5, 3) // more documents contain the term than exist

		if got <= 0 || math.IsNaN(got) || math.IsInf(got, 0) {
			t.Errorf("idf(5, 3) = %f, want a finite positive value", got)
		}
	})
}

func TestTermWeight(t *testing.T) {
	const avg = 100.0

	t.Run("saturates as frequency grows (average-length document)", func(t *testing.T) {
		tests := []struct {
			frequency int
			want      float64
		}{
			{1, 1.0},
			{2, 1.375},
			{5, 1.7741935},
			{10, 1.9642857},
			{50, 2.1484375},
		}

		for _, tt := range tests {
			got := termWeight(tt.frequency, 100, avg)
			if !almostEqual(got, tt.want) {
				t.Errorf("termWeight(%d) = %f, want %f", tt.frequency, got, tt.want)
			}
		}
	})

	t.Run("never exceeds k1+1, however often the term repeats", func(t *testing.T) {
		got := termWeight(1_000_000, 100, avg)

		if got >= bm25K1+1 {
			t.Errorf("termWeight(1e6) = %f, must stay below %f", got, bm25K1+1)
		}
	})

	t.Run("always increases with frequency, with shrinking gains", func(t *testing.T) {
		previous, previousGain := 0.0, math.Inf(1)
		for freq := 1; freq <= 30; freq++ {
			got := termWeight(freq, 100, avg)
			gain := got - previous

			if gain <= 0 {
				t.Fatalf("termWeight(%d) = %f did not increase", freq, got)
			}
			if freq > 1 && gain >= previousGain {
				t.Fatalf("gain at %d (%f) should be smaller than at %d (%f)", freq, gain, freq-1, previousGain)
			}
			previous, previousGain = got, gain
		}
	})

	t.Run("is far flatter than linear counting", func(t *testing.T) {
		// 50 occurrences are 10x as many as 5. Linear TF would score them 10x
		// higher; BM25 must score them barely more than 1.5x higher.
		ratio := termWeight(50, 100, avg) / termWeight(5, 100, avg)

		if ratio >= 1.5 {
			t.Errorf("50 vs 5 occurrences ratio = %f, want < 1.5 (linear would be 10)", ratio)
		}
	})

	t.Run("a shorter document scores higher for the same count", func(t *testing.T) {
		short := termWeight(3, 50, 200)
		long := termWeight(3, 500, 200)

		if !almostEqual(short, 1.87234) || !almostEqual(long, 1.189189) {
			t.Errorf("termWeight(3) short=%f long=%f, want 1.87234 and 1.189189", short, long)
		}
		if short <= long {
			t.Errorf("short document (%f) should outscore long document (%f)", short, long)
		}
	})

	t.Run("an average-length document is left alone", func(t *testing.T) {
		if got := termWeight(1, 200, 200); !almostEqual(got, 1.0) {
			t.Errorf("termWeight(1, avg, avg) = %f, want 1", got)
		}
	})

	t.Run("no usable average disables length normalization instead of dividing by zero", func(t *testing.T) {
		got := termWeight(2, 10, 0)

		if math.IsNaN(got) || math.IsInf(got, 0) || !almostEqual(got, 1.375) {
			t.Errorf("termWeight(2, 10, 0) = %f, want 1.375", got)
		}
	})
}

func TestScore(t *testing.T) {
	doc := Document{ID: 1, Length: 100}
	posting := Posting{DocID: 1, Frequency: 2}

	t.Run("is IDF times the term weight", func(t *testing.T) {
		got := Score(posting, doc, 2, 10, 100)
		want := inverseDocumentFrequency(2, 10) * termWeight(2, 100, 100)

		if !almostEqual(got, want) {
			t.Errorf("Score() = %f, want %f", got, want)
		}
	})

	t.Run("zero when there is nothing to score", func(t *testing.T) {
		tests := []struct {
			name      string
			posting   Posting
			listLen   int
			totalDocs int
		}{
			{"term absent from the document", Posting{}, 2, 10},
			{"term in no documents", posting, 0, 10},
			{"empty index", posting, 2, 0},
		}

		for _, tt := range tests {
			if got := Score(tt.posting, doc, tt.listLen, tt.totalDocs, 100); got != 0 {
				t.Errorf("%s: Score() = %f, want 0", tt.name, got)
			}
		}
	})

	t.Run("a rarer term scores higher than a common one", func(t *testing.T) {
		rare := Score(posting, doc, 1, 100, 100)
		common := Score(posting, doc, 80, 100, 100)

		if rare <= common {
			t.Errorf("rare (%f) should outscore common (%f)", rare, common)
		}
	})

	t.Run("a term in every document still scores above zero", func(t *testing.T) {
		got := Score(posting, doc, 10, 10, 100)

		if got <= 0 {
			t.Errorf("Score() = %f, want > 0", got)
		}
	})

	t.Run("same count, shorter document wins", func(t *testing.T) {
		short := Score(Posting{Frequency: 3}, Document{Length: 50}, 2, 10, 200)
		long := Score(Posting{Frequency: 3}, Document{Length: 500}, 2, 10, 200)

		if short < long {
			t.Errorf("short (%f) should score at least as high as long (%f)", short, long)
		}
	})
}
