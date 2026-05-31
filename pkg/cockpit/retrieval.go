package cockpit

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"

	contextmgr "klyra/pkg/context"
)

const (
	maxChunkBytes       = 2400
	minChunkBytes       = 320
	embeddingDimensions = 384
)

type retrievalConfig struct {
	MaxTokens     int
	MaxChunks     int
	MaxFiles      int
	UseEmbeddings bool
	UseReranker   bool
	RepoMap       string
}

type retrievalChunk struct {
	Path      string
	StartLine int
	EndLine   int
	Text      string
	Terms     map[string]int
	Tokens    int
	Score     float64
	Reason    string
}

func buildRetrievalCart(ctx context.Context, cfg retrievalConfig, cwd, query string) (string, []string) {
	queryTerms := retrievalTerms(query)
	if len(queryTerms) == 0 {
		return "", nil
	}
	if cfg.MaxTokens <= 0 {
		cfg.MaxTokens = 1000
	}
	if cfg.MaxChunks <= 0 {
		cfg.MaxChunks = 10
	}
	if cfg.MaxFiles <= 0 {
		cfg.MaxFiles = DefaultMaxFiles
	}

	chunks, warnings := collectRetrievalChunks(ctx, cwd, cfg.MaxFiles)
	if len(chunks) == 0 {
		return "", warnings
	}
	astHints := repoMapHints(cfg.RepoMap)
	idf := inverseDocumentFrequency(chunks)
	avgLen := averageChunkTerms(chunks)
	queryEmbedding := embeddingVector(query)
	for i := range chunks {
		embeddingScore := 0.0
		if cfg.UseEmbeddings {
			embeddingScore = cosineSparse(queryEmbedding, embeddingVector(chunks[i].Path+" "+chunks[i].Text))
		}
		chunks[i].Score, chunks[i].Reason = scoreChunk(chunks[i], queryTerms, idf, avgLen, astHints, embeddingScore)
	}
	sort.SliceStable(chunks, func(i, j int) bool {
		if chunks[i].Score == chunks[j].Score {
			if chunks[i].Path == chunks[j].Path {
				return chunks[i].StartLine < chunks[j].StartLine
			}
			return chunks[i].Path < chunks[j].Path
		}
		return chunks[i].Score > chunks[j].Score
	})

	selected := selectRetrievalChunks(chunks, cfg.MaxChunks, cfg.MaxTokens)
	if len(selected) == 0 {
		return "", warnings
	}

	var lines []string
	lines = append(lines, fmt.Sprintf("query: %s", strings.TrimSpace(query)))
	lines = append(lines, fmt.Sprintf("budget: %d tokens / %d chunks", cfg.MaxTokens, cfg.MaxChunks))
	lines = append(lines, fmt.Sprintf("embeddings: %s", embeddingMode(cfg.UseEmbeddings)))
	lines = append(lines, fmt.Sprintf("reranker: %s", onOff(cfg.UseReranker)))
	if cfg.UseReranker {
		lines = append(lines, "note: reranker is configured on, but no external reranker is wired in this MVP")
	}
	for i, chunk := range selected {
		lines = append(lines, fmt.Sprintf("%d. %s:%d-%d score=%.2f tokens=%d", i+1, chunk.Path, chunk.StartLine, chunk.EndLine, chunk.Score, chunk.Tokens))
		lines = append(lines, "   why: "+chunk.Reason)
		lines = append(lines, indentSnippet(chunk.Text, "   "))
	}
	return strings.Join(lines, "\n"), warnings
}

