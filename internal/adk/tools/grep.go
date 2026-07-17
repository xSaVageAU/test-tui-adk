package tools

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/functiontool"
)

// grep searches file *contents* for a regular expression, walking a
// directory tree in pure Go — no shelling out to ripgrep, so the app
// stays a single self-contained binary (see [[agent-platform-vision]]).
// Read-only; its resource is a directory-scoped read, so a write under
// the searched tree mid-batch can't hand back stale matches (gate.go).
func init() {
	register(spec{
		destructive: false,
		resources:   grepResources,
		build: func() (tool.Tool, error) {
			return functiontool.New(functiontool.Config{
				Name:        "grep",
				Description: "Searches file contents for a regular expression (RE2 syntax) and returns matching lines as \"path:line: text\". Searches the given path recursively if it's a directory, or a single file; an optional glob limits which filenames are searched.",
			}, grep)
		},
	})
}

type grepArgs struct {
	Pattern    string `json:"pattern" jsonschema:"Regular expression to search for (RE2 syntax, as used by Go's regexp package)."`
	Path       string `json:"path,omitempty" jsonschema:"File or directory to search. Defaults to the current working directory."`
	Glob       string `json:"glob,omitempty" jsonschema:"Optional filename glob (e.g. '*.go') limiting which files are searched."`
	IgnoreCase bool   `json:"ignore_case,omitempty" jsonschema:"Match case-insensitively."`
}

type grepResult struct {
	Matches []string `json:"matches" jsonschema:"Matching lines, each formatted as \"path:line: text\"."`
}

// grepMaxMatches caps how many matching lines grep returns, so a broad
// pattern over a big tree can't flood the model's context.
// grepMaxFileSize skips files bigger than this (likely data/binaries).
const (
	grepMaxMatches  = 500
	grepMaxFileSize = 5 << 20 // 5 MiB
	grepMaxLine     = 1 << 20 // tolerate lines up to 1 MiB before giving up
)

func grep(_ agent.Context, args grepArgs) (grepResult, error) {
	expr := args.Pattern
	if args.IgnoreCase {
		expr = "(?i)" + expr
	}
	re, err := regexp.Compile(expr)
	if err != nil {
		return grepResult{}, fmt.Errorf("grep: invalid pattern: %w", err)
	}
	root := args.Path
	if root == "" {
		root = "."
	}

	// errStop stops the walk once we've collected enough matches —
	// WalkDir has no other way to break out early.
	errStop := errors.New("match cap reached")
	var matches []string
	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable entries rather than aborting the whole search
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return fs.SkipDir
			}
			return nil
		}
		if args.Glob != "" {
			if ok, _ := filepath.Match(args.Glob, d.Name()); !ok {
				return nil
			}
		}
		if info, statErr := d.Info(); statErr == nil && info.Size() > grepMaxFileSize {
			return nil
		}
		lines, ferr := grepFile(path, re, grepMaxMatches-len(matches))
		if ferr != nil {
			return nil // unreadable file: skip, don't fail the whole search
		}
		matches = append(matches, lines...)
		if len(matches) >= grepMaxMatches {
			return errStop
		}
		return nil
	})
	if walkErr != nil && !errors.Is(walkErr, errStop) {
		return grepResult{}, fmt.Errorf("grep: %w", walkErr)
	}
	return grepResult{Matches: matches}, nil
}

// grepFile returns up to max "path:line: text" matches for re in the file
// at path. It reads line by line and bails on a file that looks binary (a
// NUL byte on the first line), keeping grep a source-code search.
func grepFile(path string, re *regexp.Regexp, max int) ([]string, error) {
	if max <= 0 {
		return nil, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var out []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), grepMaxLine)
	line := 0
	for sc.Scan() {
		line++
		b := sc.Bytes()
		if line == 1 && bytes.IndexByte(b, 0) >= 0 {
			return nil, nil // binary file
		}
		if re.Match(b) {
			out = append(out, fmt.Sprintf("%s:%d: %s", path, line, strings.TrimRight(string(b), "\r")))
			if len(out) >= max {
				return out, nil
			}
		}
	}
	// A scan error (e.g. a line longer than grepMaxLine) stops us mid-file;
	// keep whatever matched before it rather than discarding the file's
	// results — partial matches are still useful, and this isn't a search-
	// wide failure.
	if err := sc.Err(); err != nil {
		return out, nil
	}
	return out, nil
}

func grepResources(args map[string]any) []resourceRef {
	path, _ := args["path"].(string)
	return []resourceRef{dirRef(path)}
}
