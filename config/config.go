package config

import (
	"encoding/json"
	"os"
	"strings"
	"sync"

	"github.com/google/uuid"
)

type LLMConfig struct {
	Provider      string               `json:"provider"`
	Model         string               `json:"model"`
	BaseURL       string               `json:"base_url,omitempty"`     // Custom base URL (e.g. for Ollama)
	MaxTokens     int                  `json:"max_tokens"`
	MaxTurns      int                  `json:"max_turns,omitempty"`    // Max agentic loop iterations (default 50, was 10)
	Temperature   *float64             `json:"temperature,omitempty"`
	SystemPrompt  string               `json:"system_prompt,omitempty"`
	Tools         []string             `json:"tools,omitempty"`
	BuiltinTools  []BuiltinToolConfig  `json:"builtin_tools,omitempty"`
	Vision        *VisionConfig        `json:"vision,omitempty"`
	ParallelTools *ParallelToolsConfig `json:"parallel_tools,omitempty"`
	ToolLoading   *ToolLoadingConfig   `json:"tool_loading,omitempty"`
}

// ToolLoadingConfig configures on-demand tool loading to reduce token usage
type ToolLoadingConfig struct {
	Mode       string         `json:"mode"`        // "full" (default), "on_demand"
	Strategy   string         `json:"strategy"`    // "regex", "bm25", "keyword" (only used when mode="on_demand")
	AlwaysLoad []string       `json:"always_load"` // Tools always included regardless of strategy
	MaxTools   int            `json:"max_tools"`   // Cap on loaded tools (default: 15)
	Regex      *RegexConfig   `json:"regex,omitempty"`
	BM25       *BM25Config    `json:"bm25,omitempty"`
	Keyword    *KeywordConfig `json:"keyword,omitempty"`
}

// RegexConfig for regex-based tool loading
type RegexConfig struct {
	Rules []RegexRule `json:"rules"`
}

// RegexRule maps a regex pattern to tools to load
type RegexRule struct {
	Pattern string   `json:"pattern"` // Regex pattern to match against user message
	Tools   []string `json:"tools"`   // Tools to load (supports glob: "stripe:*")
}

// BM25Config for BM25-based automatic tool loading
type BM25Config struct {
	TopK      int     `json:"top_k"`     // Number of top tools to load (default: 10)
	Threshold float64 `json:"threshold"` // Min score to include (default: 0.2)
}

// KeywordConfig for keyword-based tool loading
type KeywordConfig struct {
	Rules []KeywordRule `json:"rules"`
}

// KeywordRule maps keywords to tools to load
type KeywordRule struct {
	Keywords []string `json:"keywords"` // Words to match (case-insensitive)
	Tools    []string `json:"tools"`    // Tools to load
}

type ParallelToolsConfig struct {
	Enabled        bool `json:"enabled"`         // Enable parallel tool execution (default: true)
	MaxConcurrent  int  `json:"max_concurrent"`  // Max concurrent tool executions (default: 10)
}

type VisionConfig struct {
	Enabled      bool       `json:"enabled"`
	MaxImages    int        `json:"max_images"`       // Per request
	MaxImageSize int64      `json:"max_image_size"`   // In bytes
	AllowedTypes []string   `json:"allowed_types"`    // ["jpeg", "png", "gif", "webp"]
	ResizeImages bool       `json:"resize_images"`
	MaxDimension int        `json:"max_dimension"`    // Max width/height in pixels
	PDFConfig    *PDFConfig `json:"pdf,omitempty"`    // PDF-specific settings
}

type PDFConfig struct {
	Enabled     bool  `json:"enabled"`
	MaxFileSize int64 `json:"max_file_size"` // Max PDF size in bytes (default: 32MB)
	MaxPages    int   `json:"max_pages"`     // Max pages to process (default: 100)
	AllowURLs   bool  `json:"allow_urls"`    // Allow fetching PDFs from URLs
}

type BuiltinToolConfig struct {
	Type              string              `json:"type"`                        // "web_search_20250305", "web_fetch_20250910", "computer_20250124"
	Name              string              `json:"name"`                        // "web_search", "web_fetch", "computer"
	MaxUses           *int                `json:"max_uses,omitempty"`
	AllowedDomains    []string            `json:"allowed_domains,omitempty"`
	BlockedDomains    []string            `json:"blocked_domains,omitempty"`
	UserLocation      *UserLocationConfig `json:"user_location,omitempty"`
	Citations         *CitationsConfig    `json:"citations,omitempty"`         // For web_fetch
	MaxContentTokens  *int                `json:"max_content_tokens,omitempty"` // For web_fetch
	DisplayWidthPx    *int                `json:"display_width_px,omitempty"`   // For computer tool
	DisplayHeightPx   *int                `json:"display_height_px,omitempty"`  // For computer tool
	DisplayNumber     *int                `json:"display_number,omitempty"`     // For computer tool
}

// ComputerToolVersion returns the correct computer tool type string for the given model.
// Claude Opus 4.6, Sonnet 4.6, and Opus 4.5 use computer_20251124;
// all other models use computer_20250124.
func ComputerToolVersion(model string) string {
	if strings.Contains(model, "opus-4-6") ||
		strings.Contains(model, "sonnet-4-6") ||
		strings.Contains(model, "opus-4-5") {
		return "computer_20251124"
	}
	return "computer_20250124"
}

// ComputerUseBetaFlag returns the correct anthropic-beta header value for computer use.
func ComputerUseBetaFlag(model string) string {
	if strings.Contains(model, "opus-4-6") ||
		strings.Contains(model, "sonnet-4-6") ||
		strings.Contains(model, "opus-4-5") {
		return "computer-use-2025-11-24"
	}
	return "computer-use-2025-01-24"
}

// IsComputerTool returns true if the tool type is any version of the computer tool (Anthropic or OpenAI).
func IsComputerTool(toolType string) bool {
	return toolType == "computer_20250124" || toolType == "computer_20251124" || toolType == "computer"
}

// IsOpenAIComputerTool returns true if the tool type is OpenAI's native computer tool.
func IsOpenAIComputerTool(toolType string) bool {
	return toolType == "computer"
}

type CitationsConfig struct {
	Enabled bool `json:"enabled"`
}

type UserLocationConfig struct {
	Type     string `json:"type"`     // "approximate"
	City     string `json:"city"`
	Region   string `json:"region"`
	Country  string `json:"country"`
	Timezone string `json:"timezone"`
}

type MCPCredential struct {
	CredentialID string `json:"credential_id"` // Backend credential ID (from /credentials endpoint)
	Provider     string `json:"provider"`      // Provider name: "stripe", "github", etc.
	Name         string `json:"name"`          // Display name (optional, for reference)
}

