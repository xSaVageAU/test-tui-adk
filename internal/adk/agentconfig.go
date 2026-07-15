package adk

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"tui-testing/internal/appdir"

	"tui-testing/internal/adk/tools"
)

// AgentSummary is one agent's identity plus model selection, as read
// from its agent.json — the read side of what internal/ui's /agents
// menu lets a user browse and edit (via ListAgentConfigs, wired through
// AppConfig the same way NewBackend already bridges /key across the
// ui/adk package boundary — ui never imports this package directly).
// ID is "" for the root agent, or the sub-agent's subagents/<ID>
// directory name — the same value SetAgentProvider/SetAgentModel expect
// back to know which agent.json to write. Tools is exactly what's
// stored on disk (see agentFileConfig.Tools) — nil/empty means the
// agent currently has none granted, not "use some default set."
type AgentSummary struct {
	ID       string
	Name     string
	Provider string
	Model    string
	Tools    []string
	IsRoot   bool
}

// ToolSummary is one tool this app can grant to an agent — the full
// universe /agents' tools picker offers checkboxes for, independent of
// which agents currently have which enabled. Description is the same
// text the model itself sees (tool.Tool.Description()), shown in the
// picker so a user can tell what a tool actually does without having to
// already know the codebase.
type ToolSummary struct {
	Name        string
	Description string
}

// ListToolSummaries returns every tool tools.Registry can build, sorted
// by name. rootName doesn't matter for this — per tools.Registry's own
// doc comment it only affects a tool's runtime confirmation behavior
// (see tools/gate.go), not its name, description, or construction — so
// "" is passed; this is enumeration, nothing here is ever actually
// invoked.
func ListToolSummaries() ([]ToolSummary, error) {
	reg, err := tools.Registry("")
	if err != nil {
		return nil, fmt.Errorf("build tool registry: %w", err)
	}
	out := make([]ToolSummary, 0, len(reg))
	for _, t := range reg {
		out = append(out, ToolSummary{Name: t.Name(), Description: t.Description()})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// ListAgentConfigs reads the root agent plus every discovered sub-agent
// — root first, then sub-agents in directory order (the same order
// buildRootAgent loads them in). Provider/Model are returned exactly as
// stored on disk, including "" (meaning "use the default" — see
// buildModel); resolving that to a display string is the caller's job.
func ListAgentConfigs() ([]AgentSummary, error) {
	root, err := loadRootAgentConfig()
	if err != nil {
		return nil, fmt.Errorf("load root agent config: %w", err)
	}
	out := []AgentSummary{{Name: root.Name, Provider: root.Provider, Model: root.Model, Tools: root.Tools, IsRoot: true}}

	dir, err := subAgentsDir()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read subagents dir: %w", err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		cfg, err := loadAgentFileConfig(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, fmt.Errorf("sub-agent %q: %w", e.Name(), err)
		}
		out = append(out, AgentSummary{ID: e.Name(), Name: cfg.Name, Provider: cfg.Provider, Model: cfg.Model, Tools: cfg.Tools})
	}
	return out, nil
}

// SetAgentProvider and SetAgentModel each edit one field of an agent's
// agent.json in place, leaving name/description/tools untouched. id is
// "" for the root agent, otherwise a sub-agent's directory name (an
// AgentSummary.ID from ListAgentConfigs). Neither rebuilds the running
// backend — that's the caller's call to make (see internal/ui's
// reloadBackend), since a config write should succeed independent of
// whether reconnecting afterward also does.
func SetAgentProvider(id, provider string) error {
	return updateAgentConfig(id, func(cfg *agentFileConfig) { cfg.Provider = provider })
}

func SetAgentModel(id, modelName string) error {
	return updateAgentConfig(id, func(cfg *agentFileConfig) { cfg.Model = modelName })
}

// SetAgentTools replaces an agent's whole tools list — the /agents tools
// picker writes the full current selection on every checkbox toggle
// rather than one name at a time, so this mirrors that shape instead of
// an add/remove pair.
func SetAgentTools(id string, toolNames []string) error {
	return updateAgentConfig(id, func(cfg *agentFileConfig) { cfg.Tools = toolNames })
}

func configDirFor(id string) (string, error) {
	if id == "" {
		return appdir.Dir()
	}
	dir, err := subAgentsDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, id), nil
}

func updateAgentConfig(id string, mutate func(*agentFileConfig)) error {
	dir, err := configDirFor(id)
	if err != nil {
		return err
	}
	cfg, err := loadAgentFileConfig(dir)
	if err != nil {
		return err
	}
	mutate(&cfg)

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal agent.json: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, agentConfigFile), data, 0o644); err != nil {
		return fmt.Errorf("write agent.json: %w", err)
	}
	return nil
}
