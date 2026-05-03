// PicoClaw - Ultra-lightweight personal AI agent

package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/zhazhaku/reef/pkg/agent/interfaces"
	"github.com/zhazhaku/reef/pkg/audio/tts"
	"github.com/zhazhaku/reef/pkg/bus"
	"github.com/zhazhaku/reef/pkg/channels"
	"github.com/zhazhaku/reef/pkg/commands"
	"github.com/zhazhaku/reef/pkg/config"
	"github.com/zhazhaku/reef/pkg/logger"
	"github.com/zhazhaku/reef/pkg/providers"
	"github.com/zhazhaku/reef/pkg/skills"
	"github.com/zhazhaku/reef/pkg/state"
	"github.com/zhazhaku/reef/pkg/tools"
)

func NewAgentLoop(
	cfg *config.Config,
	msgBus *bus.MessageBus,
	provider providers.LLMProvider,
) *AgentLoop {
	registry := NewAgentRegistry(cfg, provider)

	// Set up shared fallback chain with rate limiting.
	cooldown := providers.NewCooldownTracker()
	rl := providers.NewRateLimiterRegistry()
	// Register rate limiters for all agents' candidates so that RPM limits
	// configured in ModelConfig are enforced before each LLM call.
	for _, agentID := range registry.ListAgentIDs() {
		if agent, ok := registry.GetAgent(agentID); ok {
			rl.RegisterCandidates(agent.Candidates)
			rl.RegisterCandidates(agent.LightCandidates)
		}
	}
	fallbackChain := providers.NewFallbackChain(cooldown, rl)

	// Create state manager using default agent's workspace for channel recording
	defaultAgent := registry.GetDefaultAgent()
	var stateManager *state.Manager
	if defaultAgent != nil {
		stateManager = state.NewManager(defaultAgent.Workspace)
	}

	eventBus := NewEventBus()

	// Determine worker pool size from config (default: 1 = sequential)
	workerPoolSize := cfg.Agents.Defaults.MaxParallelTurns
	if workerPoolSize <= 0 {
		workerPoolSize = 1
	}

	al := &AgentLoop{
		bus:         msgBus,
		cfg:         cfg,
		registry:    registry,
		state:       stateManager,
		eventBus:    eventBus,
		fallback:    fallbackChain,
		cmdRegistry: commands.NewRegistry(commands.BuiltinDefinitions()),
		steering:    newSteeringQueue(parseSteeringMode(cfg.Agents.Defaults.SteeringMode)),
		workerSem:   make(chan struct{}, workerPoolSize),
	}

	// Initialize Hermes capability architecture
	al.hermesMode = ParseHermesMode(cfg.Hermes.HermesMode())
	al.hermesGuard = NewHermesGuard(al.hermesMode)

	// Register Hermes role prompt contributor (if not Full mode)
	if al.hermesMode != HermesFull {
		hermesContributor := newHermesRoleContributor(al.hermesMode)
		for _, agentID := range registry.ListAgentIDs() {
			if a, ok := registry.GetAgent(agentID); ok {
				if err := a.ContextBuilder.RegisterPromptContributor(hermesContributor); err != nil {
					logger.WarnCF("agent", "Failed to register Hermes prompt contributor",
						map[string]any{"agent_id": agentID, "error": err.Error()})
				}
			}
		}
		logger.InfoCF("agent", "Hermes mode initialized",
			map[string]any{"mode": string(al.hermesMode)})
	}
	al.providerFactory = providers.CreateProviderFromConfig
	al.hooks = NewHookManager(eventBus)
	configureHookManagerFromConfig(al.hooks, cfg)

	// Register the dangerous tool approver to require human approval
	// for destructive/sensitive tool operations (HITL safety layer).
	// OWASP LLM07/LLM08: Insecure Plugin Design / Excessive Agency.
	dangerousApprover := NewDangerousToolApprover()
	_ = al.hooks.Mount(HookRegistration{
		Name:     "dangerous-tool-approver",
		Hook:     dangerousApprover,
		Priority: 50,
		Source:   HookSourceInProcess,
	})

	al.contextManager = al.resolveContextManager()

	// Register shared tools to all agents (now that al is created)
	registerSharedTools(al, cfg, msgBus, registry, provider)

	return al
}