type MCPConfig struct {
	Enabled     bool                     `json:"enabled"`
	BaseURL     string                   `json:"base_url,omitempty"`         // Backend API URL for subscriptions
	APIKey      string                   `json:"api_key,omitempty"`          // Backend API key for subscriptions
	TestMode    *bool                    `json:"test_mode,omitempty"`        // When true, MCP calls use mock responses. nil = use global test_mode
	Tools       []string                 `json:"tools,omitempty"`            // MCP tools to enable (e.g., ["geocode", "list-customers"])
	Credentials map[string]MCPCredential `json:"credentials,omitempty"`      // MCP tool credentials
	Webhooks    []string                 `json:"webhooks,omitempty"`         // Webhook servers (e.g., ["stripe", "github"])
	Servers     []ExternalMCPServer      `json:"servers,omitempty"`          // External standard MCP servers
}

// ExternalMCPServer configures a connection to an external standard MCP server
type ExternalMCPServer struct {
	Name    string            `json:"name"`              // Unique name for this server (e.g., "time", "fetch")
	Type    string            `json:"type,omitempty"`    // "http" (default) or "stdio"
	URL     string            `json:"url,omitempty"`     // Server endpoint URL for HTTP type
	Headers map[string]string `json:"headers,omitempty"` // Optional headers for HTTP auth
	Command []string          `json:"command,omitempty"` // Command to run for stdio type (e.g., ["npx", "-y", "@dangahagan/weather-mcp"])
	Args    []string          `json:"args,omitempty"`    // Additional arguments for stdio command
	Env     map[string]string `json:"env,omitempty"`     // Environment variables for stdio command
	Enabled bool              `json:"enabled"`           // Whether to connect to this server
}

// Skill represents a skill definition with instructions
type Skill struct {
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	Instructions string   `json:"instructions"`
	Icon         string   `json:"icon,omitempty"`
	Category     string   `json:"category,omitempty"`
	Tags         []string `json:"tags,omitempty"`
	Tools        []string `json:"tools,omitempty"` // MCP/builtin tools this skill needs
	Enabled      bool     `json:"enabled"`
}

// SkillsConfig configures skill-based instructions
type SkillsConfig struct {
	Enabled     bool    `json:"enabled"`
	Definitions []Skill `json:"definitions,omitempty"` // Inline skill definitions
}

type SchedulerConfig struct {
	Enabled  bool   `json:"enabled"`
	Interval string `json:"interval"`  // "30s", "1m", "5m"
	MaxTasks int    `json:"max_tasks"` // Future: task limit
}

type ReflectionConfig struct {
	Enabled         bool   `json:"enabled"`
	Interval        string `json:"interval"`          // "6h", "12h", "24h"
	Prompt          string `json:"prompt"`             // Reflection prompt (agent can self-modify via config_set)
	AfterTask       bool   `json:"after_task"`         // Trigger after each task completes
	ConversationMin int    `json:"conversation_min"`   // Trigger after N conversations (0=disabled)
	LookbackHours   int    `json:"lookback_hours"`     // How far back to analyze (default: 24)
}

type TasksConfig struct {
	Enabled         bool `json:"enabled"`
	AllowScheduling bool `json:"allow_scheduling"` // Can create future tasks
	AllowRecurring  bool `json:"allow_recurring"`  // Can create recurring tasks
	MaxTasks        int  `json:"max_tasks"`        // Maximum total tasks
	AutoExecute     bool `json:"auto_execute"`     // Auto-run immediate tasks
}

type MemoryConfig struct {
	Enabled             bool    `json:"enabled"`
	EmbeddingModel      string  `json:"embedding_model"`        // "text-embedding-3-small" or model for selected provider
	DecisionModel       string  `json:"decision_model"`         // Model for memory decisions (auto-selected if empty)
	MaxMemoriesPerQuery int     `json:"max_memories_per_query"` // 20
	MinImportance       float64 `json:"min_importance"`         // 0.3
	MinSimilarity       float64 `json:"min_similarity"`         // 0.3 - minimum cosine similarity for RAG retrieval
	AutoPrune           bool    `json:"auto_prune"`
	MaxMemories         int     `json:"max_memories"`           // 10000

	// Embedding provider configuration
	// REQUIRED when memory is enabled - must be explicitly set
	// Options: "openai", "gemini", "jina", "voyage", "cohere", "huggingface", "ollama"
	EmbeddingProvider   string `json:"embedding_provider"`   // Required: "openai", "gemini", "jina", "voyage", "cohere", "huggingface", "ollama"
	EmbeddingDimensions int    `json:"embedding_dimensions"` // Output dimensions (0 = provider default). Gemini supports 768, 1536, 3072.
	OllamaURL           string `json:"ollama_url"`           // Ollama API URL (default: http://localhost:11434)

	// Decision model provider configuration
	// When empty or "auto", uses the same provider as main LLM with a small model
	DecisionProvider string `json:"decision_provider"` // "auto" (default), "anthropic", "openai", "gemini"

	// Differential memory mode - pure RAG-based novelty detection without LLM calls
	// "differential" (default): Uses embedding similarity to detect novel information
	// "llm": Uses LLM to decide what to remember (requires decision_provider)
	DecisionMode      string  `json:"decision_mode"`       // "differential" (default) or "llm"
	NoveltyThreshold  float64 `json:"novelty_threshold"`   // Max similarity to existing memories to be considered novel (default: 0.80)
	MinSentenceWords  int     `json:"min_sentence_words"`  // Minimum words in a sentence to consider (default: 4)
	SkipQuestions     bool    `json:"skip_questions"`      // Skip storing questions (default: true)

	// Memory extraction from conversations
	AutoExtractMemories *bool `json:"auto_extract_memories"` // Auto-extract memories from conversations (default: true when nil)

	// File ingestion for RAG
	AutoIngestFiles    bool     `json:"auto_ingest_files"`    // Auto-chunk uploaded files into memories
	IngestTypes        []string `json:"ingest_types"`         // MIME types to ingest ["text/plain", "text/markdown"]
	ChunkSize          int      `json:"chunk_size"`           // Max characters per chunk (default: 1000)
	ChunkOverlap       int      `json:"chunk_overlap"`        // Overlap between chunks (default: 200)
	DocumentImportance float64  `json:"document_importance"`  // Default importance for doc chunks (default: 0.5)
}

