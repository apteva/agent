package config

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	appConfig "github.com/apteva/agent/config"
	"github.com/apteva/agent/mcp"
	"github.com/apteva/agent/scheduler"
	"github.com/apteva/agent/skills"
)

var globalDB *sql.DB
var discoveryCallback func(enabled bool, group string) error
var discoveryRunningCallback func() bool
var memoryCallback func(enabled bool) error
var filesystemCallback func(enabled bool) error
var telemetryCallback func(enabled bool) error
var tasksCallback func(enabled bool) error

// SetDatabase sets the global database for scheduler operations
func SetDatabase(db *sql.DB) {
	globalDB = db
}

// SetDiscoveryCallback sets the callback function for discovery service updates
func SetDiscoveryCallback(callback func(enabled bool, group string) error) {
	discoveryCallback = callback
}

// SetDiscoveryRunningCallback sets the callback function to check if discovery is running
func SetDiscoveryRunningCallback(callback func() bool) {
	discoveryRunningCallback = callback
}

// SetMemoryCallback sets the callback function for memory manager updates
func SetMemoryCallback(callback func(enabled bool) error) {
	memoryCallback = callback
}

// SetFilesystemCallback sets the callback function for filesystem updates
func SetFilesystemCallback(callback func(enabled bool) error) {
	filesystemCallback = callback
}

// SetTelemetryCallback sets the callback function for telemetry updates
func SetTelemetryCallback(callback func(enabled bool) error) {
	telemetryCallback = callback
}

// SetTasksCallback sets the callback function for tasks updates
func SetTasksCallback(callback func(enabled bool) error) {
	tasksCallback = callback
}

