package tools

import (
	"fmt"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/functiontool"
)

// write_file is destructive (creates or overwrites its target), so it
// requires confirmation in "normal" mode (destructive:true) — decided
// per-call by confirmGated rather than statically via
// functiontool.Config.RequireConfirmation, see gate.go for why that
// couldn't stay a static flag once permission-mode/sub-agent-auto-accept
// logic needed per-call context. Its resource is a write, conflicting
// with any read/write/listing that overlaps its path.
func init() {
	register(spec{
		destructive: true,
		resources:   writeFileResources,
		build: func() (tool.Tool, error) {
			return functiontool.New(functiontool.Config{
				Name:        "write_file",
				Description: "Writes content to a file at the given path, creating it or overwriting it if it already exists.",
			}, writeFile)
		},
	})
}

type writeFileArgs struct {
	Path    string `json:"path" jsonschema:"Path of the file to write, relative or absolute."`
	Content string `json:"content" jsonschema:"The full content to write to the file."`
}

type writeFileResult struct {
	BytesWritten int `json:"bytesWritten" jsonschema:"Number of bytes written."`
}

func writeFile(_ agent.Context, args writeFileArgs) (writeFileResult, error) {
	if err := target().WriteFile(args.Path, []byte(args.Content), 0o644); err != nil {
		return writeFileResult{}, fmt.Errorf("write file %q: %w", args.Path, err)
	}
	return writeFileResult{BytesWritten: len(args.Content)}, nil
}

func writeFileResources(args map[string]any) []resourceRef {
	path, _ := args["path"].(string)
	return []resourceRef{fileRef(path, true)}
}