type OperatorConfig struct {
	Enabled         bool     `json:"enabled"`
	BrowserProvider string   `json:"browser_provider"`      // "browserengine" (default), "browserbase", "steel", "cdp"
	TestMode        *bool    `json:"test_mode,omitempty"`   // Use test mode (nil = use global test_mode)
	DisplayWidth    int      `json:"display_width"`         // 1024
	DisplayHeight   int      `json:"display_height"`        // 768
	Proxy        *bool  `json:"proxy,omitempty"`         // Enable proxy (nil = true for backward compat)
	ProxyCountry string `json:"proxy_country,omitempty"` // Default proxy country ISO 3166-1 alpha-2 code (e.g., "US", "GB", "DE")
	AllowedDomains []string `json:"allowed_domains,omitempty"`
	BlockedDomains []string `json:"blocked_domains,omitempty"`

	// Provider-specific configs (only the active provider's config is used)
	BrowserEngine *BrowserEngineProviderConfig `json:"browserengine,omitempty"`
	Browserbase   *BrowserbaseProviderConfig   `json:"browserbase,omitempty"`
	Steel         *SteelProviderConfig         `json:"steel,omitempty"`
	CDP           *CDPProviderConfig           `json:"cdp,omitempty"`
}

// CDPProviderConfig holds direct CDP connection settings
type CDPProviderConfig struct {
	URL string `json:"url"` // e.g., "ws://localhost:9222" or "http://5.75.128.19:9222"
}

// BrowserEngineProviderConfig holds BrowserEngine cloud service settings
type BrowserEngineProviderConfig struct {
	BaseURL string `json:"base_url"` // Default: "https://api.browserengine.co"
	APIKey  string `json:"api_key"`  // X-API-Key header value
}

// BrowserbaseProviderConfig holds Browserbase connection settings
type BrowserbaseProviderConfig struct {
	APIKey    string `json:"api_key"`
	ProjectID string `json:"project_id"`
}

// SteelProviderConfig holds Steel.dev connection settings
type SteelProviderConfig struct {
	APIKey  string `json:"api_key"`
	BaseURL string `json:"base_url"` // Default: "https://api.steel.dev"
}

type ContextConfig struct {
	MaxMessages  int               `json:"max_messages"`            // Keep last N messages (0 = unlimited) - fallback if max_tokens not set
	MaxTokens    int               `json:"max_tokens"`              // Max estimated tokens to keep (0 = use max_messages instead)
	KeepImages   int               `json:"keep_images"`             // Keep images only in last N messages (0 = keep all)
	Compaction   *CompactionConfig `json:"compaction,omitempty"`
	SummaryModel string            `json:"summary_model,omitempty"` // Model for thread activity summaries (default: "claude-haiku-4-5")
}

type CompactionConfig struct {
	Enabled          bool    `json:"enabled"`            // Enable auto-compaction (default: true)
	TriggerRatio     float64 `json:"trigger_ratio"`      // Compact when context reaches X% of max_tokens (default: 0.7)
	KeepRecent       int     `json:"keep_recent"`        // Never compact last N messages (default: 20)
	SummaryMaxTokens int     `json:"summary_max_tokens"` // Max tokens for summary block (default: 1500)
	Model            string  `json:"model"`              // Model for summarization (default: "claude-haiku-4")
}

type FileSystemConfig struct {
	Enabled        bool     `json:"enabled"`          // Master toggle (default: false)
	MaxFileSize    int64    `json:"max_file_size"`    // Max single file size in bytes (default: 10MB)
	MaxTotalSize   int64    `json:"max_total_size"`   // Max total storage in bytes (default: 100MB)
	AutoExtract    bool     `json:"auto_extract"`     // Auto-extract base64 from messages (default: true)
	MinExtractSize int      `json:"min_extract_size"` // Only extract if base64 > N bytes (default: 10KB)
	Deduplication  bool     `json:"deduplication"`    // Use checksums to dedupe (default: true)
	AutoCleanup    bool     `json:"auto_cleanup"`     // Enable automatic cleanup (default: true)
	RetentionDays  int      `json:"retention_days"`   // Keep files for N days, 0 = forever (default: 7)
	CleanupOrphans bool     `json:"cleanup_orphans"`  // Remove files with no message reference (default: true)
	AllowedTypes   []string `json:"allowed_types"`    // File types to store (default: ["image", "document"])
}

// A2AConfig enables the Google Agent2Agent (A2A) protocol for inter-agent communication.
type A2AConfig struct {
	Enabled           bool `json:"enabled"`
	PushNotifications bool `json:"push_notifications,omitempty"`
}

type AgentsConfig struct {
	Enabled          bool              `json:"enabled"`
	Mode             string            `json:"mode,omitempty"`               // "peer" | "coordinator" | "worker" (default: "peer")
	Group            string            `json:"group,omitempty"`              // Discovery group name (e.g., "production", "staging")
	DiscoveryMethod  string            `json:"discovery_method,omitempty"`   // "file" | "mdns" | "ssdp" | "manual" (default: "file")
	FileRegistryPath string            `json:"file_registry_path,omitempty"` // Path for file-based discovery (default: "/tmp/apteva-agents")
	GossipSeed       string            `json:"gossip_seed,omitempty"`        // Gossip seed address (triggers gossip mode)
	GossipPort       int               `json:"gossip_port,omitempty"`        // Gossip port (default: 7946)
	AvailableAgents  []AgentInfo       `json:"available_agents,omitempty"`   // Manual mode (legacy/fallback)
	Settings         AgentCommSettings `json:"settings"`
	InjectIntoPrompt *bool             `json:"inject_into_prompt,omitempty"` // Inject discovered agents into system prompt (default: true)
	RefreshInterval  string            `json:"refresh_interval,omitempty"`   // Discovery refresh interval (default: "10s")
}

// IsWorkerMode returns true if this agent is in worker mode (receive-only, cannot call other agents)
func (c *AgentsConfig) IsWorkerMode() bool {
	return c.Mode == "worker"
}

// IsPeerMode returns true if this agent is in peer mode (can call and be called by other agents)
// Peer mode is the default — any agent can collaborate with any other agent,
// with guardrails (call chain, max depth, TTL, circuit breaker) preventing loops.
func (c *AgentsConfig) IsPeerMode() bool {
	return c.Mode == "" || c.Mode == "peer"
}

// IsCoordinatorMode returns true if this agent can call other agents
func (c *AgentsConfig) IsCoordinatorMode() bool {
	return c.Mode == "coordinator"
}

// CanCallAgents returns true if this agent is allowed to call other agents
// Both peer and coordinator modes can call; only worker mode cannot.
func (c *AgentsConfig) CanCallAgents() bool {
	return !c.IsWorkerMode()
}

// CanDiscoverAgents returns true if this agent should actively discover peers
// Both peer and coordinator modes discover; worker mode only registers itself.
func (c *AgentsConfig) CanDiscoverAgents() bool {
	return !c.IsWorkerMode()
}

