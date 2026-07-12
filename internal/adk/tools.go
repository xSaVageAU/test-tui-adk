package adk

import (
	"fmt"
	"os"
	"sort"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/functiontool"
)

// Every tool the agent tree can call lives in this file — as more get
// added (per the Hermes-parity "tool gateway" idea: web search, image
// generation, ...) this is where they go, one function-tool constructor
// plus its handler per tool, same shape as newListFilesTool below.

// newListFilesTool builds the list_files tool, shared by the root agent
// and every specialist that wants it (functiontool.New just builds a
// schema/handler pair — it's not agent-specific state, so the same
// tool.Tool value can be handed to more than one agent's Tools list).
func newListFilesTool() (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name:        "list_files",
		Description: "Lists files and directories at the given path. Defaults to the current working directory if path is omitted.",
		// RequireConfirmation hands the pause/resume orchestration to ADK
		// entirely: it emits a toolconfirmation.FunctionCallName event
		// instead of running listFiles, and either runs it or reports it
		// declined once we answer via Client.RespondToConfirmation.
		// listFiles itself needs no changes for this — see the HITL
		// handling in eventstream.go.
		RequireConfirmation: true,
	}, listFiles)
}

// Struct comments here are just for human readers — jsonschema-go (what
// functiontool.New uses to infer each tool's schema) only reads the
// "jsonschema" struct tag for the description the model actually sees,
// not regular Go comments.
type listFilesArgs struct {
	Path string `json:"path,omitempty" jsonschema:"Directory to list, relative or absolute. Defaults to the current working directory if omitted."`
}

type listFilesResult struct {
	Files []string `json:"files" jsonschema:"Entry names found in the directory; directories are suffixed with a trailing slash."`
}

func listFiles(_ agent.Context, args listFilesArgs) (listFilesResult, error) {
	dir := args.Path
	if dir == "" {
		dir = "."
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return listFilesResult{}, fmt.Errorf("read dir %q: %w", dir, err)
	}

	files := make([]string, 0, len(entries))
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() {
			name += "/"
		}
		files = append(files, name)
	}
	sort.Strings(files)

	return listFilesResult{Files: files}, nil
}
