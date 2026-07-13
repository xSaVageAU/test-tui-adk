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
// plus its handler per tool, same shape as newListFilesTool below. Any
// tool whose args include a file/directory path should be wrapped with
// gated (see toolgate.go) so concurrent calls that actually touch
// overlapping paths serialize instead of racing.

// newListFilesTool builds the list_files tool, shared by the root agent
// and every specialist that wants it (functiontool.New just builds a
// schema/handler pair — it's not agent-specific state, so the same
// tool.Tool value can be handed to more than one agent's Tools list).
// rootName is threaded through to confirmGated — see toolgate.go — so it
// can tell a root call apart from a sub-agent one; it never actually
// requires confirmation in "normal" mode (it's read-only, nothing to
// approve), but is still wrapped for consistency and in case a future
// mode wants it gated.
func newListFilesTool(rootName string) (tool.Tool, error) {
	t, err := functiontool.New(functiontool.Config{
		Name:        "list_files",
		Description: "Lists files and directories at the given path. Defaults to the current working directory if path is omitted.",
	}, listFiles)
	if err != nil {
		return nil, err
	}
	return gated(confirmGated(t, false, rootName), listFilesResources), nil
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
// directory (see this file's doc comment: a write_file completing while
// a list_files call from the same batch is still "in flight" is exactly
// the scenario that used to confuse the model with a stale listing).
func listFilesResources(args map[string]any) []resourceRef {
	path, _ := args["path"].(string)
	return []resourceRef{dirRef(path)}
}

// newReadFileTool builds the read_file tool — read-only, so it needs no
// confirmation in any mode (nothing to approve) and never conflicts
// with another read, only with a write on the same path.
func newReadFileTool(rootName string) (tool.Tool, error) {
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

// newWriteFileTool builds the write_file tool — destructive (creates or
// overwrites its target), so it requires confirmation in "normal" mode
// (not statically via functiontool.Config.RequireConfirmation anymore —
// see toolgate.go's confirmGated for why that couldn't stay a static
// flag once the permission-mode/sub-agent-auto-accept logic needed
// per-call context). Its resource is a write, conflicting with any
// read/write/listing that overlaps its path.
func newWriteFileTool(rootName string) (tool.Tool, error) {
	t, err := functiontool.New(functiontool.Config{
		Name:        "write_file",
		Description: "Writes content to a file at the given path, creating it or overwriting it if it already exists.",
	}, writeFile)
	if err != nil {
		return nil, err
	}
	return gated(confirmGated(t, true, rootName), writeFileResources), nil
}

type writeFileArgs struct {
	Path    string `json:"path" jsonschema:"Path of the file to write, relative or absolute."`
	Content string `json:"content" jsonschema:"The full content to write to the file."`
}

type writeFileResult struct {
	BytesWritten int `json:"bytesWritten" jsonschema:"Number of bytes written."`
}

func writeFile(_ agent.Context, args writeFileArgs) (writeFileResult, error) {
	if err := os.WriteFile(args.Path, []byte(args.Content), 0o644); err != nil {
		return writeFileResult{}, fmt.Errorf("write file %q: %w", args.Path, err)
	}
	return writeFileResult{BytesWritten: len(args.Content)}, nil
}

func writeFileResources(args map[string]any) []resourceRef {
	path, _ := args["path"].(string)
	return []resourceRef{fileRef(path, true)}
}