type AgentInfo struct {
	ID              string              `json:"id"`
	Name            string              `json:"name"`
	Description     string              `json:"description"`
	URL             string              `json:"url"`
	APIKey          string              `json:"api_key,omitempty"`
	Capabilities    []string            `json:"capabilities"`
	MCPServers      []string            `json:"mcp_servers,omitempty"`
	Tags            []string            `json:"tags"`
	Enabled         bool                `json:"enabled"`
	Timeout         string              `json:"timeout"`
	Features        map[string]bool     `json:"features,omitempty"`          // Enabled features: tasks, memory, filesystem, mcp, operator, scheduler, voice, agents
	Skills          []AgentSkillInfo    `json:"skills,omitempty"`            // Rich skill info from A2A Agent Card probing
	A2ACapabilities *A2ACapabilityInfo  `json:"a2a_capabilities,omitempty"`  // Capabilities from A2A Agent Card
}

// AgentSkillInfo is a lightweight skill descriptor extracted from an A2A Agent Card.
type AgentSkillInfo struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Tags        []string `json:"tags,omitempty"`
	Examples    []string `json:"examples,omitempty"`
}

// A2ACapabilityInfo stores capability data from an A2A Agent Card.
type A2ACapabilityInfo struct {
	Streaming         bool     `json:"streaming"`
	PushNotifications bool     `json:"push_notifications"`
	InputModes        []string `json:"input_modes,omitempty"`
	OutputModes       []string `json:"output_modes,omitempty"`
}

type AgentCommSettings struct {
	DefaultTimeout      string                `json:"default_timeout"`
	RetryCount          int                   `json:"retry_count"`
	RetryDelay          string                `json:"retry_delay"`
	MaxContextMessages  int                   `json:"max_context_messages"`
	AllowRecursiveCalls bool                  `json:"allow_recursive_calls"`
	MaxCallDepth        int                   `json:"max_call_depth"`
	TrackCallChain      bool                  `json:"track_call_chain"`
	CallTTL             string                `json:"call_ttl,omitempty"`        // Max time for entire call chain (e.g., "120s")
	CircuitBreaker      *CircuitBreakerConfig `json:"circuit_breaker,omitempty"` // Circuit breaker settings
}

// CircuitBreakerConfig configures the circuit breaker for agent calls
type CircuitBreakerConfig struct {
	Enabled          bool   `json:"enabled"`           // Enable circuit breaker
	FailureThreshold int    `json:"failure_threshold"` // Failures before opening (default: 3)
	ResetAfter       string `json:"reset_after"`       // Time before resetting failure count (default: "2m")
	OpenDuration     string `json:"open_duration"`     // How long circuit stays open (default: "30s")
}

type RealtimeConfig struct {
	Enabled      bool   `json:"enabled"`
	Provider     string `json:"provider"`       // "openai", "gemini", or "standard" (default: openai)
	Model        string `json:"model"`          // OpenAI: "gpt-4o-realtime-preview", "gpt-4o-realtime-preview-2024-12-17"
	GeminiModel  string `json:"gemini_model"`   // Gemini: "gemini-2.0-flash-exp"
	Voice        string `json:"voice"`          // OpenAI: "alloy", "ash", "ballad", "coral", "sage", "verse"
	GeminiVoice  string `json:"gemini_voice"`   // Gemini: "Aoede", "Charon", "Fenrir", "Kore", "Puck"
	VADType      string `json:"vad_type"`       // "semantic_vad" or "server_vad"
	GoogleSearch bool   `json:"google_search"`  // Gemini: enable Google Search grounding

	// Standard voice mode: STT + Core LLM + TTS (any provider)
	STT *STTConfig `json:"stt,omitempty"`
	TTS *TTSConfig `json:"tts,omitempty"`
}

// STTConfig configures the speech-to-text provider for standard voice mode
type STTConfig struct {
	Provider string `json:"provider"`         // "whisper" (default), extensible
	Model    string `json:"model"`            // "whisper-1" (default)
	Language string `json:"language"`         // Optional language hint (e.g., "en")
	BaseURL  string `json:"base_url,omitempty"` // Custom API base URL
}

// TTSConfig configures the text-to-speech provider for standard voice mode
type TTSConfig struct {
	Provider   string  `json:"provider"`              // "elevenlabs" (default), extensible
	Voice      string  `json:"voice"`                 // Voice ID or name
	Model      string  `json:"model"`                 // TTS model (e.g., "eleven_turbo_v2_5")
	Speed      float64 `json:"speed,omitempty"`       // Playback speed multiplier
	SampleRate int     `json:"sample_rate,omitempty"` // Output sample rate (default: 24000)
	Stability  float64 `json:"stability,omitempty"`   // Voice stability (0-1)
	Similarity float64 `json:"similarity,omitempty"`  // Voice similarity boost (0-1)
}

type TelemetryConfig struct {
	Enabled       bool     `json:"enabled"`
	Endpoint      string   `json:"endpoint"`                 // Remote endpoint URL
	APIKey        string   `json:"api_key,omitempty"`        // Optional auth key
	BatchSize     int      `json:"batch_size,omitempty"`     // Events per batch (default: 10)
	FlushInterval int      `json:"flush_interval,omitempty"` // Seconds between flushes (default: 30)
	Categories    []string `json:"categories,omitempty"`     // Filter categories (empty = all)
}

type AgentConfig struct {
	ID          string             `json:"id"`
	Name        string             `json:"name"`
	Description string             `json:"description"`
	TestMode    bool               `json:"test_mode"`            // Global test mode - enables mocks for all subsystems
	SetupMode   bool               `json:"setup_mode"`           // Setup mode - agent can modify its own config
	PublicURL   string             `json:"public_url,omitempty"` // Public URL for this agent (e.g., https://agent.example.com)
	LLM         LLMConfig          `json:"llm"`
	MCP         *MCPConfig         `json:"mcp,omitempty"`
	Skills      *SkillsConfig      `json:"skills,omitempty"`
	Scheduler   *SchedulerConfig   `json:"scheduler,omitempty"`
	Tasks       *TasksConfig       `json:"tasks,omitempty"`
	Memory      *MemoryConfig      `json:"memory,omitempty"`
	Operator    *OperatorConfig    `json:"operator,omitempty"`
	Context     *ContextConfig     `json:"context,omitempty"`
	Agents      *AgentsConfig      `json:"agents,omitempty"`
	FileSystem  *FileSystemConfig  `json:"filesystem,omitempty"`
	Realtime    *RealtimeConfig    `json:"realtime,omitempty"`
	Telemetry   *TelemetryConfig   `json:"telemetry,omitempty"`
	Reflection  *ReflectionConfig  `json:"reflection,omitempty"`
	A2A         *A2AConfig         `json:"a2a,omitempty"`
	Version     string             `json:"version"`
}

type Config struct {
	Agent AgentConfig `json:"agent"`
	mu    sync.RWMutex
}

var globalConfig *Config
var once sync.Once

