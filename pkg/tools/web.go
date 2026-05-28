package tools

import (
	"context"
	"errors"
	"fmt"
	"html"
	"io"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	contextmgr "klyra/pkg/context"
	"klyra/pkg/llm"
)

const webToolTimeout = 8 * time.Second
const webEmbeddingDimensions = 384

type WebSearch struct {
	Endpoint string
	Client   *http.Client
}

func (w WebSearch) Spec() llm.ToolSpec {
	return llm.ToolSpec{
		Name:        "web_search",
		Description: "Search the public web for current information. Use only when the user asks for internet/current/latest/external facts.",
		Parameters: objectSchema(map[string]any{
			"query":       stringProperty("Search query."),
			"max_results": integerProperty("Maximum result count.", 1),
		}, "query"),
	}
}

func (w WebSearch) Run(ctx context.Context, inv Invocation) (Result, error) {
	query, err := stringArg(inv.Args, "query")
	if err != nil {
		return Result{}, err
	}
	maxResults, err := optionalIntArg(inv.Args, "max_results", 5)
	if err != nil {
		return Result{}, err
	}
	if maxResults <= 0 || maxResults > 10 {
		maxResults = 5
	}
	endpoints := []string{w.Endpoint}
	if strings.TrimSpace(w.Endpoint) == "" {
		endpoints = []string{
			"https://lite.duckduckgo.com/lite/?q=%s",
			"https://html.duckduckgo.com/html/?q=%s",
		}
	}
	var errs []string
	for _, endpoint := range endpoints {
		if strings.TrimSpace(endpoint) == "" {
			continue
		}
		searchURL := searchEndpointURL(endpoint, query)
		data, err := httpGetText(ctx, w.Client, searchURL, 512_000)
		if err != nil {
			errs = append(errs, err.Error())
			if errors.Is(err, context.Canceled) {
				return Result{}, err
			}
			continue
		}
		results := parseSearchResults(data, maxResults)
		if len(results) == 0 {
			errs = append(errs, "no web results from "+searchURL)
			continue
		}
		return Result{Output: strings.Join(results, "\n")}, nil
	}
	if len(errs) == 0 {
		return Result{Output: "no web results"}, nil
	}
	return Result{Output: strings.Join(errs, "\n")}, fmt.Errorf("web search failed")
}

type FetchURL struct {
	Client *http.Client
}

func (FetchURL) Spec() llm.ToolSpec {
	return llm.ToolSpec{
		Name:        "fetch_url",
		Description: "Fetch a public http(s) URL. Pass query/focus from the user request to return only relevant chunks from long pages.",
		Parameters: objectSchema(map[string]any{
			"url":        stringProperty("HTTP or HTTPS URL."),
			"max_bytes":  integerProperty("Maximum response bytes to read.", 1),
			"query":      stringProperty("Optional focus query for retrieval over the fetched page."),
			"focus":      stringProperty("Optional focus query alias."),
			"max_tokens": integerProperty("Approximate token budget for focused page chunks.", 1),
		}, "url"),
	}
}

func (f FetchURL) Run(ctx context.Context, inv Invocation) (Result, error) {
	rawURL, err := stringArg(inv.Args, "url")
	if err != nil {
		return Result{}, err
	}
	maxBytes, err := optionalIntArg(inv.Args, "max_bytes", 12000)
	if err != nil {
		return Result{}, err
	}
	if maxBytes <= 0 || maxBytes > 100_000 {
		maxBytes = 12000
	}
	query, err := optionalStringArg(inv.Args, "query", "")
	if err != nil {
		return Result{}, err
	}
	if strings.TrimSpace(query) == "" {
		query, err = optionalStringArg(inv.Args, "focus", "")
		if err != nil {
			return Result{}, err
		}
	}
	maxTokens, err := optionalIntArg(inv.Args, "max_tokens", 1800)
	if err != nil {
		return Result{}, err
	}
	if maxTokens <= 0 || maxTokens > 8000 {
		maxTokens = 1800
	}
	data, err := httpGetText(ctx, f.Client, rawURL, int64(maxBytes))
	if err != nil {
		return Result{}, err
	}
	text := htmlToText(data)
	if strings.TrimSpace(query) != "" {
		if focused := focusedWebText(rawURL, text, query, maxTokens); strings.TrimSpace(focused) != "" {
			return Result{Output: focused}, nil
		}
	}
	return Result{Output: CompressOutput(text, 180)}, nil
}

