package adk

import (
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
	subAgentConfigFile      = "agent.json"
	subAgentInstructionFile = "instruction.md"
)

// subAgentConfig is one specialist's definition as discovered on disk:
// one subdirectory per agent under appdir's "subagents" directory,
// named for it, holding an agent.json (Name/Description/Tools) and an
// instruction.md (the agent's instruction, as plain text/markdown).
// Instruction is kept in its own file rather than a JSON string field so
// it can be written and diffed as prose, not escaped inside quotes.
type subAgentConfig struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Tools       []string `json:"tools,omitempty"`

	instruction string // from instruction.md, not agent.json — see loadSubAgentConfigs
}

// subAgentsDir returns (creating it if missing) the directory
// config-discovered sub-agents live under. Nothing seeds it: a fresh
// install starts with only the hardcoded root agent (see agents.go) and
// zero specialists until a user adds one — this directory existing is
// just so there's somewhere obvious to put one.
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

// loadSubAgentConfigs discovers every sub-agent under subAgentsDir(): one
// subdirectory per agent, each required to hold both agent.json and
// instruction.md. A subdirectory missing either file (or with invalid
// JSON, or an empty instruction) is a hard error rather than being
// silently skipped — a half-authored agent should be visible as broken
// so a user notices, not quietly disappear from the root's specialist
// list. Directories are read in os.ReadDir's order (lexical by name),
// which is what controls display order in the root's generated
// instruction, since nothing else orders them.
func loadSubAgentConfigs() ([]subAgentConfig, error) {
	dir, err := subAgentsDir()
	if err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read subagents dir: %w", err)
	}

	var configs []subAgentConfig
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		agentDir := filepath.Join(dir, e.Name())

		configPath := filepath.Join(agentDir, subAgentConfigFile)
		data, err := os.ReadFile(configPath)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", configPath, err)
		}
		var cfg subAgentConfig
		if err := json.Unmarshal(data, &cfg); err != nil {
			return nil, fmt.Errorf("parse %s: %w", configPath, err)
		}
		if cfg.Name == "" {
			return nil, fmt.Errorf("%s: missing required \"name\"", configPath)
		}

		instrPath := filepath.Join(agentDir, subAgentInstructionFile)
		instrData, err := os.ReadFile(instrPath)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", instrPath, err)
		}
		cfg.instruction = strings.TrimSpace(string(instrData))
		if cfg.instruction == "" {
			return nil, fmt.Errorf("%s: instruction is empty", instrPath)
		}

		configs = append(configs, cfg)
	}
	return configs, nil
}

// buildSubAgents turns loaded configs into real ADK agents, resolving
// each config's Tools names against toolRegistry — every tool a
// sub-agent can be given must be registered there by the caller first
// (see agents.go's buildRootAgent).
func buildSubAgents(m model.LLM, toolRegistry map[string]tool.Tool, configs []subAgentConfig) ([]agent.Agent, error) {
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