func getConfigFile() string {
	if configPath := os.Getenv("CONFIG_PATH"); configPath != "" {
		return configPath
	}
	return "agent-config.json"
}

func GetConfig() *Config {
	once.Do(func() {
		globalConfig = &Config{}
		globalConfig.loadDefaults()
		globalConfig.Load()
	})
	return globalConfig
}

// buildVersion is set by main.go at startup from ldflags or VERSION file
var buildVersion string

// SetVersion sets the version (called by main at startup)
func SetVersion(v string) {
	buildVersion = v
}

// GetVersion returns the agent version
func GetVersion() string {
	if buildVersion != "" {
		return buildVersion
	}
	// Fallback: read VERSION file (development mode)
	data, err := os.ReadFile("VERSION")
	if err != nil {
		return "1.0.0"
	}
	return strings.TrimSpace(string(data))
}

func (c *Config) loadDefaults() {
	temp := 0.7
	c.Agent = AgentConfig{
		ID:          uuid.New().String(),
		Name:        "AI Assistant",
		Description: "An intelligent AI assistant capable of helping with analysis, writing, coding, and general conversation",
		TestMode:    false, // Default: production mode
		LLM: LLMConfig{
			Provider:     "anthropic",
			Model:        "claude-sonnet-4-5",
			MaxTokens:    4000,
			MaxTurns:     50,
			Temperature:  &temp,
			SystemPrompt: "You are a helpful AI assistant. Be concise and accurate in your responses.",
			Tools:        []string{},
			BuiltinTools: []BuiltinToolConfig{},
			Vision: &VisionConfig{
				Enabled:      true,
				MaxImages:    20,                                         // API limit for Anthropic
				MaxImageSize: 5 * 1024 * 1024,                           // 5MB
				AllowedTypes: []string{"jpeg", "png", "gif", "webp"},
				ResizeImages: true,
				MaxDimension: 1568,                                       // Anthropic recommendation
				PDFConfig: &PDFConfig{
					Enabled:     true,
					MaxFileSize: 32 * 1024 * 1024,                       // 32MB limit
					MaxPages:    100,                                     // Claude's limit
					AllowURLs:   true,
				},
			},
			ParallelTools: &ParallelToolsConfig{
				Enabled:       true,
				MaxConcurrent: 10,
			},
		},
		MCP: &MCPConfig{
			Enabled:     true,
			TestMode:    nil,
			Tools:       []string{},
			Credentials: make(map[string]MCPCredential),
			Servers:     []ExternalMCPServer{},
		},
		Skills: &SkillsConfig{
			Enabled:     false,     // Disabled by default
			Definitions: []Skill{}, // Inline skill definitions
		},
		Scheduler: &SchedulerConfig{
			Enabled:  false,
			Interval: "1h",
			MaxTasks: 100,
		},
		Tasks: &TasksConfig{
			Enabled:         false,  // Disabled by default, enable via config
			AllowScheduling: true,
			AllowRecurring:  true,
			MaxTasks:        100,
			AutoExecute:     false,
		},
		Memory: &MemoryConfig{
			Enabled:             false, // Disabled by default, enable via config
			EmbeddingModel:      "",    // Auto-selected based on provider
			DecisionModel:       "",    // Auto-selected based on main LLM provider
			MaxMemoriesPerQuery: 5,
			MinImportance:       0.3,  // Allow more memories to be stored
			AutoPrune:           true,
			MaxMemories:         10000,
			// Embedding provider: MUST be explicitly set when memory is enabled
			// Options: openai, gemini, jina, voyage, cohere, huggingface, ollama
			EmbeddingProvider: "",     // Required - no default
			OllamaURL:         "http://localhost:11434",
			// Decision model provider: "auto" uses main LLM provider with small model
			DecisionProvider:  "auto",
			// Differential memory: pure RAG-based novelty detection (default)
			DecisionMode:     "differential", // "differential" (default) or "llm"
			NoveltyThreshold: 0.80,           // Similarity < 0.80 = novel
			MinSentenceWords: 4,              // Skip very short fragments
			SkipQuestions:    true,           // Don't store questions
			// File ingestion for RAG
			AutoIngestFiles:    true, // Auto-chunk uploaded text files into memories
			IngestTypes:        []string{"text/plain", "text/markdown", "application/json", "text/csv", "application/pdf"},
			ChunkSize:          1000,
			ChunkOverlap:       200,
			DocumentImportance: 0.5,
		},
		Operator: &OperatorConfig{
			Enabled:         false, // Disabled by default, enable via config
			BrowserProvider: "browserengine",
			TestMode:        nil, // nil = use global test_mode
			DisplayWidth:    1024,
			DisplayHeight:   768,
			AllowedDomains:  []string{},
			BlockedDomains:  []string{},
		},
		Context: &ContextConfig{
			MaxMessages: 0, // No message-count limit — use token-based management
			MaxTokens:   0, // 0 = auto-derive from model context window
			KeepImages:  5, // Keep images in last 5 messages only
			Compaction: &CompactionConfig{
				Enabled: true, // Always on — compacts in background before context fills up
			},
		},
		FileSystem: &FileSystemConfig{
			Enabled:        false,                 // Disabled by default
			MaxFileSize:    10 * 1024 * 1024,      // 10MB per file
			MaxTotalSize:   100 * 1024 * 1024,     // 100MB total storage
			AutoExtract:    true,                  // Automatically extract base64
			MinExtractSize: 10 * 1024,             // Only extract if > 10KB
			Deduplication:  true,                  // Enable deduplication
			AutoCleanup:    true,                  // Enable auto-cleanup
			RetentionDays:  7,                     // Keep files for 7 days
			CleanupOrphans: true,                  // Remove orphaned files
			AllowedTypes:   []string{"image", "document", "audio"}, // Store images, documents, and audio
		},
		Reflection: &ReflectionConfig{
			Enabled:         false,
			Interval:        "24h",
			AfterTask:       false,
			ConversationMin: 0,
			LookbackHours:   24,
			Prompt: `You are performing a self-reflection session. No human user is present. Review your recent activity and think deeply about what you've learned.

Analyze:
1. What tasks/conversations went well and why
2. What could have been handled better
3. Patterns in user requests or issues
4. Knowledge gaps you noticed
5. Strategies that worked effectively

After your analysis, take action: if you identify recurring patterns or preferences that should change your default behavior, use config_set to update your system_prompt accordingly. You can also adjust this reflection prompt itself via config_set if you want future reflections to focus differently.

Don't just observe — adapt.`,
		},
		Version: GetVersion(),
	}
}