func searchEndpointURL(endpoint, query string) string {
	escaped := url.QueryEscape(query)
	if strings.Contains(endpoint, "%s") {
		return fmt.Sprintf(endpoint, escaped)
	}
	separator := "?"
	if strings.Contains(endpoint, "?") {
		separator = "&"
	}
	return endpoint + separator + "q=" + escaped
}

func httpGetText(ctx context.Context, client *http.Client, rawURL string, maxBytes int64) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return "", err
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("only http(s) URLs are allowed")
	}
	var cancel context.CancelFunc
	if _, ok := ctx.Deadline(); !ok {
		ctx, cancel = context.WithTimeout(ctx, webToolTimeout)
		defer cancel()
	}
	if client == nil {
		client = &http.Client{Timeout: webToolTimeout}
	} else if client.Timeout == 0 {
		cloned := *client
		cloned.Timeout = webToolTimeout
		client = &cloned
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Klyra/1.0 (+https://github.com/tg-prplx/klyra)")
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return "", fmt.Errorf("GET %s returned %s", parsed.String(), resp.Status)
	}
	limit := maxBytes
	if limit <= 0 {
		limit = 12000
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return "", err
	}
	if int64(len(data)) > limit {
		data = data[:limit]
	}
	return string(data), nil
}

func parseSearchResults(page string, maxResults int) []string {
	linkPattern := regexp.MustCompile(`(?is)<a[^>]+href="([^"]+)"[^>]*>(.*?)</a>`)
	tagPattern := regexp.MustCompile(`(?is)<[^>]+>`)
	seen := map[string]bool{}
	var results []string
	for _, match := range linkPattern.FindAllStringSubmatch(page, -1) {
		href := html.UnescapeString(match[1])
		title := strings.TrimSpace(tagPattern.ReplaceAllString(match[2], " "))
		title = strings.Join(strings.Fields(html.UnescapeString(title)), " ")
		if title == "" || strings.Contains(strings.ToLower(href), "duckduckgo.com/y.js") {
			continue
		}
		if decoded := decodeDuckDuckGoURL(href); decoded != "" {
			href = decoded
		}
		if !strings.HasPrefix(href, "http://") && !strings.HasPrefix(href, "https://") {
			continue
		}
		if seen[href] {
			continue
		}
		seen[href] = true
		results = append(results, strconv.Itoa(len(results)+1)+". "+title+" - "+href)
		if len(results) >= maxResults {
			return results
		}
	}
	return results
}

