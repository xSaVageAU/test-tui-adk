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
func newBackend(ctx context.Context, apiKey string) (ui.Backend, error) {
	client, err := adk.New(ctx, apiKey)
	if err != nil {
		return nil, err
	}
	// Best-effort: failing to persist shouldn't block the key from being
	// used for this run, and there's nowhere safe to report it from here
	// (writing to stderr while the TUI owns the alt-screen would corrupt
	// the rendered frame — the same reason GORM's default logger had to
	// be silenced elsewhere in this codebase). Worst case, /key just
	// needs to be run again next launch.
	_ = adk.SaveAPIKey(adk.ProviderGemini, apiKey)
	return client, nil
}

func main() {
	ctx := context.Background()

	var backend ui.Backend
	var note string
	var specialists []string
	var modelName string

	// Read independently of whether a connection ever succeeds — the
	// header/boot banner should show the configured name even before
	// (or without) a working API key, same as list_files et al. being
	// discoverable without one.
	agentName, err := adk.RootAgentName()
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

	client, err := adk.New(ctx, apiKey)
	switch {
	case err == nil:
		backend = client
		specialists = client.Specialists()
		modelName = client.ModelName()
	case apiKey != "":
		// A key was present but didn't work — worth surfacing, unlike the
		// no-key case below, which the /key popup now explains itself the
		// moment it's actually needed (see App.sendMessage).
		note = fmt.Sprintf("Could not connect with %s: %v. Use /key to try again.", keySource, err)
	}

	app := ui.NewApp(ui.AppConfig{
		Backend:     backend,
		BackendNote: note,
		NewBackend:  newBackend,
		ModelName:   modelName,
		AgentName:   agentName,
		Specialists: specialists,
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
