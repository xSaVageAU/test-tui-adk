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
// place that bridges the two.
func newBackend(ctx context.Context, apiKey string) (ui.Backend, error) {
	return adk.New(ctx, apiKey)
}

func main() {
	ctx := context.Background()

	var backend ui.Backend
	var note string

	apiKey := os.Getenv("GOOGLE_API_KEY")
	client, err := adk.New(ctx, apiKey)
	switch {
	case err == nil:
		backend = client
	case apiKey != "":
		// A key was present but didn't work — worth surfacing, unlike the
		// no-key case below, which the /key popup now explains itself the
		// moment it's actually needed (see App.sendMessage).
		note = "Could not connect with GOOGLE_API_KEY from the environment: " + err.Error() + ". Use /key to try again."
	}

	app := ui.NewApp(ui.AppConfig{
		Backend:     backend,
		BackendNote: note,
		NewBackend:  newBackend,
		ModelName:   adk.ModelName,
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