func decodeDuckDuckGoURL(raw string) string {
	if strings.HasPrefix(raw, "//") {
		raw = "https:" + raw
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	if parsed.Query().Get("uddg") != "" {
		return parsed.Query().Get("uddg")
	}
	return ""
}

func htmlToText(raw string) string {
	text := regexp.MustCompile(`(?is)<script[^>]*>.*?</script>`).ReplaceAllString(raw, " ")
	text = regexp.MustCompile(`(?is)<style[^>]*>.*?</style>`).ReplaceAllString(text, " ")
	text = regexp.MustCompile(`(?is)<br\s*/?>|</p>|</div>|</h[1-6]>|</li>`).ReplaceAllString(text, "\n")
	text = regexp.MustCompile(`(?is)<[^>]+>`).ReplaceAllString(text, " ")
	text = html.UnescapeString(text)
	lines := splitNonEmptyLines(text)
	return strings.Join(lines, "\n")
}

type webChunk struct {
	StartLine int
	EndLine   int
	Text      string
	Terms     map[string]int
	Tokens    int
	Score     float64
	Reason    string
}

func focusedWebText(rawURL, text, query string, maxTokens int) string {
	terms := webRetrievalTerms(query)
	if len(terms) == 0 {
		return ""
	}
	chunks := webTextChunks(text)
	if len(chunks) == 0 {
		return ""
	}
	idf := webInverseDocumentFrequency(chunks)
	avgLen := webAverageChunkTerms(chunks)
	queryEmbedding := webEmbeddingVector(query)
	for i := range chunks {
		embeddingScore := webCosineSparse(queryEmbedding, webEmbeddingVector(chunks[i].Text))
		chunks[i].Score, chunks[i].Reason = webScoreChunk(chunks[i], terms, idf, avgLen, embeddingScore)
	}
	sort.SliceStable(chunks, func(i, j int) bool {
		if chunks[i].Score == chunks[j].Score {
			return chunks[i].StartLine < chunks[j].StartLine
		}
		return chunks[i].Score > chunks[j].Score
	})

	var selected []webChunk
	total := 0
	for _, chunk := range chunks {
		if chunk.Score <= 0 {
			continue
		}
		if len(selected) >= 6 {
			break
		}
		if total+chunk.Tokens > maxTokens {
			continue
		}
		selected = append(selected, chunk)
		total += chunk.Tokens
	}
	if len(selected) == 0 {
		return ""
	}

	var out []string
	out = append(out, "url: "+strings.TrimSpace(rawURL))
	out = append(out, "query: "+strings.TrimSpace(query))
	out = append(out, fmt.Sprintf("budget: %d tokens; selected: %d chunks; embeddings: local-hash", maxTokens, len(selected)))
	for i, chunk := range selected {
		out = append(out, fmt.Sprintf("%d. lines %d-%d score=%.2f tokens=%d", i+1, chunk.StartLine, chunk.EndLine, chunk.Score, chunk.Tokens))
		out = append(out, "   why: "+chunk.Reason)
		out = append(out, webIndentSnippet(chunk.Text, "   "))
	}
	return strings.Join(out, "\n")
}

func webTextChunks(text string) []webChunk {
	lines := splitWebRetrievalLines(text)
	var chunks []webChunk
	start := 0
	bytes := 0
	for i, line := range lines {
		if i > start && looksWebHeadingLine(line) && bytes >= 120 {
			chunks = appendWebChunk(chunks, lines[start:i], start+1)
			start = i
			bytes = 0
		}
		bytes += len(line) + 1
		paragraphBoundary := bytes >= 900 && strings.TrimSpace(line) != ""
		blankBoundary := strings.TrimSpace(line) == "" && bytes >= 180
		if bytes >= 1400 || paragraphBoundary || blankBoundary {
			chunks = appendWebChunk(chunks, lines[start:i+1], start+1)
			start = i + 1
			bytes = 0
		}
	}
	if start < len(lines) {
		chunks = appendWebChunk(chunks, lines[start:], start+1)
	}
	return chunks
}

func looksWebHeadingLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || len(trimmed) > 80 {
		return false
	}
	if strings.ContainsAny(trimmed, ".:;!?") {
		return false
	}
	words := strings.Fields(trimmed)
	return len(words) > 0 && len(words) <= 8
}

func splitWebRetrievalLines(text string) []string {
	var out []string
	for _, line := range strings.Split(strings.TrimSpace(text), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if len(line) <= 900 {
			out = append(out, line)
			continue
		}
		words := strings.Fields(line)
		var current []string
		currentLen := 0
		flush := func() {
			if len(current) == 0 {
				return
			}
			out = append(out, strings.Join(current, " "))
			current = nil
			currentLen = 0
		}
		for _, word := range words {
			if currentLen+len(word)+1 > 900 {
				flush()
			}
			current = append(current, word)
			currentLen += len(word) + 1
		}
		flush()
	}
	return out
}

func appendWebChunk(chunks []webChunk, lines []string, startLine int) []webChunk {
	text := strings.TrimSpace(strings.Join(lines, "\n"))
	if text == "" {
		return chunks
	}
	return append(chunks, webChunk{
		StartLine: startLine,
		EndLine:   startLine + strings.Count(text, "\n"),
		Text:      text,
		Terms:     webTermCounts(text),
		Tokens:    contextmgr.EstimateTokens(text),
	})
}

func webScoreChunk(chunk webChunk, queryTerms []string, idf map[string]float64, avgLen float64, embeddingScore float64) (float64, string) {
	const k1 = 1.2
	const b = 0.75
	docLen := float64(webSumTerms(chunk.Terms))
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
	if embeddingScore >= 0.08 {
		score += embeddingScore * 4.0
		boosts = append(boosts, fmt.Sprintf("local-embedding=%.2f", embeddingScore))
	}
	if len(matches) == 0 && len(boosts) == 0 {
		return 0, "no lexical or embedding match"
	}
	reason := "bm25 terms: " + strings.Join(matches, ", ")
	if len(boosts) > 0 {
		reason += "; boosts: " + strings.Join(boosts, ", ")
	}
	return score, reason
}

