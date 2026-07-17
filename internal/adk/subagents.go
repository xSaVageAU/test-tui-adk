package adk

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/agent/llmagent"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/tool"

	"tui-testing/internal/appdir"
)

const (
	agentConfigFile      = "agent.json"
	agentInstructionFile = "instruction.md"
)

// agentFileConfig is one agent's definition as discovered on disk — the
// shared shape for both the root agent (a single agent.json/
// instruction.md pair directly under appdir's root — see rootagent.go)
// and every sub-agent (one subdirectory per agent under appdir's
// "subagents" directory, named for it — see loadSubAgentConfigs below).
// Instruction is kept in its own file rather than a JSON string field so
// it can be written and diffed as prose, not escaped inside quotes.
type agentFileConfig struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Tools       []string `json:"tools,omitempty"`
	// Provider/Model pick what this agent runs on — see models.go's
	// buildModel. Both empty means "use the built-in default"
	// (root — see rootagent.go) or "inherit the root agent's resolved
	// model" (a sub-agent — see buildSubAgents).
	Provider string `json:"provider,omitempty"`
	Model    string `json:"model,omitempty"`
	// MCPServers names configured MCP servers (see mcpservers.go) this
	// agent should have as toolsets — resolved only by buildRootAgent.
	// Root-only for now: a sub-agent's own HITL confirmation can never be
	// resolved (the same known gap write_file already has for sub-agents
	// — see gate.go), and ADK's MCP confirmation hook has no per-agent
	// identity to special-case around it the way gate.go does for its own
	// tools. buildSubAgents deliberately never reads this field, so a
	// hand-edited sub-agent agent.json listing one is silently inert
	// rather than an error.
	MCPServers []string `json:"mcpServers,omitempty"`

	instruction string // from instruction.md, not agent.json — see loadAgentFileConfig
}

// subAgentsDir returns (creating it if missing) the directory
// config-discovered sub-agents live under. Nothing seeds it: a fresh
// install starts with zero specialists until a user adds one — this
// directory existing is just so there's somewhere obvious to put one.
func subAgentsDir() (string, error) {
	dir, err := appdir.Path("subagents")
	if err != nil {
		return "", fmt.Errorf("resolve subagents dir: %w", err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create subagents dir: %w", err)
	}
	return dir, nil
}

// loadAgentFileConfig reads one agent.json/instruction.md pair from dir
// — shared by root's loader and loadSubAgentConfigs below. A missing or
// invalid agent.json, or an empty instruction.md, is a hard error; the
// caller decides what "missing" should mean for its case (root
// self-heals by seeding defaults — see rootagent.go; a sub-agent
// directory missing either file is instead surfaced as broken, not
// silently skipped, by loadSubAgentConfigs).
func loadAgentFileConfig(dir string) (agentFileConfig, error) {
	configPath := filepath.Join(dir, agentConfigFile)
	data, err := os.ReadFile(configPath)
	if err != nil {
		return agentFileConfig{}, err
	}
	var cfg agentFileConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return agentFileConfig{}, fmt.Errorf("parse %s: %w", configPath, err)
	}
	if cfg.Name == "" {
		return agentFileConfig{}, fmt.Errorf("%s: missing required \"name\"", configPath)
	}

	instrPath := filepath.Join(dir, agentInstructionFile)
	instrData, err := os.ReadFile(instrPath)
	if err != nil {
		return agentFileConfig{}, err
	}
	cfg.instruction = strings.TrimSpace(string(instrData))
	if cfg.instruction == "" {
		return agentFileConfig{}, fmt.Errorf("%s: instruction is empty", instrPath)
	}

	return cfg, nil
}

// loadSubAgentConfigs discovers every sub-agent under subAgentsDir(): one
// subdirectory per agent, each required to hold both agent.json and
// instruction.md. A subdirectory missing either file (or with invalid
// JSON, or an empty instruction) is a hard error rather than being
// silently skipped — a half-authored agent should be visible as broken
// so a user notices, not quietly disappear from the root's specialist
// list. Directories are read in os.ReadDir's order (lexical by name),
// which is what controls display order in the root's generated
// instruction, since nothing else orders them.
func loadSubAgentConfigs() ([]agentFileConfig, error) {
	dir, err := subAgentsDir()
	if err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read subagents dir: %w", err)
	}

	var configs []agentFileConfig
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		agentDir := filepath.Join(dir, e.Name())
		cfg, err := loadAgentFileConfig(agentDir)
		if err != nil {
			return nil, fmt.Errorf("sub-agent %q: %w", e.Name(), err)
		}
		configs = append(configs, cfg)
	}
	return configs, nil
}

// buildSubAgents turns loaded configs into real ADK agents, resolving
// each config's Tools names against toolRegistry — every tool a
// sub-agent can be given must be registered there by the caller first
// (see agents.go's buildRootAgent). A config with no Provider/Model of
// its own reuses rootModel verbatim (no need to build an identical
// model twice); one that specifies either resolves its own via
// buildModel — callerProvider/callerAPIKey (whatever key the root agent
// was built with) is only actually used as an override if the config's
// own provider matches callerProvider too (see keyOverride); anything
// else (e.g. a sub-agent configured for a different provider than root)
// resolves its own key from data/credentials.json instead.
func buildSubAgents(ctx context.Context, callerProvider, callerAPIKey string, rootModel model.LLM, toolRegistry map[string]tool.Tool, configs []agentFileConfig) ([]agent.Agent, error) {
	agents := make([]agent.Agent, 0, len(configs))
	for _, cfg := range configs {
		tools := make([]tool.Tool, 0, len(cfg.Tools))
		for _, name := range cfg.Tools {
			t, ok := toolRegistry[name]
			if !ok {
				return nil, fmt.Errorf("sub-agent %q: unknown tool %q", cfg.Name, name)
			}
			tools = append(tools, t)
		}

		m := rootModel
		if cfg.Provider != "" || cfg.Model != "" {
			var err error
			m, err = buildModel(ctx, cfg.Provider, cfg.Model, keyOverride(cfg.Provider, callerProvider, callerAPIKey))
			if err != nil {
				return nil, fmt.Errorf("sub-agent %q: %w", cfg.Name, err)
			}
		}

		a, err := llmagent.New(llmagent.Config{
			Name:        cfg.Name,
			Model:       m,
			Description: cfg.Description,
			Instruction: cfg.instruction,
			Tools:       tools,
		})
		if err != nil {
			return nil, fmt.Errorf("create sub-agent %q: %w", cfg.Name, err)
		}
		agents = append(agents, a)
	}
	return agents, nil
}