func collectRetrievalChunks(ctx context.Context, cwd string, maxFiles int) ([]retrievalChunk, []string) {
	deadline, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	type candidate struct {
		path string
		size int64
	}
	var candidates []candidate
	var warnings []string
	err := filepath.WalkDir(cwd, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			warnings = append(warnings, "retrieval walk: "+walkErr.Error())
			return nil
		}
		if err := deadline.Err(); err != nil {
			return err
		}
		if entry.IsDir() {
			if path != cwd && retrievalSkipDir(entry.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			warnings = append(warnings, "retrieval stat: "+err.Error())
			return nil
		}
		rel, err := filepath.Rel(cwd, path)
		if err != nil {
			warnings = append(warnings, "retrieval relpath: "+err.Error())
			return nil
		}
		rel = filepath.ToSlash(rel)
		if retrievalSkipPath(rel, info) {
			return nil
		}
		candidates = append(candidates, candidate{path: rel, size: info.Size()})
		return nil
	})
	if err != nil && err != context.DeadlineExceeded {
		warnings = append(warnings, "retrieval walk: "+err.Error())
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		left, right := retrievalFileScore(candidates[i].path), retrievalFileScore(candidates[j].path)
		if left == right {
			if candidates[i].size == candidates[j].size {
				return candidates[i].path < candidates[j].path
			}
			return candidates[i].size < candidates[j].size
		}
		return left > right
	})
	if len(candidates) > maxFiles {
		candidates = candidates[:maxFiles]
	}

	var chunks []retrievalChunk
	for _, file := range candidates {
		if err := deadline.Err(); err != nil {
			warnings = append(warnings, "retrieval timed out")
			break
		}
		data, err := os.ReadFile(filepath.Join(cwd, filepath.FromSlash(file.path)))
		if err != nil {
			warnings = append(warnings, "retrieval read: "+err.Error())
			continue
		}
		if looksBinary(data) {
			continue
		}
		chunks = append(chunks, chunkText(file.path, string(data))...)
	}
	return chunks, warnings
}

func chunkText(path, text string) []retrievalChunk {
	lines := strings.Split(text, "\n")
	var chunks []retrievalChunk
	start := 0
	bytes := 0
	for i, line := range lines {
		bytes += len(line) + 1
		blankBoundary := strings.TrimSpace(line) == "" && bytes >= minChunkBytes
		if bytes >= maxChunkBytes || blankBoundary {
			chunks = appendChunk(chunks, path, lines[start:i+1], start+1)
			start = i + 1
			bytes = 0
		}
	}
	if start < len(lines) {
		chunks = appendChunk(chunks, path, lines[start:], start+1)
	}
	return chunks
}

func appendChunk(chunks []retrievalChunk, path string, lines []string, startLine int) []retrievalChunk {
	text := strings.TrimSpace(strings.Join(lines, "\n"))
	if text == "" {
		return chunks
	}
	return append(chunks, retrievalChunk{
		Path:      path,
		StartLine: startLine,
		EndLine:   startLine + strings.Count(text, "\n"),
		Text:      text,
		Terms:     termCounts(text + " " + path),
		Tokens:    contextmgr.EstimateTokens(text),
	})
}

func scoreChunk(chunk retrievalChunk, queryTerms []string, idf map[string]float64, avgLen float64, astHints map[string]bool, embeddingScore float64) (float64, string) {
	const k1 = 1.2
	const b = 0.75
	docLen := float64(sumTerms(chunk.Terms))
	if avgLen <= 0 {
		avgLen = 1
	}
	score := 0.0
	var matches []string
	for _, term := range queryTerms {
		tf := float64(chunk.Terms[term])
		if tf == 0 {
			continue
		}
		score += idf[term] * (tf * (k1 + 1)) / (tf + k1*(1-b+b*docLen/avgLen))
		matches = append(matches, term)
	}
	var boosts []string
	if astHints[chunk.Path] {
		score += 1.4
		boosts = append(boosts, "repo-map path")
	}
	for _, term := range queryTerms {
		if strings.Contains(strings.ToLower(chunk.Path), term) {
			score += 1.0
			boosts = append(boosts, "path:"+term)
		}
	}
	if embeddingScore >= 0.08 {
		score += embeddingScore * 4.0
		boosts = append(boosts, fmt.Sprintf("local-embedding=%.2f", embeddingScore))
	}
	if len(matches) == 0 && len(boosts) == 0 {
		return 0, "no lexical or AST match"
	}
	reason := "bm25 terms: " + strings.Join(matches, ", ")
	if len(boosts) > 0 {
		reason += "; boosts: " + strings.Join(boosts, ", ")
	}
	return score, reason
}

func embeddingMode(enabled bool) string {
	if !enabled {
		return "off"
	}
	return "local-hash"
}