func (c *Config) Load() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	configFile := getConfigFile()
	data, err := os.ReadFile(configFile)
	if err != nil {
		if os.IsNotExist(err) {
			// File doesn't exist, save defaults
			return c.saveUnsafe()
		}
		return err
	}

	if err := json.Unmarshal(data, c); err != nil {
		return err
	}

	// Always override version from binary/VERSION file, never from config file
	c.Agent.Version = GetVersion()

	// Override with environment variables if set
	if publicURL := os.Getenv("PUBLIC_URL"); publicURL != "" {
		c.Agent.PublicURL = publicURL
	}

	return nil
}

func (c *Config) Save() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.saveUnsafe()
}

func (c *Config) saveUnsafe() error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	configFile := getConfigFile()
	return os.WriteFile(configFile, data, 0644)
}

func (c *Config) Get() AgentConfig {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// Create a copy of the config
	config := c.Agent

	// Auto-add task tools if task management is enabled
	if config.Tasks != nil && config.Tasks.Enabled {
		taskTools := []string{
			"create_task",
			"list_tasks",
			"get_task",
			"execute_task",
			"update_task",
			"delete_task",
		}

		// Check if task tools are already present
		toolMap := make(map[string]bool)
		for _, tool := range config.LLM.Tools {
			toolMap[tool] = true
		}

		// Add missing task tools
		for _, taskTool := range taskTools {
			if !toolMap[taskTool] {
				config.LLM.Tools = append(config.LLM.Tools, taskTool)
			}
		}
	}

	// Auto-add operator session tools if operator mode is enabled
	if config.Operator != nil && config.Operator.Enabled {
		operatorTools := []string{
			"create_operator_session",
			"connect_operator_session",
			"list_operator_sessions",
			"close_operator_session",
		}

		// Check if operator tools are already present
		toolMap := make(map[string]bool)
		for _, tool := range config.LLM.Tools {
			toolMap[tool] = true
		}

		// Add missing operator tools
		for _, operatorTool := range operatorTools {
			if !toolMap[operatorTool] {
				config.LLM.Tools = append(config.LLM.Tools, operatorTool)
			}
		}
	}

	// Auto-add browser control tools if operator mode is enabled
	if config.Operator != nil && config.Operator.Enabled {
		// For Claude (anthropic provider), use the built-in computer tool
		// For other providers, use our custom browser tool
		if config.LLM.Provider == "anthropic" {
			// Create computer tool with display dimensions from operator config
			displayWidth := config.Operator.DisplayWidth
			displayHeight := config.Operator.DisplayHeight
			displayNumber := 1

			// Set defaults if not configured
			if displayWidth == 0 {
				displayWidth = 1024
			}
			if displayHeight == 0 {
				displayHeight = 768
			}

			computerToolType := ComputerToolVersion(config.LLM.Model)
			computerTool := BuiltinToolConfig{
				Type:            computerToolType,
				Name:            "computer",
				DisplayWidthPx:  &displayWidth,
				DisplayHeightPx: &displayHeight,
				DisplayNumber:   &displayNumber,
			}

			// Check if computer tool is already present
			toolExists := false
			toolIndex := -1
			for i, tool := range config.LLM.BuiltinTools {
				if IsComputerTool(tool.Type) {
					toolExists = true
					toolIndex = i
					break
				}
			}

			if !toolExists {
				// Add new computer tool
				config.LLM.BuiltinTools = append(config.LLM.BuiltinTools, computerTool)
			} else {
				// Always update type to match current model (e.g. switching to Sonnet 4.6)
				config.LLM.BuiltinTools[toolIndex].Type = computerToolType
				// Update display dimensions if not already set
				if config.LLM.BuiltinTools[toolIndex].DisplayWidthPx == nil {
					config.LLM.BuiltinTools[toolIndex].DisplayWidthPx = &displayWidth
				}
				if config.LLM.BuiltinTools[toolIndex].DisplayHeightPx == nil {
					config.LLM.BuiltinTools[toolIndex].DisplayHeightPx = &displayHeight
				}
				if config.LLM.BuiltinTools[toolIndex].DisplayNumber == nil {
					config.LLM.BuiltinTools[toolIndex].DisplayNumber = &displayNumber
				}
			}
		} else {
			// For non-Claude providers (Gemini, OpenAI, etc.), add custom browser tool
			// This requires the model to have vision capabilities
			toolMap := make(map[string]bool)
			for _, tool := range config.LLM.Tools {
				toolMap[tool] = true
			}
			if !toolMap["browser"] {
				config.LLM.Tools = append(config.LLM.Tools, "browser")
			}
		}
	}

	// Auto-add agent communication tools if agents mode is enabled
	if config.Agents != nil && config.Agents.Enabled {
		agentTools := []string{
			"call_agent",
			"list_available_agents",
		}

		// Check if agent tools are already present
		toolMap := make(map[string]bool)
		for _, tool := range config.LLM.Tools {
			toolMap[tool] = true
		}

		// Add missing agent tools
		for _, agentTool := range agentTools {
			if !toolMap[agentTool] {
				config.LLM.Tools = append(config.LLM.Tools, agentTool)
			}
		}
	}

	// Auto-add subscription tools if MCP webhooks are enabled (webhooks is now an array of server names)
	if config.MCP != nil && config.MCP.Enabled && len(config.MCP.Webhooks) > 0 {
		subscriptionTools := []string{
			"mcp_webhooks_list",
			"subscribe",
			"subscriptions_list",
			"unsubscribe",
			"update_subscription",
		}

		// Check if subscription tools are already present
		toolMap := make(map[string]bool)
		for _, tool := range config.LLM.Tools {
			toolMap[tool] = true
		}

		// Add missing subscription tools
		for _, subTool := range subscriptionTools {
			if !toolMap[subTool] {
				config.LLM.Tools = append(config.LLM.Tools, subTool)
			}
		}
	}

	return config
}

func (c *Config) Update(newConfig AgentConfig) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.Agent = newConfig
	return c.saveUnsafe()
}

// Set updates the agent config in memory without saving
func (c *Config) Set(newConfig AgentConfig) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Agent = newConfig
}

// SetSetupMode enables or disables setup mode
func (c *Config) SetSetupMode(enabled bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Agent.SetupMode = enabled
	return c.saveUnsafe()
}

// IsSetupMode returns true if the agent is in setup mode
func (c *Config) IsSetupMode() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.Agent.SetupMode
}

