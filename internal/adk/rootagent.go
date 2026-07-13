package adk

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"tui-testing/internal/appdir"
)

// defaultRootName/Description/Tools/Instruction seed a fresh install's
// root agent.json/instruction.md — the app's own long-standing built-in
// behavior, just expressed as the same config shape a sub-agent uses
// rather than Go literals, so it's immediately visible and editable
// rather than needing a code change.
const (
	defaultRootName        = "assistant"
	defaultRootDescription = "A general-purpose assistant for testing the TUI against a real LLM."

	defaultRootInstruction = "You are the front-line assistant embedded in a terminal chat UI test harness. " +
		"Keep replies short — this is a test harness for the UI, not a place for long essays. " +
		"You have list_files, read_file, and write_file tools for browsing and editing the working " +
		"directory; use them whenever relevant. You can consult specialists via tool calls when a request " +
		"clearly fits their focus. Incorporate what they tell you into your own reply — you're still the " +
		"one answering the user. Handle general requests yourself rather than consulting a specialist " +
		"unnecessarily."
)

var defaultRootTools = []string{"list_files", "read_file", "write_file"}

// loadRootAgentConfig reads the root agent's agent.json/instruction.md,
// seeding whichever one is missing with the built-in default — unlike a
// sub-agent (which can simply not exist), the root agent is mandatory,
// so a missing file self-heals to a default instead of being a hard
// error. Malformed content (bad JSON, a missing "name", an instruction
// file that exists but is empty) still fails loudly rather than being
// silently patched over — that's a real mistake worth surfacing, not a
// fresh-install condition.
func loadRootAgentConfig() (agentFileConfig, error) {
	dir, err := appdir.Dir()
	if err != nil {
		return agentFileConfig{}, fmt.Errorf("resolve app dir: %w", err)
	}

	if err := seedIfMissing(dir); err != nil {
		return agentFileConfig{}, err
	}

	cfg, err := loadAgentFileConfig(dir)
	if err != nil {
		return agentFileConfig{}, fmt.Errorf("root agent: %w", err)
	}
	return cfg, nil
}

func seedIfMissing(dir string) error {
	configPath := filepath.Join(dir, agentConfigFile)
	instrPath := filepath.Join(dir, agentInstructionFile)

	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		cfg := agentFileConfig{
			Name:        defaultRootName,
			Description: defaultRootDescription,
			Tools:       defaultRootTools,
		}
		data, err := json.MarshalIndent(cfg, "", "  ")
		if err != nil {
			return fmt.Errorf("marshal default root agent.json: %w", err)
		}
		if err := os.WriteFile(configPath, data, 0o644); err != nil {
			return fmt.Errorf("write default root agent.json: %w", err)
		}
	}

	if _, err := os.Stat(instrPath); os.IsNotExist(err) {
		if err := os.WriteFile(instrPath, []byte(defaultRootInstruction+"\n"), 0o644); err != nil {
			return fmt.Errorf("write default root instruction.md: %w", err)
		}
	}

	return nil
}