func registerSharedTools(
	al *AgentLoop,
	cfg *config.Config,
	msgBus interfaces.MessageBus,
	registry *AgentRegistry,
	provider providers.LLMProvider,
) {
	allowReadPaths := buildAllowReadPatterns(cfg)
	var ttsProvider tts.TTSProvider
	if cfg.Tools.IsToolEnabled("send_tts") {
		ttsProvider = tts.DetectTTS(cfg)
		if ttsProvider == nil {
			logger.WarnCF("voice-tts", "send_tts enabled but no TTS provider configured", nil)
		}
	}

	for _, agentID := range registry.ListAgentIDs() {
		agent, ok := registry.GetAgent(agentID)
		if !ok {
			continue
		}

		// === Hermes Coordinator mode: only register coordination tools ===
		if al.hermesMode == HermesCoordinator {
			registerCoordinatorTools(al, agent, cfg, msgBus)
			continue
		}

		// === Hermes Executor / Full mode: register all tools ===
		// (reef_submit_task is not in the default registration, so Executor
		// mode doesn't need special handling here)

		if cfg.Tools.IsToolEnabled("web") {
			searchTool, err := tools.NewWebSearchTool(tools.WebSearchToolOptionsFromConfig(cfg))
			if err != nil {
				logger.ErrorCF("agent", "Failed to create web search tool", map[string]any{"error": err.Error()})
			} else if searchTool != nil {
				agent.Tools.Register(searchTool)
			}
		}
		if cfg.Tools.IsToolEnabled("web_fetch") {
			fetchTool, err := tools.NewWebFetchToolWithProxy(
				50000,
				cfg.Tools.Web.Proxy,
				cfg.Tools.Web.Format,
				cfg.Tools.Web.FetchLimitBytes,
				cfg.Tools.Web.PrivateHostWhitelist)
			if err != nil {
				logger.ErrorCF("agent", "Failed to create web fetch tool", map[string]any{"error": err.Error()})
			} else {
				agent.Tools.Register(fetchTool)
			}
		}

		// Hardware tools (I2C, SPI) - Linux only, returns error on other platforms
		if cfg.Tools.IsToolEnabled("i2c") {
			agent.Tools.Register(tools.NewI2CTool())
		}
		if cfg.Tools.IsToolEnabled("spi") {
			agent.Tools.Register(tools.NewSPITool())
		}

		// Message tool
		if cfg.Tools.IsToolEnabled("message") {
			messageTool := tools.NewMessageTool()
			messageTool.SetSendCallback(func(
				ctx context.Context,
				channel, chatID, content, replyToMessageID string,
			) error {
				pubCtx, pubCancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer pubCancel()
				outboundCtx := bus.NewOutboundContext(channel, chatID, replyToMessageID)
				outboundAgentID, outboundSessionKey, outboundScope := outboundTurnMetadata(
					tools.ToolAgentID(ctx),
					tools.ToolSessionKey(ctx),
					tools.ToolSessionScope(ctx),
				)
				return msgBus.PublishOutbound(pubCtx, bus.OutboundMessage{
					Context:          outboundCtx,
					AgentID:          outboundAgentID,
					SessionKey:       outboundSessionKey,
					Scope:            outboundScope,
					Content:          content,
					ReplyToMessageID: replyToMessageID,
				})
			})
			agent.Tools.Register(messageTool)
		}
		if cfg.Tools.IsToolEnabled("reaction") {
			reactionTool := tools.NewReactionTool()
			reactionTool.SetReactionCallback(func(ctx context.Context, channel, chatID, messageID string) error {
				if al.channelManager == nil {
					return fmt.Errorf("channel manager not configured")
				}
				ch, ok := al.channelManager.GetChannel(channel)
				if !ok {
					return fmt.Errorf("channel %s not found", channel)
				}
				rc, ok := ch.(channels.ReactionCapable)
				if !ok {
					return fmt.Errorf("channel %s does not support reactions", channel)
				}
				_, err := rc.ReactToMessage(ctx, chatID, messageID)
				return err
			})
			agent.Tools.Register(reactionTool)
		}

		// Send file tool (outbound media via MediaStore — store injected later by SetMediaStore)
		if cfg.Tools.IsToolEnabled("send_file") {
			sendFileTool := tools.NewSendFileTool(
				agent.Workspace,
				cfg.Agents.Defaults.RestrictToWorkspace,
				cfg.Agents.Defaults.GetMaxMediaSize(),
				nil,
				allowReadPaths,
			)
			agent.Tools.Register(sendFileTool)
		}

		if ttsProvider != nil {
			agent.Tools.Register(tools.NewSendTTSTool(ttsProvider, nil))
		}

		if cfg.Tools.IsToolEnabled("load_image") {
			loadImageTool := tools.NewLoadImageTool(
				agent.Workspace,
				cfg.Agents.Defaults.RestrictToWorkspace,
				cfg.Agents.Defaults.GetMaxMediaSize(),
				nil,
				allowReadPaths,
			)
			agent.Tools.Register(loadImageTool)
		}

		// Skill discovery and installation tools
		skills_enabled := cfg.Tools.IsToolEnabled("skills")
		find_skills_enable := cfg.Tools.IsToolEnabled("find_skills")
		install_skills_enable := cfg.Tools.IsToolEnabled("install_skill")
		if skills_enabled && (find_skills_enable || install_skills_enable) {
			registryMgr := skills.NewRegistryManagerFromToolsConfig(cfg.Tools.Skills)

			if find_skills_enable {
				searchCache := skills.NewSearchCache(
					cfg.Tools.Skills.SearchCache.MaxSize,
					time.Duration(cfg.Tools.Skills.SearchCache.TTLSeconds)*time.Second,
				)
				agent.Tools.Register(tools.NewFindSkillsTool(registryMgr, searchCache))
			}

			if install_skills_enable {
				agent.Tools.Register(tools.NewInstallSkillTool(registryMgr, agent.Workspace))
			}
		}

		// Spawn and spawn_status tools share a SubagentManager.
		// Construct it when either tool is enabled (both require subagent).
		spawnEnabled := cfg.Tools.IsToolEnabled("spawn")
		spawnStatusEnabled := cfg.Tools.IsToolEnabled("spawn_status")
		if (spawnEnabled || spawnStatusEnabled) && cfg.Tools.IsToolEnabled("subagent") {
			subagentManager := tools.NewSubagentManager(provider, agent.Model, agent.Workspace)
			subagentManager.SetLLMOptions(agent.MaxTokens, agent.Temperature)

			// Inject a media resolver so the legacy RunToolLoop fallback path can
			// resolve media:// refs in the same way the main AgentLoop does.
			// This keeps subagent vision support working even when the optimized
			// sub-turn spawner path is unavailable.
			subagentManager.SetMediaResolver(func(msgs []providers.Message) []providers.Message {
				return resolveMediaRefs(msgs, al.mediaStore, cfg.Agents.Defaults.GetMaxMediaSize())
			})

			// Set the spawner that links into AgentLoop's turnState
			subagentManager.SetSpawner(func(
				ctx context.Context,
				task, label, targetAgentID string,
				tls *tools.ToolRegistry,
				maxTokens int,
				temperature float64,
				hasMaxTokens, hasTemperature bool,
			) (*tools.ToolResult, error) {
				// 1. Recover parent Turn State from Context
				parentTS := turnStateFromContext(ctx)
				if parentTS == nil {
					// Fallback: If no turnState exists in context, create an isolated ad-hoc root turn state
					// so that the tool can still function outside of an agent loop (e.g. tests, raw invocations).
					parentTS = &turnState{
						ctx:            ctx,
						turnID:         "adhoc-root",
						depth:          0,
						session:        nil, // Ephemeral session not needed for adhoc spawn
						pendingResults: make(chan *tools.ToolResult, 16),
						concurrencySem: make(chan struct{}, 5),
					}
				}

				// 2. Build Tools slice from registry
				var tlSlice []tools.Tool
				for _, name := range tls.List() {
					if t, ok := tls.Get(name); ok {
						tlSlice = append(tlSlice, t)
					}
				}

				// 3. System Prompt
				systemPrompt := "You are a subagent. Complete the given task independently and report the result.\n" +
					"You have access to tools - use them as needed to complete your task.\n" +
					"After completing the task, provide a clear summary of what was done.\n\n" +
					"Task: " + task

				// 4. Resolve Model
				modelToUse := agent.Model
				if targetAgentID != "" {
					if targetAgent, ok := al.GetRegistry().GetAgent(targetAgentID); ok {
						modelToUse = targetAgent.Model
					}
				}

				// 5. Build SubTurnConfig
				cfg := SubTurnConfig{
					Model:        modelToUse,
					Tools:        tlSlice,
					SystemPrompt: systemPrompt,
				}
				if hasMaxTokens {
					cfg.MaxTokens = maxTokens
				}

				// 6. Spawn SubTurn
				return spawnSubTurn(ctx, al, parentTS, cfg)
			})

			// Clone the parent's tool registry so subagents can use all
			// tools registered so far (file, web, etc.) but NOT spawn/
			// spawn_status which are added below — preventing recursive
			// subagent spawning.
			subagentManager.SetTools(agent.Tools.Clone())
			if spawnEnabled {
				spawnTool := tools.NewSpawnTool(subagentManager)
				spawnTool.SetSpawner(NewSubTurnSpawner(al))
				currentAgentID := agentID
				spawnTool.SetAllowlistChecker(func(targetAgentID string) bool {
					return registry.CanSpawnSubagent(currentAgentID, targetAgentID)
				})

				agent.Tools.Register(spawnTool)

				// Also register the synchronous subagent tool
				subagentTool := tools.NewSubagentTool(subagentManager)
				subagentTool.SetSpawner(NewSubTurnSpawner(al))
				agent.Tools.Register(subagentTool)
			}
			if spawnStatusEnabled {
				agent.Tools.Register(tools.NewSpawnStatusTool(subagentManager))
			}
		} else if (spawnEnabled || spawnStatusEnabled) && !cfg.Tools.IsToolEnabled("subagent") {
			logger.WarnCF("agent", "spawn/spawn_status tools require subagent to be enabled", nil)
		}
	}
}

