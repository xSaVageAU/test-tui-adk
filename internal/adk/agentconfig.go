package adk

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"tui-testing/internal/appdir"
)

// AgentSummary is one agent's identity plus model selection, as read
// from its agent.json — the read side of what internal/ui's /agents
// menu lets a user browse and edit (via ListAgentConfigs, wired through
// AppConfig the same way NewBackend already bridges /key across the
// ui/adk package boundary — ui never imports this package directly).
// ID is "" for the root agent, or the sub-agent's subagents/<ID>
// directory name — the same value SetAgentProvider/SetAgentModel expect
// back to know which agent.json to write.
type AgentSummary struct {
	ID       string
	Name     string
	Provider string
	Model    string
	IsRoot   bool
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
	out := []AgentSummary{{Name: root.Name, Provider: root.Provider, Model: root.Model, IsRoot: true}}

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
		out = append(out, AgentSummary{ID: e.Name(), Name: cfg.Name, Provider: cfg.Provider, Model: cfg.Model})
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
