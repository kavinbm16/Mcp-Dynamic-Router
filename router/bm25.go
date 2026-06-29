package router

import (
	"math"
	"regexp"
	"sort"
	"strings"
)

var tokenPattern = regexp.MustCompile(`[\p{L}\p{N}][\p{L}\p{N}_+.-]*`)

var stopWords = map[string]struct{}{
	"a": {}, "an": {}, "and": {}, "for": {}, "i": {}, "is": {}, "me": {}, "my": {},
	"of": {}, "please": {}, "the": {}, "to": {}, "tool": {}, "use": {}, "want": {},
}

func tokenize(text string) []string {
	words := tokenPattern.FindAllString(strings.ToLower(text), -1)
	result := words[:0]
	for _, word := range words {
		if _, ignored := stopWords[word]; !ignored {
			result = append(result, word)
		}
	}
	return result
}

type lexicalIndex struct {
	documents [][]string
	idf       map[string]float64
	avgLength float64
}

func newLexicalIndex(texts []string) *lexicalIndex {
	index := &lexicalIndex{idf: make(map[string]float64)}
	documentFrequency := make(map[string]int)
	for _, text := range texts {
		tokens := tokenize(text)
		index.documents = append(index.documents, tokens)
		index.avgLength += float64(len(tokens))
		seen := make(map[string]struct{})
		for _, token := range tokens {
			seen[token] = struct{}{}
		}
		for token := range seen {
			documentFrequency[token]++
		}
	}
	if len(texts) > 0 {
		index.avgLength /= float64(len(texts))
	}
	for token, frequency := range documentFrequency {
		index.idf[token] = math.Log(1 + (float64(len(texts)-frequency)+0.5)/(float64(frequency)+0.5))
	}
	return index
}

func (i *lexicalIndex) scores(query string) []float64 {
	scores := make([]float64, len(i.documents))
	if len(i.documents) == 0 || i.avgLength == 0 {
		return scores
	}
	queryTokens := tokenize(query)
	for docIndex, document := range i.documents {
		frequency := make(map[string]int)
		for _, token := range document {
			frequency[token]++
		}
		for _, token := range queryTokens {
			count := float64(frequency[token])
			if count == 0 {
				continue
			}
			const k1, b = 1.2, 0.75
			denominator := count + k1*(1-b+b*float64(len(document))/i.avgLength)
			scores[docIndex] += i.idf[token] * count * (k1 + 1) / denominator
		}
	}
	return scores
}

func normalizeScores(scores []float64) []float64 {
	result := append([]float64(nil), scores...)
	maximum := 0.0
	for _, score := range result {
		if score > maximum {
			maximum = score
		}
	}
	if maximum == 0 {
		return result
	}
	for index := range result {
		result[index] /= maximum
	}
	return result
}

func rankedIndices(scores []float64) []int {
	indices := make([]int, len(scores))
	for index := range indices {
		indices[index] = index
	}
	sort.SliceStable(indices, func(i, j int) bool {
		if scores[indices[i]] == scores[indices[j]] {
			return indices[i] < indices[j]
		}
		return scores[indices[i]] > scores[indices[j]]
	})
	return indices
}