// registerCoordinatorTools registers only the tools allowed for a
// Hermes Coordinator agent. The coordinator's role is to understand
// user intent, delegate tasks via reef_submit_task, and aggregate results.
// It does NOT have direct execution tools (web_search, exec, read_file, etc.).
func registerCoordinatorTools(
	al *AgentLoop,
	agent *AgentInstance,
	cfg *config.Config,
	msgBus interfaces.MessageBus,
) {
	// Message tool — coordinator needs to send messages to users
	if cfg.Tools.IsToolEnabled("message") {
		messageTool := tools.NewMessageTool()
		messageTool.SetSendCallback(func(
			ctx context.Context,
			channel, chatID, content, replyToMessageID string,
		) error {
			pubCtx, pubCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer pubCancel()
			outboundCtx := bus.NewOutboundContext(channel, chatID, replyToMessageID)
			outboundAgentID, outboundSessionKey, outboundScope := outboundTurnMetadata(
				tools.ToolAgentID(ctx),
				tools.ToolSessionKey(ctx),
				tools.ToolSessionScope(ctx),
			)
			return msgBus.PublishOutbound(pubCtx, bus.OutboundMessage{
				Context:          outboundCtx,
				AgentID:          outboundAgentID,
				SessionKey:       outboundSessionKey,
				Scope:            outboundScope,
				Content:          content,
				ReplyToMessageID: replyToMessageID,
			})
		})
		agent.Tools.Register(messageTool)
	}

	// Reaction tool — lightweight interaction
	if cfg.Tools.IsToolEnabled("reaction") {
		reactionTool := tools.NewReactionTool()
		reactionTool.SetReactionCallback(func(ctx context.Context, channel, chatID, messageID string) error {
			if al.channelManager == nil {
				return fmt.Errorf("channel manager not configured")
			}
			ch, ok := al.channelManager.GetChannel(channel)
			if !ok {
				return fmt.Errorf("channel %s not found", channel)
			}
			rc, ok := ch.(channels.ReactionCapable)
			if !ok {
				return fmt.Errorf("channel %s does not support reactions", channel)
			}
			_, err := rc.ReactToMessage(ctx, chatID, messageID)
			return err
		})
		agent.Tools.Register(reactionTool)
	}

	// Cron tool — coordinator may need to schedule tasks
	// (registered if available in the future)

	// Reef coordination tools — these will be registered by the
	// Reef integration layer when it's initialized:
	//   - reef_submit_task
	//   - reef_query_task
	//   - reef_status

	logger.InfoCF("agent", "Registered coordinator tools (Hermes mode)",
		map[string]any{
			"agent_id": agent.ID,
			"tools":    strings.Join(agent.Tools.List(), ","),
		})
}
