# Giving yourself a new tool via MCP

This app can load extra tools from MCP (Model Context Protocol) servers —
small standalone programs that speak a standard tool-calling protocol
over stdin/stdout. This is how you can give yourself a capability that
isn't one of your built-in tools.

Nothing is pre-configured: unless a server has already been set up,
`~/.tui-testing/mcpservers/` starts empty and you have no MCP tools yet.
The steps below are how to add one.

## How this app expects it configured

- **Config**: one TOML file per server at `~/.tui-testing/mcpservers/<name>.toml`,
  where `<name>` (the filename, minus `.toml`) is the server's identity.
  Required key: `command` (the executable to run). Optional: `args` (a
  list of strings) and `env` (a table of extra environment variables,
  merged onto the process's own environment — useful for an API token a
  server needs even for local stdio use).

  Example `~/.tui-testing/mcpservers/mytool.toml`:

  ```toml
  command = "C:/Users/<you>/.tui-testing/mcpservers-bin/mytool.exe"
  ```

- **Binaries**: nothing requires a specific location, but this app's
  convention is `~/.tui-testing/mcpservers-bin/` — keeps compiled tool
  binaries out of the config directory itself. Create it if it doesn't
  exist yet.

- **Wiring it to yourself**: your own `agent.json` (the root agent's,
  directly under `~/.tui-testing/`) needs an `"mcpServers"` array listing
  which configured server names to load, e.g.:

  ```json
  "mcpServers": ["mytool"]
  ```

  **This only works on the root agent** — the `agent.json` sitting
  directly under `~/.tui-testing/`. It is silently ignored if set on a
  sub-agent's own `agent.json` (under `~/.tui-testing/subagents/<name>/`).

- **Reload required**: after adding or changing `mcpServers`, or adding a
  new `mcpservers/*.toml` file, tell the user the change needs `/reload`
  (or an app restart) to take effect. It will not appear mid-turn, and
  calling a tool that hasn't loaded yet will fail.

- **Confirmation is always required**: every call to an MCP-provided tool
  asks the human to approve it first (outside full-auto mode) — this app
  has no way to know a third-party server's tool is safe, so this applies
  uniformly. That is expected behavior, not something broken.

## Recommended implementation

Write it in Go using the SDK this app already depends on
(`github.com/modelcontextprotocol/go-sdk`) — consistent with the rest of
this app's toolchain, and needs nothing installed beyond the Go
toolchain already used to build it. Any language that can speak MCP over
stdio works if Go genuinely doesn't fit the task, but Go is the default.

A minimal, complete example — its own standalone module, **not** part of
this app's own module/repo — implementing one tool:

`main.go`:

```go
package main

import (
	"context"
	"log"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type reverseTextArgs struct {
	Text string `json:"text" jsonschema:"Text to reverse."`
}

func reverseText(_ context.Context, _ *mcp.CallToolRequest, args reverseTextArgs) (*mcp.CallToolResult, any, error) {
	runes := []rune(args.Text)
	for i, j := 0, len(runes)-1; i < j; i, j = i+1, j-1 {
		runes[i], runes[j] = runes[j], runes[i]
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(runes)}}}, nil, nil
}

func main() {
	server := mcp.NewServer(&mcp.Implementation{Name: "mytool", Version: "v0.1.0"}, nil)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "reverse_text",
		Description: "Reverses the characters of the given text and returns the result.",
	}, reverseText)
	if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		log.Fatalf("mytool mcp server: %v", err)
	}
}
```

Build steps, run from the new module's own directory (anywhere outside
this app's own repo):

```
go mod init mytool
go get github.com/modelcontextprotocol/go-sdk@v1.4.1
go build -o ~/.tui-testing/mcpservers-bin/mytool.exe .
```

(If this app's own `go.mod` now lists a newer `go-sdk` version than
`v1.4.1`, match that version instead.)

Then write the TOML and edit `agent.json` as described above.

`reverse_text`/`mytool` above are placeholder names — this example is
illustrative, not something already installed. Replace them with
whatever you're actually building.

## Before building one

Tell the user what the new tool will do and why, before creating and
wiring it in — it runs as a real subprocess with full access to whatever
the execution target can reach, the same trust level as any of your
other tools. Per-call confirmation doesn't substitute for the human
knowing what capability is being added in the first place.
