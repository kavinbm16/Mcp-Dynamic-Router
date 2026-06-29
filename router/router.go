package router

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"
)

type compiledIndex struct {
	version       uint64
	tools         []Tool
	toolDocuments []string
	toolLexical   *lexicalIndex
	toolVectors   [][]float32
	domains       []string
	domainDocs    []string
	domainLexical *lexicalIndex
	domainVectors [][]float32
	domainTools   map[string][]int
}

// Router is safe for concurrent Route calls. Refresh creates a new immutable
// index and swaps it in only after compilation succeeds.
type Router struct {
	registry *Registry
	embedder Embedder
	reranker Reranker
	cfg      Config
	mu       sync.RWMutex
	index    *compiledIndex
}

func New(registry *Registry, embedder Embedder, cfg Config, options ...Option) *Router {
	defaults := DefaultConfig()
	if cfg.MaxDomainTools <= 0 {
		cfg.MaxDomainTools = defaults.MaxDomainTools
	}
	if cfg.DomainTopK <= 0 {
		cfg.DomainTopK = defaults.DomainTopK
	}
	if cfg.CandidateTopK <= 0 {
		cfg.CandidateTopK = defaults.CandidateTopK
	}
	if cfg.LexicalWeight == 0 && cfg.SemanticWeight == 0 {
		cfg.LexicalWeight, cfg.SemanticWeight = defaults.LexicalWeight, defaults.SemanticWeight
	}
	if cfg.MinConfidence == 0 {
		cfg.MinConfidence = defaults.MinConfidence
	}
	if cfg.MinMargin == 0 {
		cfg.MinMargin = defaults.MinMargin
	}
	if cfg.RerankerWeight == 0 {
		cfg.RerankerWeight = defaults.RerankerWeight
	}
	result := &Router{registry: registry, embedder: embedder, cfg: cfg}
	for _, option := range options {
		option(result)
	}
	return result
}

type Option func(*Router)

func WithReranker(reranker Reranker) Option {
	return func(router *Router) { router.reranker = reranker }
}

func (r *Router) Refresh(ctx context.Context) error {
	snapshot := r.registry.Snapshot()
	if len(snapshot.Tools) == 0 {
		return fmt.Errorf("cannot build router index: registry is empty")
	}

	index := &compiledIndex{version: snapshot.Version, tools: snapshot.Tools, domainTools: make(map[string][]int)}
	domainDocuments := make(map[string][]string)
	for toolIndex, tool := range snapshot.Tools {
		domain := ExtractDomain(tool)
		index.domainTools[domain] = append(index.domainTools[domain], toolIndex)
		document := tool.Name + " " + tool.Description
		index.toolDocuments = append(index.toolDocuments, document)
		domainDocuments[domain] = append(domainDocuments[domain], document)
	}
	for domain := range domainDocuments {
		index.domains = append(index.domains, domain)
	}
	sort.Strings(index.domains)
	for _, domain := range index.domains {
		index.domainDocs = append(index.domainDocs, domain+" "+strings.Join(domainDocuments[domain], " "))
	}
	index.toolLexical = newLexicalIndex(index.toolDocuments)
	index.domainLexical = newLexicalIndex(index.domainDocs)

	if r.embedder != nil {
		toolVectors, err := r.embedder.Embed(ctx, index.toolDocuments)
		if err != nil {
			return fmt.Errorf("embed tool descriptions: %w", err)
		}
		domainVectors, err := r.embedder.Embed(ctx, index.domainDocs)
		if err != nil {
			return fmt.Errorf("embed domain descriptions: %w", err)
		}
		if len(toolVectors) != len(index.tools) || len(domainVectors) != len(index.domains) {
			return fmt.Errorf("embedder returned an unexpected vector count")
		}
		index.toolVectors, index.domainVectors = toolVectors, domainVectors
	}

	r.mu.Lock()
	r.index = index
	r.mu.Unlock()
	return nil
}