// HandleConfig handles GET and POST requests for agent configuration
func HandleConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		cfg := appConfig.GetConfig()
		agentConfig := cfg.Get()
		sendJSON(w, http.StatusOK, agentConfig)

	case http.MethodPost:
		var updateConfig map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&updateConfig); err != nil {
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}

		cfg := appConfig.GetConfig()
		currentConfig := cfg.Get()

		// Apply partial updates
		if id, ok := updateConfig["id"].(string); ok {
			currentConfig.ID = id
		}
		if name, ok := updateConfig["name"].(string); ok {
			currentConfig.Name = name
		}
		if description, ok := updateConfig["description"].(string); ok {
			currentConfig.Description = description
		}
		if version, ok := updateConfig["version"].(string); ok {
			currentConfig.Version = version
		}
		if publicURL, exists := updateConfig["public_url"]; exists {
			if publicURLStr, ok := publicURL.(string); ok {
				currentConfig.PublicURL = publicURLStr
			} else if publicURL == nil {
				currentConfig.PublicURL = ""
			}
		}

		// Setup mode
		if setupMode, ok := updateConfig["setup_mode"].(bool); ok {
			currentConfig.SetupMode = setupMode
			log.Printf("🔧 Setup mode: %v", setupMode)
		}

		// LLM config
		if llmConfig, ok := updateConfig["llm"].(map[string]interface{}); ok {
			// String fields - handle both string values and null (to clear)
			if val, exists := llmConfig["provider"]; exists {
				if str, ok := val.(string); ok {
					currentConfig.LLM.Provider = str
				} else if val == nil {
					currentConfig.LLM.Provider = ""
				}
			}
			if val, exists := llmConfig["model"]; exists {
				if str, ok := val.(string); ok {
					currentConfig.LLM.Model = str
				} else if val == nil {
					currentConfig.LLM.Model = ""
				}
			}
			if val, exists := llmConfig["system_prompt"]; exists {
				if str, ok := val.(string); ok {
					currentConfig.LLM.SystemPrompt = str
				} else if val == nil {
					currentConfig.LLM.SystemPrompt = ""
				}
			}
			// Numeric fields
			if maxTokens, ok := llmConfig["max_tokens"].(float64); ok {
				currentConfig.LLM.MaxTokens = int(maxTokens)
			}
			if temp, ok := llmConfig["temperature"].(float64); ok {
				currentConfig.LLM.Temperature = &temp
			}
			// Array fields
			if tools, ok := llmConfig["tools"].([]interface{}); ok {
				var toolNames []string
				for _, tool := range tools {
					if toolName, ok := tool.(string); ok {
						toolNames = append(toolNames, toolName)
					}
				}
				currentConfig.LLM.Tools = toolNames
			}
			// Tool loading config
			if toolLoading, ok := llmConfig["tool_loading"].(map[string]interface{}); ok {
				log.Printf("🔧 Updating tool_loading config: %+v", toolLoading)
				if currentConfig.LLM.ToolLoading == nil {
					currentConfig.LLM.ToolLoading = &appConfig.ToolLoadingConfig{}
				}
				if mode, ok := toolLoading["mode"].(string); ok {
					currentConfig.LLM.ToolLoading.Mode = mode
					log.Printf("   mode=%s", mode)
				}
				if strategy, ok := toolLoading["strategy"].(string); ok {
					currentConfig.LLM.ToolLoading.Strategy = strategy
				}
				if alwaysLoad, ok := toolLoading["always_load"].([]interface{}); ok {
					var tools []string
					for _, t := range alwaysLoad {
						if str, ok := t.(string); ok {
							tools = append(tools, str)
						}
					}
					currentConfig.LLM.ToolLoading.AlwaysLoad = tools
				}
				if maxTools, ok := toolLoading["max_tools"].(float64); ok {
					currentConfig.LLM.ToolLoading.MaxTools = int(maxTools)
				}
				// BM25 config
				if bm25, ok := toolLoading["bm25"].(map[string]interface{}); ok {
					if currentConfig.LLM.ToolLoading.BM25 == nil {
						currentConfig.LLM.ToolLoading.BM25 = &appConfig.BM25Config{}
					}
					if topK, ok := bm25["top_k"].(float64); ok {
						currentConfig.LLM.ToolLoading.BM25.TopK = int(topK)
					}
					if threshold, ok := bm25["threshold"].(float64); ok {
						currentConfig.LLM.ToolLoading.BM25.Threshold = threshold
					}
				}
				// Regex config
				if regex, ok := toolLoading["regex"].(map[string]interface{}); ok {
					if currentConfig.LLM.ToolLoading.Regex == nil {
						currentConfig.LLM.ToolLoading.Regex = &appConfig.RegexConfig{}
					}
					if rules, ok := regex["rules"].([]interface{}); ok {
						var regexRules []appConfig.RegexRule
						for _, r := range rules {
							if ruleMap, ok := r.(map[string]interface{}); ok {
								rule := appConfig.RegexRule{}
								if pattern, ok := ruleMap["pattern"].(string); ok {
									rule.Pattern = pattern
								}
								if tools, ok := ruleMap["tools"].([]interface{}); ok {
									for _, t := range tools {
										if str, ok := t.(string); ok {
											rule.Tools = append(rule.Tools, str)
										}
									}
								}
								regexRules = append(regexRules, rule)
							}
						}
						currentConfig.LLM.ToolLoading.Regex.Rules = regexRules
					}
				}
				// Keyword config
				if keyword, ok := toolLoading["keyword"].(map[string]interface{}); ok {
					if currentConfig.LLM.ToolLoading.Keyword == nil {
						currentConfig.LLM.ToolLoading.Keyword = &appConfig.KeywordConfig{}
					}
					if rules, ok := keyword["rules"].([]interface{}); ok {
						var keywordRules []appConfig.KeywordRule
						for _, r := range rules {
							if ruleMap, ok := r.(map[string]interface{}); ok {
								rule := appConfig.KeywordRule{}
								if keywords, ok := ruleMap["keywords"].([]interface{}); ok {
									for _, k := range keywords {
										if str, ok := k.(string); ok {
											rule.Keywords = append(rule.Keywords, str)
										}
									}
								}
								if tools, ok := ruleMap["tools"].([]interface{}); ok {
									for _, t := range tools {
										if str, ok := t.(string); ok {
											rule.Tools = append(rule.Tools, str)
										}
									}
								}
								keywordRules = append(keywordRules, rule)
							}
						}
						currentConfig.LLM.ToolLoading.Keyword.Rules = keywordRules
					}
				}
			}
			// Builtin tools (Anthropic API tools like web_search, computer)
			if builtinTools, ok := llmConfig["builtin_tools"].([]interface{}); ok {
				var builtinToolConfigs []appConfig.BuiltinToolConfig
				for _, bt := range builtinTools {
					if btMap, ok := bt.(map[string]interface{}); ok {
						var builtinTool appConfig.BuiltinToolConfig
						if t, ok := btMap["type"].(string); ok {
							builtinTool.Type = t
						}
						if n, ok := btMap["name"].(string); ok {
							builtinTool.Name = n
						}
						if maxUses, ok := btMap["max_uses"].(float64); ok {
							maxUsesInt := int(maxUses)
							builtinTool.MaxUses = &maxUsesInt
						}
						if domains, ok := btMap["allowed_domains"].([]interface{}); ok {
							for _, d := range domains {
								if ds, ok := d.(string); ok {
									builtinTool.AllowedDomains = append(builtinTool.AllowedDomains, ds)
								}
							}
						}
						if domains, ok := btMap["blocked_domains"].([]interface{}); ok {
							for _, d := range domains {
								if ds, ok := d.(string); ok {
									builtinTool.BlockedDomains = append(builtinTool.BlockedDomains, ds)
								}
							}
						}
						if w, ok := btMap["display_width_px"].(float64); ok {
							wInt := int(w)
							builtinTool.DisplayWidthPx = &wInt
						}
						if h, ok := btMap["display_height_px"].(float64); ok {
							hInt := int(h)
							builtinTool.DisplayHeightPx = &hInt
						}
						if n, ok := btMap["display_number"].(float64); ok {
							nInt := int(n)
							builtinTool.DisplayNumber = &nInt
						}
						builtinToolConfigs = append(builtinToolConfigs, builtinTool)
					}
				}
				currentConfig.LLM.BuiltinTools = builtinToolConfigs
			}
		}

		// MCP config
		mcpConfigChanged := false
		if mcpConfig, ok := updateConfig["mcp"].(map[string]interface{}); ok {
			if currentConfig.MCP == nil {
				currentConfig.MCP = &appConfig.MCPConfig{}
			}
			if enabled, ok := mcpConfig["enabled"].(bool); ok {
				if currentConfig.MCP.Enabled != enabled {
					mcpConfigChanged = true
				}
				currentConfig.MCP.Enabled = enabled
			}
			// String fields - handle both string values and null (to clear)
			if val, exists := mcpConfig["base_url"]; exists {
				var newVal string
				if str, ok := val.(string); ok {
					newVal = str
				}
				if currentConfig.MCP.BaseURL != newVal {
					mcpConfigChanged = true
				}
				currentConfig.MCP.BaseURL = newVal
			}
			if val, exists := mcpConfig["api_key"]; exists {
				var newVal string
				if str, ok := val.(string); ok {
					newVal = str
				}
				if currentConfig.MCP.APIKey != newVal {
					mcpConfigChanged = true
				}
				currentConfig.MCP.APIKey = newVal
			}
			if val, exists := mcpConfig["timeout"]; exists {
				if str, ok := val.(string); ok {
					currentConfig.MCP.Timeout = str
				} else if val == nil {
					currentConfig.MCP.Timeout = ""
				}
			}
			if val, exists := mcpConfig["cache_ttl"]; exists {
				if str, ok := val.(string); ok {
					currentConfig.MCP.CacheTTL = str
				} else if val == nil {
					currentConfig.MCP.CacheTTL = ""
				}
			}
			if retryCount, ok := mcpConfig["retry_count"].(float64); ok {
				currentConfig.MCP.RetryCount = int(retryCount)
			}
			if tools, ok := mcpConfig["tools"].([]interface{}); ok {
				var toolNames []string
				for _, tool := range tools {
					if toolName, ok := tool.(string); ok {
						toolNames = append(toolNames, toolName)
					}
				}
				currentConfig.MCP.Tools = toolNames
				mcpConfigChanged = true
			}
			if resources, ok := mcpConfig["resources"].([]interface{}); ok {
				var resourceServers []string
				for _, res := range resources {
					if resName, ok := res.(string); ok {
						resourceServers = append(resourceServers, resName)
					}
				}
				currentConfig.MCP.Resources = resourceServers
				mcpConfigChanged = true
			}
			if resourceConfig, ok := mcpConfig["resource_config"].(map[string]interface{}); ok {
				if currentConfig.MCP.ResourceConfig == nil {
					currentConfig.MCP.ResourceConfig = &appConfig.MCPResourceConfig{}
				}
				if enabled, ok := resourceConfig["enabled"].(bool); ok {
					currentConfig.MCP.ResourceConfig.Enabled = enabled
					mcpConfigChanged = true
				}
				if autoSync, ok := resourceConfig["auto_sync"].(bool); ok {
					currentConfig.MCP.ResourceConfig.AutoSync = autoSync
				}
				if syncInterval, ok := resourceConfig["sync_interval"].(string); ok {
					currentConfig.MCP.ResourceConfig.SyncInterval = syncInterval
				}
				if subscribe, ok := resourceConfig["subscribe"].(bool); ok {
					currentConfig.MCP.ResourceConfig.Subscribe = subscribe
				}
				if maxSize, ok := resourceConfig["max_size"].(float64); ok {
					currentConfig.MCP.ResourceConfig.MaxSize = int(maxSize)
				}
				if filters, ok := resourceConfig["filters"].([]interface{}); ok {
					var filterList []string
					for _, f := range filters {
						if str, ok := f.(string); ok {
							filterList = append(filterList, str)
						}
					}
					currentConfig.MCP.ResourceConfig.Filters = filterList
				}
			}
			if testMode, ok := mcpConfig["test_mode"].(bool); ok {
				currentConfig.MCP.TestMode = &testMode
			}
			if webhooks, ok := mcpConfig["webhooks"].([]interface{}); ok {
				var webhookServers []string
				for _, wh := range webhooks {
					if whName, ok := wh.(string); ok {
						webhookServers = append(webhookServers, whName)
					}
				}
				currentConfig.MCP.Webhooks = webhookServers
				mcpConfigChanged = true
			}
			if credentials, ok := mcpConfig["credentials"].(map[string]interface{}); ok {
				// Initialize credentials map if nil
				if currentConfig.MCP.Credentials == nil {
					currentConfig.MCP.Credentials = make(map[string]appConfig.MCPCredential)
				}
				// Process each credential - just store credential_id reference to backend
				for credKey, credValue := range credentials {
					if credMap, ok := credValue.(map[string]interface{}); ok {
						cred := appConfig.MCPCredential{}

						// credential_id can be string or number
						if credID, ok := credMap["credential_id"].(string); ok {
							cred.CredentialID = credID
						} else if credID, ok := credMap["credential_id"].(float64); ok {
							cred.CredentialID = fmt.Sprintf("%.0f", credID)
						}
						if provider, ok := credMap["provider"].(string); ok {
							cred.Provider = provider
						} else {
							cred.Provider = credKey
						}
						if name, ok := credMap["name"].(string); ok {
							cred.Name = name
						}

						currentConfig.MCP.Credentials[credKey] = cred
					}
				}
				mcpConfigChanged = true
			}
			// External MCP servers (standard protocol)
			if servers, ok := mcpConfig["servers"].([]interface{}); ok {
				var externalServers []appConfig.ExternalMCPServer
				for _, srv := range servers {
					if srvMap, ok := srv.(map[string]interface{}); ok {
						server := appConfig.ExternalMCPServer{}
						if name, ok := srvMap["name"].(string); ok {
							server.Name = name
						}
						if url, ok := srvMap["url"].(string); ok {
							server.URL = url
						}
						if enabled, ok := srvMap["enabled"].(bool); ok {
							server.Enabled = enabled
						}
						if headers, ok := srvMap["headers"].(map[string]interface{}); ok {
							server.Headers = make(map[string]string)
							for k, v := range headers {
								if vs, ok := v.(string); ok {
									server.Headers[k] = vs
								}
							}
						}
						if server.Name != "" && server.URL != "" {
							externalServers = append(externalServers, server)
						}
					}
				}
				currentConfig.MCP.Servers = externalServers
				mcpConfigChanged = true
				log.Printf("🔌 MCP External servers updated: %d servers", len(externalServers))
			}
		}

		// Scheduler config
		schedulerConfigChanged := false
		if schedulerConfig, ok := updateConfig["scheduler"].(map[string]interface{}); ok {
			if currentConfig.Scheduler == nil {
				currentConfig.Scheduler = &appConfig.SchedulerConfig{}
			}
			if enabled, ok := schedulerConfig["enabled"].(bool); ok {
				currentConfig.Scheduler.Enabled = enabled
				schedulerConfigChanged = true
			}
			if interval, ok := schedulerConfig["interval"].(string); ok {
				currentConfig.Scheduler.Interval = interval
				schedulerConfigChanged = true
			}
			if maxTasks, ok := schedulerConfig["max_tasks"].(float64); ok {
				currentConfig.Scheduler.MaxTasks = int(maxTasks)
				schedulerConfigChanged = true
			}
		}

		// Tasks config
		tasksConfigChanged := false
		if tasksConfig, ok := updateConfig["tasks"].(map[string]interface{}); ok {
			if currentConfig.Tasks == nil {
				currentConfig.Tasks = &appConfig.TasksConfig{}
			}
			if enabled, ok := tasksConfig["enabled"].(bool); ok {
				tasksConfigChanged = true
				currentConfig.Tasks.Enabled = enabled
			}
			if allowScheduling, ok := tasksConfig["allow_scheduling"].(bool); ok {
				currentConfig.Tasks.AllowScheduling = allowScheduling
			}
			if allowRecurring, ok := tasksConfig["allow_recurring"].(bool); ok {
				currentConfig.Tasks.AllowRecurring = allowRecurring
			}
			if maxTasks, ok := tasksConfig["max_tasks"].(float64); ok {
				currentConfig.Tasks.MaxTasks = int(maxTasks)
			}
			if autoExecute, ok := tasksConfig["auto_execute"].(bool); ok {
				currentConfig.Tasks.AutoExecute = autoExecute
			}
		}

		// Register/unregister task tools if tasks enabled state changed
		if tasksConfigChanged && tasksCallback != nil {
			if err := tasksCallback(currentConfig.Tasks.Enabled); err != nil {
				log.Printf("Warning: Failed to update tasks: %v", err)
			}
		}

		// Memory config
		memoryConfigChanged := false
		if memoryConfig, ok := updateConfig["memory"].(map[string]interface{}); ok {
			if currentConfig.Memory == nil {
				currentConfig.Memory = &appConfig.MemoryConfig{}
			}
			if enabled, ok := memoryConfig["enabled"].(bool); ok {
				memoryConfigChanged = true
				currentConfig.Memory.Enabled = enabled
			}
			// String fields - handle both string values and null (to clear)
			if val, exists := memoryConfig["embedding_model"]; exists {
				if str, ok := val.(string); ok {
					currentConfig.Memory.EmbeddingModel = str
				} else if val == nil {
					currentConfig.Memory.EmbeddingModel = ""
				}
			}
			if val, exists := memoryConfig["decision_model"]; exists {
				if str, ok := val.(string); ok {
					currentConfig.Memory.DecisionModel = str
				} else if val == nil {
					currentConfig.Memory.DecisionModel = ""
				}
			}
			if val, exists := memoryConfig["embedding_provider"]; exists {
				if str, ok := val.(string); ok {
					currentConfig.Memory.EmbeddingProvider = str
				} else if val == nil {
					currentConfig.Memory.EmbeddingProvider = ""
				}
			}
			if val, exists := memoryConfig["ollama_url"]; exists {
				if str, ok := val.(string); ok {
					currentConfig.Memory.OllamaURL = str
				} else if val == nil {
					currentConfig.Memory.OllamaURL = ""
				}
			}
			// Numeric fields
			if maxMemoriesPerQuery, ok := memoryConfig["max_memories_per_query"].(float64); ok {
				currentConfig.Memory.MaxMemoriesPerQuery = int(maxMemoriesPerQuery)
			}
			if minImportance, ok := memoryConfig["min_importance"].(float64); ok {
				currentConfig.Memory.MinImportance = minImportance
			}
			if minSimilarity, ok := memoryConfig["min_similarity"].(float64); ok {
				currentConfig.Memory.MinSimilarity = minSimilarity
			}
			if maxMemories, ok := memoryConfig["max_memories"].(float64); ok {
				currentConfig.Memory.MaxMemories = int(maxMemories)
			}
			if chunkSize, ok := memoryConfig["chunk_size"].(float64); ok {
				currentConfig.Memory.ChunkSize = int(chunkSize)
			}
			if chunkOverlap, ok := memoryConfig["chunk_overlap"].(float64); ok {
				currentConfig.Memory.ChunkOverlap = int(chunkOverlap)
			}
			if documentImportance, ok := memoryConfig["document_importance"].(float64); ok {
				currentConfig.Memory.DocumentImportance = documentImportance
			}
			// Boolean fields
			if autoPrune, ok := memoryConfig["auto_prune"].(bool); ok {
				currentConfig.Memory.AutoPrune = autoPrune
			}
			if autoIngestFiles, ok := memoryConfig["auto_ingest_files"].(bool); ok {
				currentConfig.Memory.AutoIngestFiles = autoIngestFiles
			}
			// Handle auto_extract_memories (pointer to bool, defaults to true when nil)
			if val, exists := memoryConfig["auto_extract_memories"]; exists {
				if autoExtract, ok := val.(bool); ok {
					currentConfig.Memory.AutoExtractMemories = &autoExtract
				} else if val == nil {
					currentConfig.Memory.AutoExtractMemories = nil // Reset to default (true)
				}
			}
			// Array fields
			if ingestTypes, ok := memoryConfig["ingest_types"].([]interface{}); ok {
				var types []string
				for _, t := range ingestTypes {
					if str, ok := t.(string); ok {
						types = append(types, str)
					}
				}
				currentConfig.Memory.IngestTypes = types
			}
			// Note: memory callback triggered after cfg.Update() so new config is visible
		}

		// Operator config
		if operatorConfig, ok := updateConfig["operator"].(map[string]interface{}); ok {
			if currentConfig.Operator == nil {
				currentConfig.Operator = &appConfig.OperatorConfig{}
			}
			if enabled, ok := operatorConfig["enabled"].(bool); ok {
				currentConfig.Operator.Enabled = enabled
				// When operator is disabled, clear only computer builtin tools and operator tools
				if !enabled {
					log.Printf("🖥️  Operator disabled, clearing computer builtin tools and operator tools")
					// Filter out only computer-related builtin tools, keep others (web_search, web_fetch, etc.)
					var filteredBuiltinTools []appConfig.BuiltinToolConfig
					for _, bt := range currentConfig.LLM.BuiltinTools {
						// Keep non-computer builtin tools
						if bt.Type != "computer_20250124" && bt.Name != "computer" {
							filteredBuiltinTools = append(filteredBuiltinTools, bt)
						}
					}
					currentConfig.LLM.BuiltinTools = filteredBuiltinTools
					// Remove operator-related tools from LLM tools
					operatorTools := map[string]bool{
						"create_operator_session": true,
						"list_operator_sessions":  true,
						"close_operator_session":  true,
					}
					var filteredTools []string
					for _, tool := range currentConfig.LLM.Tools {
						if !operatorTools[tool] {
							filteredTools = append(filteredTools, tool)
						}
					}
					currentConfig.LLM.Tools = filteredTools
				}
			}
			if displayWidth, ok := operatorConfig["display_width"].(float64); ok {
				currentConfig.Operator.DisplayWidth = int(displayWidth)
			}
			if displayHeight, ok := operatorConfig["display_height"].(float64); ok {
				currentConfig.Operator.DisplayHeight = int(displayHeight)
			}
			if maxActionsPerTurn, ok := operatorConfig["max_actions_per_turn"].(float64); ok {
				currentConfig.Operator.MaxActionsPerTurn = int(maxActionsPerTurn)
			}
		}

		// Agents config
		agentsConfigChanged := false
		if agentsConfig, ok := updateConfig["agents"].(map[string]interface{}); ok {
			if currentConfig.Agents == nil {
				currentConfig.Agents = &appConfig.AgentsConfig{}
			}
			if enabled, ok := agentsConfig["enabled"].(bool); ok {
				if currentConfig.Agents.Enabled != enabled {
					agentsConfigChanged = true
				}
				currentConfig.Agents.Enabled = enabled
			}
			// String fields - handle both string values and null (to clear)
			if val, exists := agentsConfig["mode"]; exists {
				var newVal string
				if str, ok := val.(string); ok {
					newVal = str
				}
				if currentConfig.Agents.Mode != newVal {
					agentsConfigChanged = true
				}
				currentConfig.Agents.Mode = newVal
			}
			if val, exists := agentsConfig["group"]; exists {
				var newVal string
				if str, ok := val.(string); ok {
					newVal = str
				}
				if currentConfig.Agents.Group != newVal {
					agentsConfigChanged = true
				}
				currentConfig.Agents.Group = newVal
			}
			if val, exists := agentsConfig["gossip_seed"]; exists {
				if str, ok := val.(string); ok {
					currentConfig.Agents.GossipSeed = str
				} else if val == nil {
					currentConfig.Agents.GossipSeed = ""
				}
			}
			if gossipPort, ok := agentsConfig["gossip_port"].(float64); ok {
				currentConfig.Agents.GossipPort = int(gossipPort)
			}
		}

		// Filesystem config
		filesystemConfigChanged := false
		if filesystemConfig, ok := updateConfig["filesystem"].(map[string]interface{}); ok {
			if currentConfig.FileSystem == nil {
				currentConfig.FileSystem = &appConfig.FileSystemConfig{}
			}
			if enabled, ok := filesystemConfig["enabled"].(bool); ok {
				filesystemConfigChanged = true
				currentConfig.FileSystem.Enabled = enabled
			}
			if maxFileSize, ok := filesystemConfig["max_file_size"].(float64); ok {
				currentConfig.FileSystem.MaxFileSize = int64(maxFileSize)
			}
			if maxTotalSize, ok := filesystemConfig["max_total_size"].(float64); ok {
				currentConfig.FileSystem.MaxTotalSize = int64(maxTotalSize)
			}
			if autoExtract, ok := filesystemConfig["auto_extract"].(bool); ok {
				currentConfig.FileSystem.AutoExtract = autoExtract
			}
			if minExtractSize, ok := filesystemConfig["min_extract_size"].(float64); ok {
				currentConfig.FileSystem.MinExtractSize = int(minExtractSize)
			}
			if deduplication, ok := filesystemConfig["deduplication"].(bool); ok {
				currentConfig.FileSystem.Deduplication = deduplication
			}
			if autoCleanup, ok := filesystemConfig["auto_cleanup"].(bool); ok {
				currentConfig.FileSystem.AutoCleanup = autoCleanup
			}
			if retentionDays, ok := filesystemConfig["retention_days"].(float64); ok {
				currentConfig.FileSystem.RetentionDays = int(retentionDays)
			}
			if cleanupOrphans, ok := filesystemConfig["cleanup_orphans"].(bool); ok {
				currentConfig.FileSystem.CleanupOrphans = cleanupOrphans
			}
			// Note: filesystem callback triggered after cfg.Update() so new config is visible
		}

		// Context config
		if contextConfig, ok := updateConfig["context"].(map[string]interface{}); ok {
			if currentConfig.Context == nil {
				currentConfig.Context = &appConfig.ContextConfig{}
			}
			if maxMessages, ok := contextConfig["max_messages"].(float64); ok {
				currentConfig.Context.MaxMessages = int(maxMessages)
			}
			if keepImages, ok := contextConfig["keep_images"].(float64); ok {
				currentConfig.Context.KeepImages = int(keepImages)
			}
		}

		// Telemetry config
		telemetryConfigChanged := false
		if telemetryConfig, ok := updateConfig["telemetry"].(map[string]interface{}); ok {
			if currentConfig.Telemetry == nil {
				currentConfig.Telemetry = &appConfig.TelemetryConfig{}
			}
			if enabled, ok := telemetryConfig["enabled"].(bool); ok {
				if currentConfig.Telemetry.Enabled != enabled {
					telemetryConfigChanged = true
				}
				currentConfig.Telemetry.Enabled = enabled
			}
			if val, exists := telemetryConfig["endpoint"]; exists {
				if str, ok := val.(string); ok {
					currentConfig.Telemetry.Endpoint = str
				} else if val == nil {
					currentConfig.Telemetry.Endpoint = ""
				}
			}
			if val, exists := telemetryConfig["api_key"]; exists {
				if str, ok := val.(string); ok {
					currentConfig.Telemetry.APIKey = str
				} else if val == nil {
					currentConfig.Telemetry.APIKey = ""
				}
			}
			if batchSize, ok := telemetryConfig["batch_size"].(float64); ok {
				currentConfig.Telemetry.BatchSize = int(batchSize)
			}
			if flushInterval, ok := telemetryConfig["flush_interval"].(float64); ok {
				currentConfig.Telemetry.FlushInterval = int(flushInterval)
			}
			if categories, ok := telemetryConfig["categories"].([]interface{}); ok {
				var catList []string
				for _, c := range categories {
					if str, ok := c.(string); ok {
						catList = append(catList, str)
					}
				}
				currentConfig.Telemetry.Categories = catList
			}
		}

		// Skills config
		if skillsConfig, ok := updateConfig["skills"].(map[string]interface{}); ok {
			if currentConfig.Skills == nil {
				currentConfig.Skills = &appConfig.SkillsConfig{}
			}
			if enabled, ok := skillsConfig["enabled"].(bool); ok {
				currentConfig.Skills.Enabled = enabled
			}
			if definitions, ok := skillsConfig["definitions"].([]interface{}); ok {
				var skills []appConfig.Skill
				for _, def := range definitions {
					if defMap, ok := def.(map[string]interface{}); ok {
						skill := appConfig.Skill{}
						if name, ok := defMap["name"].(string); ok {
							skill.Name = name
						}
						if desc, ok := defMap["description"].(string); ok {
							skill.Description = desc
						}
						if instr, ok := defMap["instructions"].(string); ok {
							skill.Instructions = instr
						}
						if icon, ok := defMap["icon"].(string); ok {
							skill.Icon = icon
						}
						if cat, ok := defMap["category"].(string); ok {
							skill.Category = cat
						}
						if enabled, ok := defMap["enabled"].(bool); ok {
							skill.Enabled = enabled
						}
						if tags, ok := defMap["tags"].([]interface{}); ok {
							for _, t := range tags {
								if str, ok := t.(string); ok {
									skill.Tags = append(skill.Tags, str)
								}
							}
						}
						if tools, ok := defMap["tools"].([]interface{}); ok {
							for _, t := range tools {
								if str, ok := t.(string); ok {
									skill.Tools = append(skill.Tools, str)
								}
							}
						}
						skills = append(skills, skill)
					}
				}
				currentConfig.Skills.Definitions = skills
			}
		}

		// Realtime config
		if realtimeConfig, ok := updateConfig["realtime"].(map[string]interface{}); ok {
			if currentConfig.Realtime == nil {
				currentConfig.Realtime = &appConfig.RealtimeConfig{}
			}
			if enabled, ok := realtimeConfig["enabled"].(bool); ok {
				currentConfig.Realtime.Enabled = enabled
			}
			if val, exists := realtimeConfig["provider"]; exists {
				if str, ok := val.(string); ok {
					currentConfig.Realtime.Provider = str
				} else if val == nil {
					currentConfig.Realtime.Provider = ""
				}
			}
			if val, exists := realtimeConfig["model"]; exists {
				if str, ok := val.(string); ok {
					currentConfig.Realtime.Model = str
				} else if val == nil {
					currentConfig.Realtime.Model = ""
				}
			}
			if val, exists := realtimeConfig["gemini_model"]; exists {
				if str, ok := val.(string); ok {
					currentConfig.Realtime.GeminiModel = str
				} else if val == nil {
					currentConfig.Realtime.GeminiModel = ""
				}
			}
			if val, exists := realtimeConfig["voice"]; exists {
				if str, ok := val.(string); ok {
					currentConfig.Realtime.Voice = str
				} else if val == nil {
					currentConfig.Realtime.Voice = ""
				}
			}
			if val, exists := realtimeConfig["gemini_voice"]; exists {
				if str, ok := val.(string); ok {
					currentConfig.Realtime.GeminiVoice = str
				} else if val == nil {
					currentConfig.Realtime.GeminiVoice = ""
				}
			}
			if val, exists := realtimeConfig["vad_type"]; exists {
				if str, ok := val.(string); ok {
					currentConfig.Realtime.VADType = str
				} else if val == nil {
					currentConfig.Realtime.VADType = ""
				}
			}
			if googleSearch, ok := realtimeConfig["google_search"].(bool); ok {
				currentConfig.Realtime.GoogleSearch = googleSearch
			}
		}

		// Update the global config FIRST so callbacks can read the new values
		if err := cfg.Update(currentConfig); err != nil {
			log.Printf("⚠️  Failed to save config: %v", err)
			http.Error(w, fmt.Sprintf("Failed to save config: %v", err), http.StatusInternalServerError)
			return
		}
		log.Printf("✅ Config saved successfully, tool_loading=%+v", currentConfig.LLM.ToolLoading)

		// Trigger memory manager callback if enabled changed
		if memoryConfigChanged && memoryCallback != nil {
			if err := memoryCallback(currentConfig.Memory.Enabled); err != nil {
				log.Printf("⚠️  Failed to update memory manager: %v", err)
			}
		}

		// Trigger filesystem callback if enabled changed
		if filesystemConfigChanged && filesystemCallback != nil {
			if err := filesystemCallback(currentConfig.FileSystem.Enabled); err != nil {
				log.Printf("⚠️  Failed to update filesystem: %v", err)
			}
		}

		// Trigger telemetry callback if config changed
		if telemetryConfigChanged && telemetryCallback != nil {
			if err := telemetryCallback(currentConfig.Telemetry.Enabled); err != nil {
				log.Printf("⚠️  Failed to update telemetry: %v", err)
			}
		}

		// Reinitialize skills manager if skills config was updated
		if currentConfig.Skills != nil {
			skills.GetManager().Initialize(currentConfig.Skills)
			log.Printf("🎯 Skills manager reinitialized: %d skills", skills.GetManager().Count())
		}

		// Dynamically update discovery service if agents config changed (AFTER cfg.Update so new config is visible)
		if agentsConfigChanged {
			log.Printf("🔄 Agents config changed: enabled=%v, group=%s", currentConfig.Agents.Enabled, currentConfig.Agents.Group)
			if discoveryCallback != nil {
				log.Printf("🔄 Calling discovery callback...")
				if err := discoveryCallback(currentConfig.Agents.Enabled, currentConfig.Agents.Group); err != nil {
					log.Printf("⚠️  Failed to update discovery service: %v", err)
				} else {
					log.Printf("✅ Discovery service updated successfully")
				}
			} else {
				log.Printf("⚠️  No discovery callback registered")
			}
		} else if currentConfig.Agents != nil && currentConfig.Agents.Enabled && currentConfig.Agents.Group != "" {
			// Even if config didn't change, ensure discovery is running if it should be
			// This handles the case where config was already set but discovery never started
			isRunning := false
			if discoveryRunningCallback != nil {
				isRunning = discoveryRunningCallback()
			}
			if !isRunning {
				log.Printf("🔄 Discovery should be running but isn't, starting it (enabled=%v, group=%s)", currentConfig.Agents.Enabled, currentConfig.Agents.Group)
				if discoveryCallback != nil {
					if err := discoveryCallback(currentConfig.Agents.Enabled, currentConfig.Agents.Group); err != nil {
						log.Printf("⚠️  Failed to start discovery service: %v", err)
					} else {
						log.Printf("✅ Discovery service started successfully")
					}
				}
			}
		}

		// Restart scheduler if scheduler config changed
		if schedulerConfigChanged && globalDB != nil {
			log.Printf("🔄 Scheduler configuration changed, restarting scheduler...")
			go func() {
				// Convert config.SchedulerConfig to scheduler.SchedulerConfig
				schedulerCfg := scheduler.SchedulerConfig{
					Enabled:  currentConfig.Scheduler.Enabled,
					Interval: currentConfig.Scheduler.Interval,
					MaxTasks: currentConfig.Scheduler.MaxTasks,
				}

				// Get the global scheduler instance
				agentScheduler := scheduler.GetScheduler(schedulerCfg)

				// Stop if running
				if agentScheduler.IsRunning() {
					log.Printf("⏹️  Stopping existing scheduler...")
					agentScheduler.Stop()
				}

				// Update the scheduler configuration with new interval
				if err := agentScheduler.UpdateConfig(schedulerCfg); err != nil {
					log.Printf("⚠️  Failed to update scheduler config: %v", err)
					return
				}

				// Start if enabled
				if currentConfig.Scheduler.Enabled {
					agentScheduler.SetDatabase(globalDB)
					if err := agentScheduler.Start(); err != nil {
						log.Printf("⚠️  Failed to start scheduler after config update: %v", err)
					} else {
						log.Printf("✅ Scheduler restarted successfully with interval: %s", currentConfig.Scheduler.Interval)
					}
				} else {
					log.Printf("✅ Scheduler stopped (disabled in config)")
				}
			}()
		}

		// Refresh MCP cache if MCP config changed and is enabled
		if mcpConfigChanged && currentConfig.MCP != nil && currentConfig.MCP.Enabled {
			log.Printf("🔄 MCP configuration changed, refreshing cache...")
			go func() {
				// Refresh gateway cache
				err := mcp.RefreshMCPCache(currentConfig.MCP)
				if err != nil {
					log.Printf("⚠️  Failed to refresh MCP cache after config update: %v", err)
					log.Printf("💡 You can manually refresh via: POST /mcp/refresh")
				} else {
					cache := mcp.GetMCPCache()
					tools := cache.GetTools("")
					log.Printf("✅ MCP cache refreshed: %d tools loaded from %s", len(tools), currentConfig.MCP.BaseURL)
				}

				// Initialize external MCP servers if configured
				if len(currentConfig.MCP.Servers) > 0 {
					log.Printf("🔌 Initializing %d external MCP servers...", len(currentConfig.MCP.Servers))
					if err := mcp.InitializeExternalServers(currentConfig.MCP.Servers); err != nil {
						log.Printf("⚠️  Failed to initialize some external servers: %v", err)
					}
					externalManager := mcp.GetExternalServerManager()
					externalTools := externalManager.GetTools()
					log.Printf("✅ External MCP: %d tools from %d servers",
						len(externalTools), len(externalManager.GetServerNames()))
				}
			}()
		}

		sendJSON(w, http.StatusOK, map[string]interface{}{
			"success": true,
			"message": "Configuration updated successfully",
			"config":  currentConfig,
		})

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// sendJSON is a helper to send JSON responses
func sendJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}
