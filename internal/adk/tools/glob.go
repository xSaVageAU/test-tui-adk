package tools

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/functiontool"
)

// glob finds files by *name* pattern, supporting "**" (match across
// directory boundaries), which filepath.Match/Glob can't do on their own
// — so it walks the tree and matches each file's relative path against a
// small glob→regexp translation. Read-only; a directory-scoped read
// resource, same as grep (see gate.go).
func init() {
	register(spec{
		destructive: false,
		resources:   globResources,
		build: func() (tool.Tool, error) {
			return functiontool.New(functiontool.Config{
				Name:        "glob",
				Description: "Finds files whose path matches a glob pattern, searching a root directory recursively. Supports '*' (within one path segment), '?' (a single character), and '**' (across directory boundaries), e.g. '**/*.go'. Returns matching file paths, relative to the root.",
			}, globFiles)
		},
	})
}

type globArgs struct {
	Pattern string `json:"pattern" jsonschema:"Glob pattern matched against each file's path relative to the root, e.g. '**/*.go' or 'cmd/*/main.go'."`
	Path    string `json:"path,omitempty" jsonschema:"Root directory to search under. Defaults to the current working directory."`
}

type globResult struct {
	Files []string `json:"files" jsonschema:"Paths of files matching the pattern, relative to the search root, sorted."`
}

const globMaxResults = 1000

func globFiles(_ agent.Context, args globArgs) (globResult, error) {
	re, err := globToRegexp(args.Pattern)
	if err != nil {
		return globResult{}, fmt.Errorf("glob: %w", err)
	}
	root := args.Path
	if root == "" {
		root = "."
	}

	var files []string
	walkErr := target().Walk(root, func(path string, info fs.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			if info.Name() == ".git" {
				return fs.SkipDir
			}
			return nil
		}
		rel := relPath(root, path)
		if re.MatchString(rel) {
			files = append(files, rel)
			if len(files) >= globMaxResults {
				return fs.SkipAll
			}
		}
		return nil
	})
	if walkErr != nil {
		return globResult{}, fmt.Errorf("glob: %w", walkErr)
	}
	sort.Strings(files)
	return globResult{Files: files}, nil
}

// relPath returns path relative to root using slash separators. It's
// deliberately separator-agnostic (string prefix trimming, not
// filepath.Rel) so it works for both the local target's OS-separator walk
// output and an SSH target's slash paths — filepath.Rel would mangle a
// remote "/home/x" path on a Windows host. Falls back to the slash form
// of path when it isn't under root.
func relPath(root, path string) string {
	p := filepath.ToSlash(path)
	r := strings.TrimSuffix(filepath.ToSlash(root), "/")
	if rel, ok := strings.CutPrefix(p, r+"/"); ok {
		return rel
	}
	return p
}

// globToRegexp translates a glob (with '*', '?', and '**') into an
// anchored RE2 pattern matched against a file's slash-separated relative
// path. '**' matches any run of characters including separators; '*'
// matches within a single segment (no separator); '?' matches one non-
// separator character. Everything else is matched literally.
func globToRegexp(pattern string) (*regexp.Regexp, error) {
	var b strings.Builder
	b.WriteString("^")
	for i := 0; i < len(pattern); i++ {
		switch pattern[i] {
		case '*':
			if i+1 < len(pattern) && pattern[i+1] == '*' {
				b.WriteString(".*")
				i++
				// Swallow a '/' right after '**' so '**/x' also matches
				// 'x' at the root (zero directories deep).
				if i+1 < len(pattern) && pattern[i+1] == '/' {
					b.WriteString("/?")
					i++
				}
			} else {
				b.WriteString("[^/]*")
			}
		case '?':
			b.WriteString("[^/]")
		default:
			b.WriteString(regexp.QuoteMeta(string(pattern[i])))
		}
	}
	b.WriteString("$")
	return regexp.Compile(b.String())
}

func globResources(args map[string]any) []resourceRef {
	path, _ := args["path"].(string)
	return []resourceRef{dirRef(path)}
}
