package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var customProviderEnvSanitizer = regexp.MustCompile(`[^A-Z0-9]+`)

type Config struct {
	Provider               string                    `json:"provider"`
	Model                  string                    `json:"model"`
	ModelRoutes            map[string]string         `json:"model_routes,omitempty"`
	BaseURLs               map[string]string         `json:"base_urls,omitempty"`
	CustomProviders        map[string]CustomProvider `json:"custom_providers,omitempty"`
	Reasoning              string                    `json:"reasoning,omitempty"`
	Stream                 bool                      `json:"stream"`
	MaxSteps               int                       `json:"max_steps"`
	MaxMessages            int                       `json:"max_messages"`
	MaxContext             int                       `json:"max_context_tokens"`
	MaxInstructions        int                       `json:"max_instruction_bytes"`
	MaxOutput              int                       `json:"max_output_tokens"`
	ApprovalMode           string                    `json:"approval_mode"`
	Sandbox                string                    `json:"sandbox"`
	Mode                   string                    `json:"mode"`
	ContextFiles           []string                  `json:"context_files,omitempty"`
	ContextCockpit         bool                      `json:"context_cockpit"`
	ContextCockpitInject   bool                      `json:"context_cockpit_inject"`
	ContextCockpitTokens   int                       `json:"context_cockpit_tokens"`
	ContextCockpitMaxFiles int                       `json:"context_cockpit_max_files"`
	ContextCockpitMaxCards int                       `json:"context_cockpit_max_cards"`
	ContextCockpitDiff     bool                      `json:"context_cockpit_diff"`
	ContextRetrieval       bool                      `json:"context_retrieval"`
	ContextRetrievalTokens int                       `json:"context_retrieval_tokens"`
	ContextRetrievalChunks int                       `json:"context_retrieval_chunks"`
	ContextEmbeddings      bool                      `json:"context_embeddings"`
	ContextReranker        bool                      `json:"context_reranker"`
	ContextRecipes         bool                      `json:"context_recipes"`
	NegativeContext        bool                      `json:"negative_context"`
	Skills                 bool                      `json:"skills"`
	MCPServers             map[string]MCPServer      `json:"mcp_servers,omitempty"`
	StoreResponses         bool                      `json:"store_responses"`
	DisabledTools          []string                  `json:"disabled_tools,omitempty"`
	Profiles               map[string]Profile        `json:"profiles,omitempty"`
}