func embeddingVector(text string) map[int]float64 {
	features := embeddingFeatures(text)
	if len(features) == 0 {
		return nil
	}
	vec := make(map[int]float64, len(features))
	for feature, weight := range features {
		if feature == "" || weight == 0 {
			continue
		}
		idx := stableFeatureIndex(feature)
		vec[idx] += weight
	}
	norm := 0.0
	for _, value := range vec {
		norm += value * value
	}
	if norm == 0 {
		return vec
	}
	norm = math.Sqrt(norm)
	for idx, value := range vec {
		vec[idx] = value / norm
	}
	return vec
}

func embeddingFeatures(text string) map[string]float64 {
	features := map[string]float64{}
	for _, token := range rawEmbeddingTokens(text) {
		tokenLower := strings.ToLower(token)
		addEmbeddingFeature(features, "tok:"+tokenLower, 2.0)
		for _, part := range splitIdentifierToken(token) {
			if len(part) >= 2 {
				addEmbeddingFeature(features, "part:"+part, 1.8)
			}
			addCharNgrams(features, part, 3, 4, 0.35)
		}
		addCharNgrams(features, tokenLower, 3, 5, 0.25)
	}
	return features
}

func addEmbeddingFeature(features map[string]float64, key string, weight float64) {
	if key == "" {
		return
	}
	features[key] += weight
}

func addCharNgrams(features map[string]float64, token string, minN, maxN int, weight float64) {
	runes := []rune(token)
	if len(runes) < minN {
		return
	}
	for n := minN; n <= maxN; n++ {
		if len(runes) < n {
			continue
		}
		for i := 0; i+n <= len(runes); i++ {
			addEmbeddingFeature(features, fmt.Sprintf("ng%d:%s", n, string(runes[i:i+n])), weight)
		}
	}
}

func rawEmbeddingTokens(text string) []string {
	var tokens []string
	var current []rune
	flush := func() {
		if len(current) == 0 {
			return
		}
		token := string(current)
		current = current[:0]
		tokenLower := strings.ToLower(token)
		if len(tokenLower) < 2 {
			return
		}
		tokens = append(tokens, token)
	}
	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			current = append(current, r)
			continue
		}
		flush()
	}
	flush()
	return tokens
}

func splitIdentifierToken(token string) []string {
	var parts []string
	var current []rune
	flush := func() {
		if len(current) == 0 {
			return
		}
		part := strings.ToLower(string(current))
		current = current[:0]
		if len(part) >= 2 {
			parts = append(parts, part)
		}
	}
	for i, r := range token {
		if i > 0 && unicode.IsUpper(r) {
			flush()
		}
		current = append(current, r)
	}
	flush()
	tokenLower := strings.ToLower(token)
	if len(parts) == 0 && len(tokenLower) >= 2 {
		parts = append(parts, tokenLower)
	}
	return parts
}

func stableFeatureIndex(feature string) int {
	hash := uint32(2166136261)
	for _, b := range []byte(feature) {
		hash ^= uint32(b)
		hash *= 16777619
	}
	return int(hash % embeddingDimensions)
}

func cosineSparse(left, right map[int]float64) float64 {
	if len(left) == 0 || len(right) == 0 {
		return 0
	}
	if len(left) > len(right) {
		left, right = right, left
	}
	score := 0.0
	for idx, value := range left {
		score += value * right[idx]
	}
	return score
}

func selectRetrievalChunks(chunks []retrievalChunk, maxChunks, maxTokens int) []retrievalChunk {
	selected := make([]retrievalChunk, 0, maxChunks)
	tokens := 0
	seenPath := map[string]int{}
	for _, chunk := range chunks {
		if chunk.Score <= 0 {
			continue
		}
		if len(selected) >= maxChunks {
			break
		}
		if seenPath[chunk.Path] >= 3 {
			continue
		}
		if tokens+chunk.Tokens > maxTokens {
			continue
		}
		selected = append(selected, chunk)
		tokens += chunk.Tokens
		seenPath[chunk.Path]++
	}
	return selected
}

