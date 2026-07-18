package tools

import (
	"fmt"
	"io/fs"
	"strings"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/functiontool"
)

// edit_file makes a surgical, in-place change: it replaces an exact
// substring (old_string) with new_string, requiring old_string to be
// unique in the file unless replace_all is set — the same discipline as
// Claude Code's Edit tool. Destructive (it rewrites the file), so it
// confirms in "normal" mode; its resource is a write on the target path,
// serializing against any overlapping read/write/listing (see gate.go).
// It's the workhorse for changing an existing file without a full
// write_file rewrite: the model reads the file (with line numbers, via
// read_file), then names the exact text to swap.
func init() {
	register(spec{
		destructive: true,
		resources:   editFileResources,
		build: func() (tool.Tool, error) {
			return functiontool.New(functiontool.Config{
				Name:        "edit_file",
				Description: "Replaces an exact block of text in an existing file. old_string must match the file verbatim (including whitespace and indentation) and, unless replace_all is true, must be unique in the file — otherwise the edit is rejected. Do not include the line-number prefixes that read_file adds; match the raw file text.",
			}, editFile)
		},
	})
}

type editFileArgs struct {
	Path       string `json:"path" jsonschema:"Path of the file to edit, relative or absolute."`
	OldString  string `json:"old_string" jsonschema:"The exact text to replace. Must appear verbatim in the file, and must be unique unless replace_all is true."`
	NewString  string `json:"new_string" jsonschema:"The text to replace it with. Use an empty string to delete old_string."`
	ReplaceAll bool   `json:"replace_all,omitempty" jsonschema:"Replace every occurrence of old_string instead of requiring exactly one match."`
}

type editFileResult struct {
	Replacements int `json:"replacements" jsonschema:"Number of occurrences replaced."`
}

func editFile(_ agent.Context, args editFileArgs) (editFileResult, error) {
	if args.OldString == args.NewString {
		return editFileResult{}, fmt.Errorf("edit file %q: old_string and new_string are identical — nothing to change", args.Path)
	}
	data, err := target().ReadFile(args.Path)
	if err != nil {
		return editFileResult{}, fmt.Errorf("edit file %q: %w", args.Path, err)
	}
	content := string(data)

	count := strings.Count(content, args.OldString)
	switch {
	case count == 0:
		return editFileResult{}, fmt.Errorf("edit file %q: old_string not found in file", args.Path)
	case count > 1 && !args.ReplaceAll:
		return editFileResult{}, fmt.Errorf("edit file %q: old_string is not unique (%d matches) — add surrounding context to make it unique, or set replace_all", args.Path, count)
	}

	var updated string
	if args.ReplaceAll {
		updated = strings.ReplaceAll(content, args.OldString, args.NewString)
	} else {
		updated = strings.Replace(content, args.OldString, args.NewString, 1)
		count = 1
	}

	// Preserve the file's existing permission bits rather than forcing
	// 0o644 the way write_file's create path does — this edits a file
	// that already exists, so its mode is the user's, not ours to reset.
	mode := fs.FileMode(0o644)
	if info, statErr := target().Stat(args.Path); statErr == nil {
		mode = info.Mode().Perm()
	}
	if err := target().WriteFile(args.Path, []byte(updated), mode); err != nil {
		return editFileResult{}, fmt.Errorf("edit file %q: %w", args.Path, err)
	}
	return editFileResult{Replacements: count}, nil
}

func editFileResources(args map[string]any) []resourceRef {
	path, _ := args["path"].(string)
	return []resourceRef{fileRef(path, true)}
}
