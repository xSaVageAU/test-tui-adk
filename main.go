package main

import (
	"context"
	"fmt"
	"os"

	"tui-testing/internal/adk"
	"tui-testing/internal/ui"

	tea "github.com/charmbracelet/bubbletea"
)

// newBackend adapts adk.New to ui.BackendFactory — *adk.Client isn't
// directly assignable to a func returning ui.Backend, so this is the one
// place that bridges the two. This is also the only place a key gets
// persisted: it's what /key ultimately calls, and only a key that's just
// been proven to work (New succeeded with it) is saved — a typo'd key
// never ends up on disk. The startup path below reads a previously saved
// key back but deliberately never writes one itself, so an env-var-
// provided key is never silently persisted without the user having
// explicitly run /key.
//
// /agents also calls this, via App.reloadBackend, with both provider
// and apiKey left "" — that means "rebuild everything from whatever's
// already saved on disk for each agent's own configured provider, no
// fresh override key" (see adk.New's doc comment); the apiKey != ""
// guard below is what stops that no-key reload from overwriting a real
// saved key with an empty one.
func newBackend(ctx context.Context, provider, apiKey string) (ui.Backend, error) {
	client, err := adk.New(ctx, provider, apiKey)
	if err != nil {
		return nil, err
	}
	if apiKey != "" {
		// Best-effort: failing to persist shouldn't block the key from
		// being used for this run, and there's nowhere safe to report it
		// from here (writing to stderr while the TUI owns the alt-screen
		// would corrupt the rendered frame — the same reason GORM's
		// default logger had to be silenced elsewhere in this codebase).
		// Worst case, /key just needs to be run again next launch.
		_ = adk.SaveAPIKey(provider, apiKey)
	}
	return client, nil
}

// listAgents/setAgentProvider/setAgentModel adapt the adk package's
// config read/write functions to the plain function shapes ui.AppConfig
// expects — same bridging reason as newBackend/BackendFactory: the ui
// package never imports adk directly, only these caller-supplied
// closures (and the ui.AgentConfigSummary shape, converted to/from
// adk.AgentSummary here).
func listAgents() ([]ui.AgentConfigSummary, error) {
	agents, err := adk.ListAgentConfigs()
	if err != nil {
		return nil, err
	}
	out := make([]ui.AgentConfigSummary, len(agents))
	for i, a := range agents {
		out[i] = ui.AgentConfigSummary{ID: a.ID, Name: a.Name, Provider: a.Provider, Model: a.Model, IsRoot: a.IsRoot}
	}
	return out, nil
}

func main() {
	ctx := context.Background()

	var backend ui.Backend
	var note string
	var specialists []string
	var modelName string
	var contextWindow int

	// Read independently of whether a connection ever succeeds, purely to
	// fail fast on a broken config even before (or without) a working API
	// key — same as list_files et al. being discoverable without one. The
	// name itself isn't used for display anywhere anymore (see
	// ui/header.go's renderTopBar), just the error.
	_, err := adk.RootAgentName()
	if err != nil {
		note = "Could not load the root agent's config: " + err.Error()
	}

	// The environment variable always wins if set; otherwise fall back to
	// whatever /key last saved (see newBackend). Startup itself never
	// writes to the credentials file — only an explicit /key does.
	apiKey, keySource := os.Getenv("GOOGLE_API_KEY"), "the GOOGLE_API_KEY environment variable"
	if apiKey == "" {
		if saved, err := adk.LoadAPIKey(adk.ProviderGemini); err == nil && saved != "" {
			apiKey, keySource = saved, "your saved API key"
		}
	}

	client, err := adk.New(ctx, adk.ProviderGemini, apiKey)
	switch {
	case err == nil:
		backend = client
		specialists = client.Specialists()
		modelName = client.ModelName()
		contextWindow = client.ContextWindow()
	case apiKey != "":
		// A key was present but didn't work — worth surfacing, unlike the
		// no-key case below, which the /key popup now explains itself the
		// moment it's actually needed (see App.sendMessage).
		note = fmt.Sprintf("Could not connect with %s: %v. Use /key to try again.", keySource, err)
	}

	app := ui.NewApp(ui.AppConfig{
		Backend:          backend,
		BackendNote:      note,
		NewBackend:       newBackend,
		ModelName:        modelName,
		Specialists:      specialists,
		ContextWindow:    contextWindow,
		ListAgents:       listAgents,
		SetAgentProvider: adk.SetAgentProvider,
		SetAgentModel:    adk.SetAgentModel,
	})

	// WithMouseCellMotion is what actually makes the terminal report wheel
	// events at all — the chat viewport has MouseWheelEnabled by default,
	// but nothing reached it without this.
	p := tea.NewProgram(app, tea.WithAltScreen(), tea.WithMouseCellMotion())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