func (r *Router) Route(ctx context.Context, request RouteRequest) (RouteResult, error) {
	started := time.Now()
	r.mu.RLock()
	index := r.index
	r.mu.RUnlock()
	if index == nil {
		return RouteResult{}, fmt.Errorf("router index is not built; call Refresh first")
	}
	query := strings.TrimSpace(request.Utterance)
	if r.cfg.IncludeContext && strings.TrimSpace(request.Context) != "" {
		query += " context " + request.Context
	}
	if query == "" {
		return RouteResult{Decision: DecisionNoTool, Reason: "empty utterance"}, nil
	}

	domainStarted := time.Now()
	domainLexical := normalizeScores(index.domainLexical.scores(query))
	domainSemantic, queryVector, usedEmbeddings, err := r.semanticScores(ctx, query, index.domainVectors)
	if err != nil {
		return RouteResult{}, err
	}
	lexicalWeight, semanticWeight := r.weights(usedEmbeddings)
	domainScores := fuse(domainLexical, domainSemantic, lexicalWeight, semanticWeight)
	domainRanks := rankedIndices(domainScores)
	toolLexical := normalizeScores(index.toolLexical.scores(query))
	toolSemantic := make([]float64, len(index.tools))
	if usedEmbeddings {
		for toolIndex, vector := range index.toolVectors {
			toolSemantic[toolIndex] = cosine01(queryVector, vector)
		}
	}
	preliminaryToolScores := fuse(toolLexical, toolSemantic, lexicalWeight, semanticWeight)
	selectedDomains, allowedTools := r.selectDomains(index, domainRanks, preliminaryToolScores)
	domainLatency := time.Since(domainStarted)

	selectStarted := time.Now()
	toolScores := preliminaryToolScores
	if r.reranker != nil {
		shortlist := make([]Tool, 0, len(allowedTools))
		for toolIndex := range allowedTools {
			shortlist = append(shortlist, index.tools[toolIndex])
		}
		reranked, rerankErr := r.reranker.Rerank(ctx, request.Utterance, request.Context, shortlist)
		if rerankErr != nil {
			return RouteResult{}, fmt.Errorf("rerank shortlist: %w", rerankErr)
		}
		weight := math.Max(0, math.Min(1, r.cfg.RerankerWeight))
		for toolIndex := range allowedTools {
			if score, exists := reranked[index.tools[toolIndex].ID]; exists {
				toolScores[toolIndex] = (1-weight)*toolScores[toolIndex] + weight*math.Max(0, math.Min(1, score))
			}
		}
	}
	candidates := make([]Candidate, 0, len(allowedTools))
	for toolIndex := range allowedTools {
		candidates = append(candidates, Candidate{Tool: index.tools[toolIndex], Score: toolScores[toolIndex], LexicalScore: toolLexical[toolIndex], SemanticScore: toolSemantic[toolIndex]})
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].Score == candidates[j].Score {
			return candidates[i].Tool.ID < candidates[j].Tool.ID
		}
		return candidates[i].Score > candidates[j].Score
	})
	if len(candidates) > r.cfg.CandidateTopK {
		candidates = candidates[:r.cfg.CandidateTopK]
	}
	for candidateIndex := range candidates {
		candidates[candidateIndex].Rank = candidateIndex + 1
	}
	selectLatency := time.Since(selectStarted)

	result := decide(candidates, r.cfg)
	result.Trace = Trace{RegistryVersion: index.version, Domains: selectedDomains, DomainLatency: domainLatency, SelectLatency: selectLatency, TotalLatency: time.Since(started), UsedEmbeddings: usedEmbeddings, UsedReranker: r.reranker != nil}
	return result, nil
}

func (r *Router) semanticScores(ctx context.Context, query string, vectors [][]float32) ([]float64, []float32, bool, error) {
	if r.embedder == nil || len(vectors) == 0 {
		return make([]float64, len(vectors)), nil, false, nil
	}
	queryVectors, err := r.embedder.Embed(ctx, []string{query})
	if err != nil {
		return nil, nil, false, fmt.Errorf("embed query: %w", err)
	}
	if len(queryVectors) != 1 {
		return nil, nil, false, fmt.Errorf("embedder returned %d query vectors", len(queryVectors))
	}
	scores := make([]float64, len(vectors))
	for index, vector := range vectors {
		scores[index] = cosine01(queryVectors[0], vector)
	}
	return scores, queryVectors[0], true, nil
}

func (r *Router) weights(semantic bool) (float64, float64) {
	if !semantic {
		return 1, 0
	}
	total := r.cfg.LexicalWeight + r.cfg.SemanticWeight
	return r.cfg.LexicalWeight / total, r.cfg.SemanticWeight / total
}

func (r *Router) selectDomains(index *compiledIndex, ranks []int, toolScores []float64) ([]string, map[int]struct{}) {
	domains, allowed := make([]string, 0, r.cfg.DomainTopK), make(map[int]struct{})
	for _, rank := range ranks {
		if len(domains) >= r.cfg.DomainTopK || len(allowed) >= r.cfg.MaxDomainTools {
			break
		}
		domain := index.domains[rank]
		domains = append(domains, domain)
		toolIndices := append([]int(nil), index.domainTools[domain]...)
		sort.SliceStable(toolIndices, func(i, j int) bool {
			if toolScores[toolIndices[i]] == toolScores[toolIndices[j]] {
				return index.tools[toolIndices[i]].ID < index.tools[toolIndices[j]].ID
			}
			return toolScores[toolIndices[i]] > toolScores[toolIndices[j]]
		})
		for _, toolIndex := range toolIndices {
			if len(allowed) >= r.cfg.MaxDomainTools {
				break
			}
			allowed[toolIndex] = struct{}{}
		}
	}
	return domains, allowed
}

func fuse(lexical, semantic []float64, lexicalWeight, semanticWeight float64) []float64 {
	result := make([]float64, len(lexical))
	for index := range result {
		result[index] = lexicalWeight * lexical[index]
		if index < len(semantic) {
			result[index] += semanticWeight * semantic[index]
		}
	}
	return result
}

func decide(candidates []Candidate, cfg Config) RouteResult {
	if len(candidates) == 0 {
		return RouteResult{Decision: DecisionNoTool, Reason: "no tools in selected domains"}
	}
	confidence, margin := candidates[0].Score, candidates[0].Score
	if len(candidates) > 1 {
		margin -= candidates[1].Score
	}
	result := RouteResult{Confidence: confidence, Margin: margin, Candidates: candidates}
	switch {
	case confidence < cfg.MinConfidence:
		result.Decision, result.Reason = DecisionNoTool, "top candidate is below the confidence threshold"
	case margin < cfg.MinMargin:
		result.Decision, result.Reason = DecisionClarify, "top candidates are too close to choose safely"
	default:
		result.Decision, result.Reason = DecisionSelected, "top candidate passed confidence and margin thresholds"
	}
	return result
}

func cosine01(left, right []float32) float64 {
	if len(left) == 0 || len(left) != len(right) {
		return 0
	}
	var dot, leftNorm, rightNorm float64
	for index := range left {
		dot += float64(left[index] * right[index])
		leftNorm += float64(left[index] * left[index])
		rightNorm += float64(right[index] * right[index])
	}
	if leftNorm == 0 || rightNorm == 0 {
		return 0
	}
	cosine := dot / math.Sqrt(leftNorm*rightNorm)
	return math.Max(0, math.Min(1, (cosine+1)/2))
}