func inverseDocumentFrequency(chunks []retrievalChunk) map[string]float64 {
	df := map[string]int{}
	for _, chunk := range chunks {
		for term := range chunk.Terms {
			df[term]++
		}
	}
	total := float64(len(chunks))
	idf := map[string]float64{}
	for term, count := range df {
		idf[term] = math.Log(1 + (total-float64(count)+0.5)/(float64(count)+0.5))
	}
	return idf
}

func averageChunkTerms(chunks []retrievalChunk) float64 {
	if len(chunks) == 0 {
		return 0
	}
	total := 0
	for _, chunk := range chunks {
		total += sumTerms(chunk.Terms)
	}
	return float64(total) / float64(len(chunks))
}

func sumTerms(terms map[string]int) int {
	total := 0
	for _, count := range terms {
		total += count
	}
	return total
}

func repoMapHints(repoMap string) map[string]bool {
	hints := map[string]bool{}
	for _, line := range strings.Split(repoMap, "\n") {
		line = strings.TrimSpace(strings.TrimPrefix(line, "- "))
		if line == "" || strings.Contains(line, ": ") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		path := strings.TrimSpace(fields[0])
		if strings.Contains(path, "/") || strings.Contains(path, ".") {
			hints[path] = true
		}
	}
	return hints
}

func retrievalTerms(query string) []string {
	counts := termCounts(query)
	terms := make([]string, 0, len(counts))
	for term := range counts {
		if len(term) >= 3 {
			terms = append(terms, term)
		}
	}
	sort.Strings(terms)
	return terms
}

func termCounts(text string) map[string]int {
	terms := map[string]int{}
	var current []rune
	flush := func() {
		if len(current) == 0 {
			return
		}
		term := strings.ToLower(string(current))
		current = current[:0]
		if len(term) < 2 {
			return
		}
		terms[term]++
	}
	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' {
			current = append(current, r)
			continue
		}
		flush()
	}
	flush()
	return terms
}

func indentSnippet(text, prefix string) string {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	if len(lines) > 14 {
		lines = append(lines[:14], "...")
	}
	for i, line := range lines {
		if len(line) > 160 {
			line = line[:157] + "..."
		}
		lines[i] = prefix + line
	}
	return strings.Join(lines, "\n")
}

func retrievalFileScore(path string) int {
	depth := strings.Count(filepath.ToSlash(filepath.Clean(path)), "/")
	if depth >= 12 {
		return 0
	}
	return 12 - depth
}

func retrievalSkipDir(name string) bool {
	switch strings.ToLower(name) {
	case ".git", ".agentcli", "node_modules", "dist", "build", ".cache", ".next", "vendor", "coverage", "target", ".venv", "venv":
		return true
	default:
		return false
	}
}

func retrievalSkipPath(path string, info os.FileInfo) bool {
	lower := strings.ToLower(filepath.ToSlash(path))
	name := filepath.Base(lower)
	if name == ".env" || strings.HasPrefix(name, ".env.") ||
		strings.HasSuffix(name, ".pem") || strings.HasSuffix(name, ".key") ||
		strings.HasSuffix(name, ".p12") || strings.HasSuffix(name, ".pfx") {
		return true
	}
	switch name {
	case "package-lock.json", "yarn.lock", "pnpm-lock.yaml", "bun.lockb", "go.sum", "cargo.lock", "poetry.lock", "gemfile.lock":
		return true
	}
	if strings.HasSuffix(name, ".min.js") || strings.HasSuffix(name, ".min.css") || strings.HasSuffix(name, ".snap") {
		return true
	}
	if strings.Contains(lower, "__snapshots__/") || strings.Contains(name, ".generated.") || strings.Contains(name, ".gen.") {
		return true
	}
	switch filepath.Ext(name) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".ico", ".pdf", ".zip", ".gz", ".tar", ".mp4", ".mov", ".wasm":
		return true
	}
	if info != nil && info.Size() > 256*1024 {
		return true
	}
	return false
}

func looksBinary(data []byte) bool {
	sample := data
	if len(sample) > 4096 {
		sample = sample[:4096]
	}
	for _, b := range sample {
		if b == 0 {
			return true
		}
	}
	return false
}
