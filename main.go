package main

import (
	"context"
	"fmt"
	"os"

	"tui-testing/internal/adk"
	"tui-testing/internal/ui"

	tea "charm.land/bubbletea/v2"
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

// listAgents/setAgentProvider/setAgentModel/setAgentTools/listTools
// adapt the adk package's config read/write functions to the plain
// function shapes ui.AppConfig expects — same bridging reason as
// newBackend/BackendFactory: the ui package never imports adk directly,
// only these caller-supplied closures (and the ui.AgentConfigSummary/
// ui.ToolSummary shapes, converted to/from their adk counterparts here).
func listAgents() ([]ui.AgentConfigSummary, error) {
	agents, err := adk.ListAgentConfigs()
	if err != nil {
		return nil, err
	}
	out := make([]ui.AgentConfigSummary, len(agents))
	for i, a := range agents {
		out[i] = ui.AgentConfigSummary{ID: a.ID, Name: a.Name, Provider: a.Provider, Model: a.Model, Tools: a.Tools, IsRoot: a.IsRoot}
	}
	return out, nil
}

func listTools() ([]ui.ToolSummary, error) {
	tools, err := adk.ListToolSummaries()
	if err != nil {
		return nil, err
	}
	out := make([]ui.ToolSummary, len(tools))
	for i, t := range tools {
		out[i] = ui.ToolSummary{Name: t.Name, Description: t.Description}
	}
	return out, nil
}

// joinNotes combines two boot-note fragments, keeping both when the first
// is already set rather than one overwriting the other (the single note
// string is shown as-is in the boot banner).
func joinNotes(existing, add string) string {
	if existing == "" {
		return add
	}
	return existing + "\n" + add
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

	// Install the execution target (local host, or a remote SSH machine if
	// settings select one) before the user can send anything that runs a
	// tool. An SSH failure leaves tools on the local host, so surface it —
	// the user should know their remote target didn't take effect rather
	// than silently operating on the wrong machine. Appended so it doesn't
	// clobber a connection/config note set above.
	if desc, terr := adk.ConfigureExecutionTarget(); terr != nil {
		note = joinNotes(note, "Execution target: "+terr.Error()+" — tools will run on the local host until fixed.")
	} else if desc != "" && desc != "host" {
		note = joinNotes(note, "Tools are running on "+desc)
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
		SetAgentTools:    adk.SetAgentTools,
		ListTools:        listTools,
		ConfigureTarget:  adk.ConfigureExecutionTarget,
	})

	// AltScreen and MouseMode are set on the tea.View returned from
	// App.View() now (v2 moved these from Program options to per-View
	// declarative fields) — the chat viewport has MouseWheelEnabled by
	// default, but nothing reached it without MouseMode set there.
	p := tea.NewProgram(app)
	_, runErr := p.Run()
	// Kill any processes the run_shell tool started in the background so
	// they don't outlive the TUI. Called explicitly (not via defer)
	// because the os.Exit below on the error path would skip deferred
	// cleanup.
	adk.ShutdownBackgroundProcesses()
	// Close the execution target too (an SSH/SFTP connection, if one was
	// installed) so it doesn't linger past the TUI.
	adk.CloseExecutionTarget()
	if runErr != nil {
		fmt.Fprintln(os.Stderr, "error:", runErr)
		os.Exit(1)
	}
}
