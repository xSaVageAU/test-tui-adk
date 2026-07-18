package tools

import (
	"fmt"
	"strings"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/functiontool"
)

// read_file is read-only, so it needs no confirmation in any mode
// (nothing to approve, hence destructive:false); it never conflicts with
// another read, only with a write on the same path (a single
// non-recursive read resource — see readFileResources).
func init() {
	register(spec{
		destructive: false,
		resources:   readFileResources,
		build: func() (tool.Tool, error) {
			return functiontool.New(functiontool.Config{
				Name:        "read_file",
				Description: "Reads a file and returns its contents with 1-based line numbers (like 'cat -n'). Optional offset/limit read just a window of a large file.",
			}, readFile)
		},
	})
}

type readFileArgs struct {
	Path   string `json:"path" jsonschema:"Path of the file to read, relative or absolute."`
	Offset int    `json:"offset,omitempty" jsonschema:"1-based line number to start reading from. Omit or 0 to start at the first line."`
	Limit  int    `json:"limit,omitempty" jsonschema:"Maximum number of lines to return, counting from offset. Omit or 0 for the rest of the file."`
}

type readFileResult struct {
	Content string `json:"content" jsonschema:"The file's contents, each line prefixed with its 1-based line number and a tab (like 'cat -n'). The numbers are a display aid only — they are not part of the file, so do not include them when matching text for edit_file."`
}

func readFile(_ agent.Context, args readFileArgs) (readFileResult, error) {
	data, err := target().ReadFile(args.Path)
	if err != nil {
		return readFileResult{}, fmt.Errorf("read file %q: %w", args.Path, err)
	}
	return readFileResult{Content: numberLines(string(data), args.Offset, args.Limit)}, nil
}

// numberLines renders s with each line prefixed by its 1-based line
// number and a tab (like "cat -n"), optionally windowed to at most limit
// lines starting at offset (1-based). offset<=1 starts at the first line;
// limit<=0 returns every line from offset onward. The numbers are a
// reading aid for the model — line-targeted, not byte-targeted — and are
// deliberately not part of what edit_file matches against.
func numberLines(s string, offset, limit int) string {
	if s == "" {
		return ""
	}
	lines := strings.Split(s, "\n")
	// A trailing newline yields a final empty element; drop it so we
	// don't number a phantom blank line past the end of the file.
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	start := 0
	if offset > 1 {
		start = offset - 1
	}
	if start >= len(lines) {
		return ""
	}
	end := len(lines)
	if limit > 0 && start+limit < end {
		end = start + limit
	}
	var b strings.Builder
	for i := start; i < end; i++ {
		fmt.Fprintf(&b, "%6d\t%s\n", i+1, lines[i])
	}
	return b.String()
}

func readFileResources(args map[string]any) []resourceRef {
	path, _ := args["path"].(string)
	return []resourceRef{fileRef(path, false)}
}
