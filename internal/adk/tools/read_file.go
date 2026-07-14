package tools

import (
	"fmt"
	"os"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/functiontool"
)

// NewReadFileTool builds the read_file tool — read-only, so it needs no
// confirmation in any mode (nothing to approve) and never conflicts
// with another read, only with a write on the same path.
func NewReadFileTool(rootName string) (tool.Tool, error) {
	t, err := functiontool.New(functiontool.Config{
		Name:        "read_file",
		Description: "Reads and returns the full contents of a file at the given path.",
	}, readFile)
	if err != nil {
		return nil, err
	}
	return gated(confirmGated(t, false, rootName), readFileResources), nil
}

type readFileArgs struct {
	Path string `json:"path" jsonschema:"Path of the file to read, relative or absolute."`
}

type readFileResult struct {
	Content string `json:"content" jsonschema:"The file's full contents."`
}

func readFile(_ agent.Context, args readFileArgs) (readFileResult, error) {
	data, err := os.ReadFile(args.Path)
	if err != nil {
		return readFileResult{}, fmt.Errorf("read file %q: %w", args.Path, err)
	}
	return readFileResult{Content: string(data)}, nil
}

func readFileResources(args map[string]any) []resourceRef {
	path, _ := args["path"].(string)
	return []resourceRef{fileRef(path, false)}
}