func webRetrievalTerms(query string) []string {
	counts := webTermCounts(query)
	terms := make([]string, 0, len(counts))
	for term := range counts {
		if len(term) >= 3 && !webStopTerms[term] {
			terms = append(terms, term)
		}
	}
	sort.Strings(terms)
	return terms
}

func webTermCounts(text string) map[string]int {
	terms := map[string]int{}
	var current []rune
	flush := func() {
		if len(current) == 0 {
			return
		}
		parts := webSplitIdentifierToken(string(current))
		current = current[:0]
		for _, part := range parts {
			if len(part) < 2 || webStopTerms[part] {
				continue
			}
			terms[part]++
		}
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

func webInverseDocumentFrequency(chunks []webChunk) map[string]float64 {
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

func webAverageChunkTerms(chunks []webChunk) float64 {
	if len(chunks) == 0 {
		return 0
	}
	total := 0
	for _, chunk := range chunks {
		total += webSumTerms(chunk.Terms)
	}
	return float64(total) / float64(len(chunks))
}

func webSumTerms(terms map[string]int) int {
	total := 0
	for _, count := range terms {
		total += count
	}
	return total
}

func webEmbeddingVector(text string) map[int]float64 {
	features := map[string]float64{}
	for _, token := range webRawEmbeddingTokens(text) {
		tokenLower := strings.ToLower(token)
		features["tok:"+tokenLower] += 2.0
		for _, part := range webSplitIdentifierToken(token) {
			features["part:"+part] += 1.8
			webAddCharNgrams(features, part, 3, 4, 0.35)
		}
		webAddCharNgrams(features, tokenLower, 3, 5, 0.25)
	}
	if len(features) == 0 {
		return nil
	}
	vec := map[int]float64{}
	for feature, weight := range features {
		idx := webStableFeatureIndex(feature)
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

func webRawEmbeddingTokens(text string) []string {
	var tokens []string
	var current []rune
	flush := func() {
		if len(current) == 0 {
			return
		}
		token := string(current)
		current = current[:0]
		lower := strings.ToLower(token)
		if len(lower) >= 2 && !webStopTerms[lower] {
			tokens = append(tokens, token)
		}
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

func webSplitIdentifierToken(token string) []string {
	var parts []string
	var current []rune
	flush := func() {
		if len(current) == 0 {
			return
		}
		part := strings.ToLower(string(current))
		current = current[:0]
		if len(part) >= 2 && !webStopTerms[part] {
			parts = append(parts, part)
		}
	}
	for i, r := range token {
		if i > 0 && (unicode.IsUpper(r) || r == '_') {
			flush()
			if r == '_' {
				continue
			}
		}
		current = append(current, r)
	}
	flush()
	lower := strings.ToLower(token)
	if len(parts) == 0 && len(lower) >= 2 && !webStopTerms[lower] {
		parts = append(parts, lower)
	}
	return parts
}

func webAddCharNgrams(features map[string]float64, token string, minN, maxN int, weight float64) {
	runes := []rune(token)
	if len(runes) < minN {
		return
	}
	for n := minN; n <= maxN; n++ {
		if len(runes) < n {
			continue
		}
		for i := 0; i+n <= len(runes); i++ {
			features[fmt.Sprintf("ng%d:%s", n, string(runes[i:i+n]))] += weight
		}
	}
}

func webStableFeatureIndex(feature string) int {
	hash := uint32(2166136261)
	for _, b := range []byte(feature) {
		hash ^= uint32(b)
		hash *= 16777619
	}
	return int(hash % webEmbeddingDimensions)
}

func webCosineSparse(left, right map[int]float64) float64 {
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

func webIndentSnippet(text, prefix string) string {
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

var webStopTerms = map[string]bool{
	"a": true, "an": true, "and": true, "are": true, "as": true, "at": true, "be": true, "by": true,
	"for": true, "from": true, "how": true, "in": true, "into": true, "is": true, "it": true,
	"of": true, "on": true, "or": true, "that": true, "the": true, "this": true, "to": true,
	"what": true, "when": true, "where": true, "which": true, "with": true, "и": true, "в": true,
	"во": true, "на": true, "по": true, "что": true, "как": true, "это": true,
}
