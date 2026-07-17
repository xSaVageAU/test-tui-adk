package tools

import (
	"fmt"
	"os"
	"sort"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/functiontool"
)

// list_files is read-only (destructive:false — nothing to approve), but
// still declares a directory-scoped read resource so a write completing
// under the listed directory mid-batch can't hand back a stale listing
// (see listFilesResources and gate.go's package doc). The built tool is
// shared by the root agent and every specialist that wants it — the same
// tool.Tool value can be handed to more than one agent's Tools list,
// since functiontool.New just builds a schema/handler pair, not agent-
// specific state.
func init() {
	register(spec{
		destructive: false,
		resources:   listFilesResources,
		build: func() (tool.Tool, error) {
			return functiontool.New(functiontool.Config{
				Name:        "list_files",
				Description: "Lists files and directories at the given path. Defaults to the current working directory if path is omitted.",
			}, listFiles)
		},
	})
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

// listFilesResources declares list_files as a directory-scoped read —
// Recursive so it conflicts with a write anywhere under the listed
// directory (see gate.go's package doc comment: a write_file completing
// while a list_files call from the same batch is still "in flight" is
// exactly the scenario that used to confuse the model with a stale
// listing).
func listFilesResources(args map[string]any) []resourceRef {
	path, _ := args["path"].(string)
	return []resourceRef{dirRef(path)}
}