type MCPServer struct {
	Command string            `json:"command"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	Enabled *bool             `json:"enabled,omitempty"`
}

type CustomProvider struct {
	BaseURL   string `json:"base_url,omitempty"`
	APIType   string `json:"api_type,omitempty"`
	APIKeyEnv string `json:"api_key_env,omitempty"`
}

type Profile struct {
	Provider               string                    `json:"provider,omitempty"`
	Model                  string                    `json:"model,omitempty"`
	ModelRoutes            map[string]string         `json:"model_routes,omitempty"`
	BaseURLs               map[string]string         `json:"base_urls,omitempty"`
	CustomProviders        map[string]CustomProvider `json:"custom_providers,omitempty"`
	Reasoning              string                    `json:"reasoning,omitempty"`
	Stream                 *bool                     `json:"stream,omitempty"`
	MaxSteps               int                       `json:"max_steps,omitempty"`
	MaxMessages            int                       `json:"max_messages,omitempty"`
	MaxContext             int                       `json:"max_context_tokens,omitempty"`
	MaxInstructions        int                       `json:"max_instruction_bytes,omitempty"`
	MaxOutput              int                       `json:"max_output_tokens,omitempty"`
	ApprovalMode           string                    `json:"approval_mode,omitempty"`
	Sandbox                string                    `json:"sandbox,omitempty"`
	Mode                   string                    `json:"mode,omitempty"`
	ContextFiles           []string                  `json:"context_files,omitempty"`
	ContextCockpit         *bool                     `json:"context_cockpit,omitempty"`
	ContextCockpitInject   *bool                     `json:"context_cockpit_inject,omitempty"`
	ContextCockpitTokens   int                       `json:"context_cockpit_tokens,omitempty"`
	ContextCockpitMaxFiles int                       `json:"context_cockpit_max_files,omitempty"`
	ContextCockpitMaxCards int                       `json:"context_cockpit_max_cards,omitempty"`
	ContextCockpitDiff     *bool                     `json:"context_cockpit_diff,omitempty"`
	ContextRetrieval       *bool                     `json:"context_retrieval,omitempty"`
	ContextRetrievalTokens int                       `json:"context_retrieval_tokens,omitempty"`
	ContextRetrievalChunks int                       `json:"context_retrieval_chunks,omitempty"`
	ContextEmbeddings      *bool                     `json:"context_embeddings,omitempty"`
	ContextReranker        *bool                     `json:"context_reranker,omitempty"`
	ContextRecipes         *bool                     `json:"context_recipes,omitempty"`
	NegativeContext        *bool                     `json:"negative_context,omitempty"`
	Skills                 *bool                     `json:"skills,omitempty"`
	MCPServers             map[string]MCPServer      `json:"mcp_servers,omitempty"`
	StoreResponses         *bool                     `json:"store_responses,omitempty"`
	DisabledTools          []string                  `json:"disabled_tools,omitempty"`
}

func Default() Config {
	return Config{
		Provider:               "mock",
		Model:                  "mock-agent",
		BaseURLs:               map[string]string{},
		CustomProviders:        map[string]CustomProvider{},
		MCPServers:             map[string]MCPServer{},
		Stream:                 true,
		MaxSteps:               20,
		MaxMessages:            40,
		MaxContext:             24000,
		MaxInstructions:        12000,
		MaxOutput:              4096,
		ApprovalMode:           "auto",
		Sandbox:                "workspace-write",
		Mode:                   "edit",
		ContextCockpit:         true,
		ContextCockpitInject:   true,
		ContextCockpitTokens:   1200,
		ContextCockpitMaxFiles: 60,
		ContextCockpitMaxCards: 10,
		ContextCockpitDiff:     true,
		ContextRetrieval:       true,
		ContextRetrievalTokens: 1000,
		ContextRetrievalChunks: 10,
		ContextEmbeddings:      true,
		ContextReranker:        false,
		ContextRecipes:         true,
		NegativeContext:        true,
		Skills:                 true,
		DisabledTools:          []string{"write_file"},
		Profiles: map[string]Profile{
			"local": {
				Provider: "mock",
				Model:    "mock-agent",
			},
			"ollama": {
				Provider:     "ollama",
				MaxSteps:     20,
				MaxContext:   16000,
				MaxOutput:    4096,
				ApprovalMode: "ask",
				Sandbox:      "workspace-write",
			},
			"anthropic": {
				Provider:     "anthropic",
				MaxSteps:     20,
				MaxContext:   32000,
				MaxOutput:    4096,
				ApprovalMode: "ask",
				Sandbox:      "workspace-write",
			},
			"gemini": {
				Provider:     "gemini",
				MaxSteps:     20,
				MaxContext:   32000,
				MaxOutput:    4096,
				ApprovalMode: "ask",
				Sandbox:      "workspace-write",
			},
			"coding": {
				Provider:     "openai",
				Reasoning:    "low",
				MaxSteps:     20,
				MaxOutput:    4096,
				ApprovalMode: "ask",
				Sandbox:      "workspace-write",
			},
			"deep": {
				Provider:     "openai",
				Reasoning:    "medium",
				MaxSteps:     30,
				MaxContext:   64000,
				MaxOutput:    8192,
				ApprovalMode: "ask",
			},
		},
	}
}

func CanonicalProviderName(name string) string {
	normalized := strings.ToLower(strings.TrimSpace(name))
	switch normalized {
	case "", "openai", "responses":
		return "openai"
	case "local", "chat", "chat-completions", "openai-chat", "openai-compatible":
		return "local"
	case "ollama":
		return "ollama"
	case "anthropic", "claude":
		return "anthropic"
	case "gemini", "google":
		return "gemini"
	default:
		return normalized
	}
}

func IsBuiltInProvider(name string) bool {
	switch CanonicalProviderName(name) {
	case "openai", "local", "ollama", "anthropic", "gemini", "mock":
		return true
	default:
		return false
	}
}

func NormalizeProviderAPIType(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "chat", "chat_completions", "chat-completions", "completions":
		return "chat_completions"
	case "responses", "response":
		return "responses"
	default:
		return "chat_completions"
	}
}

func CustomProviderAPIKeyEnv(name string) string {
	normalized := strings.ToUpper(strings.TrimSpace(CanonicalProviderName(name)))
	if normalized == "" {
		normalized = "CUSTOM"
	}
	normalized = customProviderEnvSanitizer.ReplaceAllString(normalized, "_")
	normalized = strings.Trim(normalized, "_")
	if normalized == "" {
		normalized = "CUSTOM"
	}
	return "KLYRA_PROVIDER_" + normalized + "_API_KEY"
}

func DefaultPath() string {
	if dir, err := os.UserConfigDir(); err == nil && dir != "" {
		return filepath.Join(dir, "agentcli", "config.json")
	}
	return filepath.Join(".", ".agentcli", "config.json")
}

func Load(path string) (Config, error) {
	cfg := Default()
	if strings.TrimSpace(path) == "" {
		path = DefaultPath()
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return Config{}, err
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("read %s: %w", path, err)
	}
	raw := string(data)
	defaults := Default()
	if !strings.Contains(raw, `"context_cockpit"`) {
		cfg.ContextCockpit = defaults.ContextCockpit
	}
	if !strings.Contains(raw, `"context_cockpit_inject"`) {
		cfg.ContextCockpitInject = defaults.ContextCockpitInject
	}
	if !strings.Contains(raw, `"context_cockpit_diff"`) {
		cfg.ContextCockpitDiff = defaults.ContextCockpitDiff
	}
	if !strings.Contains(raw, `"context_retrieval"`) {
		cfg.ContextRetrieval = defaults.ContextRetrieval
	}
	if !strings.Contains(raw, `"context_embeddings"`) {
		cfg.ContextEmbeddings = defaults.ContextEmbeddings
	}
	if !strings.Contains(raw, `"context_reranker"`) {
		cfg.ContextReranker = defaults.ContextReranker
	}
	if !strings.Contains(raw, `"context_recipes"`) {
		cfg.ContextRecipes = defaults.ContextRecipes
	}
	if !strings.Contains(raw, `"negative_context"`) {
		cfg.NegativeContext = defaults.NegativeContext
	}
	if !strings.Contains(raw, `"skills"`) {
		cfg.Skills = defaults.Skills
	}
	if !strings.Contains(raw, `"stream"`) {
		cfg.Stream = defaults.Stream
	}
	cfg.applyDefaults()
	return cfg, nil
}

func WriteDefault(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		path = DefaultPath()
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	data, err := json.MarshalIndent(Default(), "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		return "", err
	}
	return path, nil
}

func (c Config) Save(path string) error {
	if strings.TrimSpace(path) == "" {
		path = DefaultPath()
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

func (c Config) WithProfile(name string) (Config, error) {
	c.applyDefaults()
	name = strings.TrimSpace(name)
	if name == "" {
		return c, nil
	}
	profile, ok := c.Profiles[name]
	if !ok {
		return Config{}, fmt.Errorf("profile %q not found", name)
	}
	providerChanged := profile.Provider != "" && profile.Provider != c.Provider
	if profile.Provider != "" {
		c.Provider = profile.Provider
	}
	if profile.Model != "" {
		c.Model = profile.Model
	} else if providerChanged {
		c.Model = ""
	}
	if profile.ModelRoutes != nil {
		c.ModelRoutes = profile.ModelRoutes
	}
	if profile.BaseURLs != nil {
		if c.BaseURLs == nil {
			c.BaseURLs = map[string]string{}
		}
		for provider, baseURL := range profile.BaseURLs {
			c.BaseURLs[provider] = baseURL
		}
	}
	if profile.CustomProviders != nil {
		if c.CustomProviders == nil {
			c.CustomProviders = map[string]CustomProvider{}
		}
		for name, provider := range profile.CustomProviders {
			c.CustomProviders[CanonicalProviderName(name)] = provider
		}
	}
	if profile.Reasoning != "" {
		c.Reasoning = profile.Reasoning
	}
	if profile.Stream != nil {
		c.Stream = *profile.Stream
	}
	if profile.MaxSteps > 0 {
		c.MaxSteps = profile.MaxSteps
	}
	if profile.MaxMessages > 0 {
		c.MaxMessages = profile.MaxMessages
	}
	if profile.MaxContext > 0 {
		c.MaxContext = profile.MaxContext
	}
	if profile.MaxInstructions > 0 {
		c.MaxInstructions = profile.MaxInstructions
	}
	if profile.MaxOutput > 0 {
		c.MaxOutput = profile.MaxOutput
	}
	if profile.ApprovalMode != "" {
		c.ApprovalMode = profile.ApprovalMode
	}
	if profile.Sandbox != "" {
		c.Sandbox = profile.Sandbox
	}
	if profile.Mode != "" {
		c.Mode = profile.Mode
	}
	if profile.ContextFiles != nil {
		c.ContextFiles = append([]string(nil), profile.ContextFiles...)
	}
	if profile.ContextCockpit != nil {
		c.ContextCockpit = *profile.ContextCockpit
	}
	if profile.ContextCockpitInject != nil {
		c.ContextCockpitInject = *profile.ContextCockpitInject
	}
	if profile.ContextCockpitTokens > 0 {
		c.ContextCockpitTokens = profile.ContextCockpitTokens
	}
	if profile.ContextCockpitMaxFiles > 0 {
		c.ContextCockpitMaxFiles = profile.ContextCockpitMaxFiles
	}
	if profile.ContextCockpitMaxCards > 0 {
		c.ContextCockpitMaxCards = profile.ContextCockpitMaxCards
	}
	if profile.ContextCockpitDiff != nil {
		c.ContextCockpitDiff = *profile.ContextCockpitDiff
	}
	if profile.ContextRetrieval != nil {
		c.ContextRetrieval = *profile.ContextRetrieval
	}
	if profile.ContextRetrievalTokens > 0 {
		c.ContextRetrievalTokens = profile.ContextRetrievalTokens
	}
	if profile.ContextRetrievalChunks > 0 {
		c.ContextRetrievalChunks = profile.ContextRetrievalChunks
	}
	if profile.ContextEmbeddings != nil {
		c.ContextEmbeddings = *profile.ContextEmbeddings
	}
	if profile.ContextReranker != nil {
		c.ContextReranker = *profile.ContextReranker
	}
	if profile.ContextRecipes != nil {
		c.ContextRecipes = *profile.ContextRecipes
	}
	if profile.NegativeContext != nil {
		c.NegativeContext = *profile.NegativeContext
	}
	if profile.Skills != nil {
		c.Skills = *profile.Skills
	}
	if profile.MCPServers != nil {
		if c.MCPServers == nil {
			c.MCPServers = map[string]MCPServer{}
		}
		for name, server := range profile.MCPServers {
			c.MCPServers[name] = server
		}
	}
	if profile.StoreResponses != nil {
		c.StoreResponses = *profile.StoreResponses
	}
	if profile.DisabledTools != nil {
		c.DisabledTools = append([]string(nil), profile.DisabledTools...)
	}
	return c, nil
}

func (c *Config) applyDefaults() {
	defaults := Default()
	if c.Provider == "" {
		c.Provider = defaults.Provider
	}
	if c.BaseURLs == nil {
		c.BaseURLs = map[string]string{}
	}
	if c.CustomProviders == nil {
		c.CustomProviders = map[string]CustomProvider{}
	}
	if len(c.CustomProviders) > 0 {
		normalized := make(map[string]CustomProvider, len(c.CustomProviders))
		for name, provider := range c.CustomProviders {
			key := CanonicalProviderName(name)
			provider.APIType = NormalizeProviderAPIType(provider.APIType)
			if strings.TrimSpace(provider.APIKeyEnv) == "" {
				provider.APIKeyEnv = CustomProviderAPIKeyEnv(key)
			}
			normalized[key] = provider
		}
		c.CustomProviders = normalized
	}
	if c.MCPServers == nil {
		c.MCPServers = map[string]MCPServer{}
	}
	if c.Model == "" && c.Provider == "mock" {
		c.Model = defaults.Model
	}
	if c.MaxSteps <= 0 {
		c.MaxSteps = defaults.MaxSteps
	}
	if c.MaxMessages <= 0 {
		c.MaxMessages = defaults.MaxMessages
	}
	if c.MaxContext <= 0 {
		c.MaxContext = defaults.MaxContext
	}
	if c.MaxInstructions <= 0 {
		c.MaxInstructions = defaults.MaxInstructions
	}
	if c.MaxOutput <= 0 {
		c.MaxOutput = defaults.MaxOutput
	}
	if c.ApprovalMode == "" {
		c.ApprovalMode = defaults.ApprovalMode
	}
	if c.Sandbox == "" {
		c.Sandbox = defaults.Sandbox
	}
	if c.Mode == "" {
		c.Mode = defaults.Mode
	}
	if c.Profiles == nil {
		c.Profiles = defaults.Profiles
	}
	if c.ContextCockpitTokens <= 0 {
		c.ContextCockpitTokens = defaults.ContextCockpitTokens
	}
	if c.ContextCockpitMaxFiles <= 0 {
		c.ContextCockpitMaxFiles = defaults.ContextCockpitMaxFiles
	}
	if c.ContextCockpitMaxCards <= 0 {
		c.ContextCockpitMaxCards = defaults.ContextCockpitMaxCards
	}
	if c.ContextRetrievalTokens <= 0 {
		c.ContextRetrievalTokens = defaults.ContextRetrievalTokens
	}
	if c.ContextRetrievalChunks <= 0 {
		c.ContextRetrievalChunks = defaults.ContextRetrievalChunks
	}
}