func (c *Config) GetLLMConfig() LLMConfig {
	c.mu.RLock()
	defer c.mu.RUnlock()

	llmConfig := c.Agent.LLM

	// Auto-add agent tools if agent communication is enabled
	if c.Agent.Agents != nil && c.Agent.Agents.Enabled {
		agentTools := []string{
			"call_agent",
			"list_available_agents",
		}

		// Check if agent tools are already present
		toolMap := make(map[string]bool)
		for _, tool := range llmConfig.Tools {
			toolMap[tool] = true
		}

		// Add missing agent tools
		for _, agentTool := range agentTools {
			if !toolMap[agentTool] {
				llmConfig.Tools = append(llmConfig.Tools, agentTool)
			}
		}
	}

	// Auto-add task tools if task management is enabled
	if c.Agent.Tasks != nil && c.Agent.Tasks.Enabled {
		taskTools := []string{
			"create_task",
			"list_tasks",
			"get_task",
			"execute_task",
			"update_task",
			"delete_task",
		}

		// Check if task tools are already present
		toolMap := make(map[string]bool)
		for _, tool := range llmConfig.Tools {
			toolMap[tool] = true
		}

		// Add missing task tools
		for _, taskTool := range taskTools {
			if !toolMap[taskTool] {
				llmConfig.Tools = append(llmConfig.Tools, taskTool)
			}
		}
	}

	// Auto-add operator session tools if operator mode is enabled
	if c.Agent.Operator != nil && c.Agent.Operator.Enabled {
		operatorTools := []string{
			"create_operator_session",
			"connect_operator_session",
			"list_operator_sessions",
			"close_operator_session",
		}

		// Check if operator tools are already present
		toolMap := make(map[string]bool)
		for _, tool := range llmConfig.Tools {
			toolMap[tool] = true
		}

		// Add missing operator tools
		for _, operatorTool := range operatorTools {
			if !toolMap[operatorTool] {
				llmConfig.Tools = append(llmConfig.Tools, operatorTool)
			}
		}
	}

	// Auto-add browser control tools if operator mode is enabled
	if c.Agent.Operator != nil && c.Agent.Operator.Enabled {
		// For Claude (anthropic provider), use the built-in computer tool
		// For other providers, use our custom browser tool
		if llmConfig.Provider == "anthropic" {
			// Create computer tool with display dimensions from operator config
			displayWidth := c.Agent.Operator.DisplayWidth
			displayHeight := c.Agent.Operator.DisplayHeight
			displayNumber := 1

			// Set defaults if not configured
			if displayWidth == 0 {
				displayWidth = 1024
			}
			if displayHeight == 0 {
				displayHeight = 768
			}

			computerToolType := ComputerToolVersion(llmConfig.Model)
			computerTool := BuiltinToolConfig{
				Type:            computerToolType,
				Name:            "computer",
				DisplayWidthPx:  &displayWidth,
				DisplayHeightPx: &displayHeight,
				DisplayNumber:   &displayNumber,
			}

			// Check if computer tool is already present
			toolExists := false
			toolIndex := -1
			for i, tool := range llmConfig.BuiltinTools {
				if IsComputerTool(tool.Type) {
					toolExists = true
					toolIndex = i
					break
				}
			}

			if !toolExists {
				// Add new computer tool
				llmConfig.BuiltinTools = append(llmConfig.BuiltinTools, computerTool)
			} else {
				// Always update type to match current model (e.g. switching to Sonnet 4.6)
				llmConfig.BuiltinTools[toolIndex].Type = computerToolType
				// Update display dimensions if not already set
				if llmConfig.BuiltinTools[toolIndex].DisplayWidthPx == nil {
					llmConfig.BuiltinTools[toolIndex].DisplayWidthPx = &displayWidth
				}
				if llmConfig.BuiltinTools[toolIndex].DisplayHeightPx == nil {
					llmConfig.BuiltinTools[toolIndex].DisplayHeightPx = &displayHeight
				}
				if llmConfig.BuiltinTools[toolIndex].DisplayNumber == nil {
					llmConfig.BuiltinTools[toolIndex].DisplayNumber = &displayNumber
				}
			}
		} else if llmConfig.Provider == "openai" {
			// For OpenAI (GPT-5.4+), use native Responses API computer tool
			displayWidth := c.Agent.Operator.DisplayWidth
			displayHeight := c.Agent.Operator.DisplayHeight

			if displayWidth == 0 {
				displayWidth = 1024
			}
			if displayHeight == 0 {
				displayHeight = 768
			}

			computerTool := BuiltinToolConfig{
				Type:            "computer", // OpenAI native computer tool type
				Name:            "computer",
				DisplayWidthPx:  &displayWidth,
				DisplayHeightPx: &displayHeight,
			}

			// Check if computer tool is already present
			toolExists := false
			for _, tool := range llmConfig.BuiltinTools {
				if tool.Name == "computer" {
					toolExists = true
					break
				}
			}

			if !toolExists {
				llmConfig.BuiltinTools = append(llmConfig.BuiltinTools, computerTool)
			}
		} else {
			// For other providers (Gemini, Groq, etc.), add custom browser tool
			// This requires the model to have vision capabilities
			toolMap := make(map[string]bool)
			for _, tool := range llmConfig.Tools {
				toolMap[tool] = true
			}
			if !toolMap["browser"] {
				llmConfig.Tools = append(llmConfig.Tools, "browser")
			}
		}
	}

	// Auto-add subscription tools if MCP webhooks are enabled (webhooks is now an array of server names)
	if c.Agent.MCP != nil && c.Agent.MCP.Enabled && len(c.Agent.MCP.Webhooks) > 0 {
		subscriptionTools := []string{
			"mcp_webhooks_list",
			"subscribe",
			"subscriptions_list",
			"unsubscribe",
			"update_subscription",
		}

		// Check if subscription tools are already present
		toolMap := make(map[string]bool)
		for _, tool := range llmConfig.Tools {
			toolMap[tool] = true
		}

		// Add missing subscription tools
		for _, subTool := range subscriptionTools {
			if !toolMap[subTool] {
				llmConfig.Tools = append(llmConfig.Tools, subTool)
			}
		}
	}

	return llmConfig
}

func (c *Config) Reset() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.loadDefaults()
	return c.saveUnsafe()
}

// IsParallelToolsEnabled checks if parallel tools are enabled in the LLM config
// Defaults to true if not explicitly configured
func IsParallelToolsEnabled(llmConfig LLMConfig) bool {
	if llmConfig.ParallelTools == nil {
		return true // Default to enabled
	}
	return llmConfig.ParallelTools.Enabled
}

// GetParallelToolsPrompt returns the system prompt for parallel tool execution
func GetParallelToolsPrompt() string {
	return "\n\nFor maximum efficiency, whenever you need to perform multiple independent operations, invoke all relevant tools simultaneously rather than sequentially. However, if one tool's input depends on another tool's output, call them sequentially — wait for the first result before making the next call."
}

// IsTestMode checks if test mode is enabled for a subsystem
// Priority: subsystem.test_mode > global.test_mode > false
func IsTestMode(globalTestMode bool, subsystemTestMode *bool) bool {
	if subsystemTestMode != nil {
		return *subsystemTestMode // Explicit subsystem override
	}
	return globalTestMode // Use global default
}

// IsAgentInjectionEnabled checks if agent injection into system prompt is enabled
// Default: true (inject agents into prompt for immediate visibility)
func IsAgentInjectionEnabled(agentsConfig *AgentsConfig) bool {
	if agentsConfig == nil || !agentsConfig.Enabled {
		return false
	}
	if agentsConfig.InjectIntoPrompt == nil {
		return true // Default to enabled
	}
	return *agentsConfig.InjectIntoPrompt
}

// GetAgentRefreshInterval returns the discovery refresh interval
// Default: "10s"
func GetAgentRefreshInterval(agentsConfig *AgentsConfig) string {
	if agentsConfig == nil || agentsConfig.RefreshInterval == "" {
		return "10s"
	}
	return agentsConfig.RefreshInterval
}

// GetMemoryChunkSize returns the chunk size for document ingestion
// Default: 1000
func GetMemoryChunkSize(memoryConfig *MemoryConfig) int {
	if memoryConfig == nil || memoryConfig.ChunkSize <= 0 {
		return 1000
	}
	return memoryConfig.ChunkSize
}

// GetMemoryChunkOverlap returns the chunk overlap for document ingestion
// Default: 200
func GetMemoryChunkOverlap(memoryConfig *MemoryConfig) int {
	if memoryConfig == nil || memoryConfig.ChunkOverlap <= 0 {
		return 200
	}
	return memoryConfig.ChunkOverlap
}

// GetMemoryDocumentImportance returns the default importance for document chunks
// Default: 0.5
func GetMemoryDocumentImportance(memoryConfig *MemoryConfig) float64 {
	if memoryConfig == nil || memoryConfig.DocumentImportance <= 0 {
		return 0.5
	}
	return memoryConfig.DocumentImportance
}

// GetMemoryIngestTypes returns the MIME types to auto-ingest
// Default: ["text/plain", "text/markdown", "application/json", "text/csv", "application/pdf"]
func GetMemoryIngestTypes(memoryConfig *MemoryConfig) []string {
	if memoryConfig == nil || len(memoryConfig.IngestTypes) == 0 {
		return []string{"text/plain", "text/markdown", "application/json", "text/csv", "application/pdf"}
	}
	return memoryConfig.IngestTypes
}

// IsMemoryAutoIngestEnabled checks if auto file ingestion is enabled
func IsMemoryAutoIngestEnabled(memoryConfig *MemoryConfig) bool {
	if memoryConfig == nil || !memoryConfig.Enabled {
		return false
	}
	return memoryConfig.AutoIngestFiles
}

// IsAutoExtractMemoriesEnabled checks if auto memory extraction from conversations is enabled
// Default: true (when nil or not set)
func IsAutoExtractMemoriesEnabled(memoryConfig *MemoryConfig) bool {
	if memoryConfig == nil || !memoryConfig.Enabled {
		return false
	}
	// Default to true if not explicitly set
	if memoryConfig.AutoExtractMemories == nil {
		return true
	}
	return *memoryConfig.AutoExtractMemories
}

// GetEmbeddingProvider returns the embedding provider to use
// Default: "auto" (selects based on available API keys)
func GetEmbeddingProvider(memoryConfig *MemoryConfig) string {
	if memoryConfig == nil || memoryConfig.EmbeddingProvider == "" {
		return "auto"
	}
	return memoryConfig.EmbeddingProvider
}

// GetDecisionMode returns the memory decision mode
// Default: "differential" (pure RAG-based novelty detection)
func GetDecisionMode(memoryConfig *MemoryConfig) string {
	if memoryConfig == nil || memoryConfig.DecisionMode == "" {
		return "differential"
	}
	return memoryConfig.DecisionMode
}

// GetNoveltyThreshold returns the novelty threshold for differential mode
// Default: 0.80 (memories with similarity < 0.80 are considered novel)
func GetNoveltyThreshold(memoryConfig *MemoryConfig) float64 {
	if memoryConfig == nil || memoryConfig.NoveltyThreshold <= 0 {
		return 0.80
	}
	return memoryConfig.NoveltyThreshold
}

// GetMinSentenceWords returns the minimum words required for a sentence to be considered
// Default: 4
func GetMinSentenceWords(memoryConfig *MemoryConfig) int {
	if memoryConfig == nil || memoryConfig.MinSentenceWords <= 0 {
		return 4
	}
	return memoryConfig.MinSentenceWords
}

// ShouldSkipQuestions returns whether questions should be skipped in differential mode
// Default: true
func ShouldSkipQuestions(memoryConfig *MemoryConfig) bool {
	if memoryConfig == nil {
		return true
	}
	return memoryConfig.SkipQuestions
}

// GetOllamaURL returns the Ollama API URL
// Default: "http://localhost:11434"
func GetOllamaURL(memoryConfig *MemoryConfig) string {
	if memoryConfig == nil || memoryConfig.OllamaURL == "" {
		return "http://localhost:11434"
	}
	return memoryConfig.OllamaURL
}

// GetEmbeddingModel returns the embedding model to use
// For OpenAI default: "text-embedding-3-small"
// For Ollama default: "nomic-embed-text"
func GetEmbeddingModel(memoryConfig *MemoryConfig) string {
	if memoryConfig == nil || memoryConfig.EmbeddingModel == "" {
		if GetEmbeddingProvider(memoryConfig) == "ollama" {
			return "nomic-embed-text"
		}
		return "text-embedding-3-small"
	}
	return memoryConfig.EmbeddingModel
}

// BuildFeatures builds a features map from the agent configuration
// This is used to announce what features this agent has enabled
func BuildFeatures(cfg *AgentConfig) map[string]bool {
	features := make(map[string]bool)

	features["tasks"] = cfg.Tasks != nil && cfg.Tasks.Enabled
	features["memory"] = cfg.Memory != nil && cfg.Memory.Enabled
	features["filesystem"] = cfg.FileSystem != nil && cfg.FileSystem.Enabled
	features["mcp"] = cfg.MCP != nil && cfg.MCP.Enabled
	features["operator"] = cfg.Operator != nil && cfg.Operator.Enabled
	features["scheduler"] = cfg.Scheduler != nil && cfg.Scheduler.Enabled
	features["realtime"] = cfg.Realtime != nil && cfg.Realtime.Enabled
	features["agents"] = cfg.Agents != nil && cfg.Agents.Enabled
	features["webhooks"] = cfg.MCP != nil && cfg.MCP.Enabled && len(cfg.MCP.Webhooks) > 0
	features["skills"] = cfg.Skills != nil && cfg.Skills.Enabled
	features["a2a"] = cfg.A2A != nil && cfg.A2A.Enabled

	return features
}