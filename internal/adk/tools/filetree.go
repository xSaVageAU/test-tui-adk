// This file backs the webui's file-tree sidebar. It lists ONE directory
// per call (VS Code explorer-style lazy loading) rather than walking the
// whole tree: the earlier recursive walk needed a global entry budget to
// stay bounded, and any budget both truncated big trees mid-fold and
// spent SFTP round trips on directories nobody had opened. A single
// ReadDir per expanded folder is complete at any depth and O(visible),
// not O(tree). The client asks for "" (the root) first and then for each
// directory the user expands.
package tools

import (
	"sort"
	"strings"
)

// dirListMaxEntries bounds a single directory's listing — not a tree
// budget, just a sanity cap so one directory with tens of thousands of
// entries can't produce an unbounded response. Truncated marks the cut.
const dirListMaxEntries = 2000

// FileTreeEntry is one row of a directory listing. Name only — the
// client owns path assembly, since it's the one tracking the hierarchy.
type FileTreeEntry struct {
	Name string
	Dir  bool
}

// DirListing is ListDir's result. Root (the target's Getwd) rides along
// on every response so the client can detect a cwd/target change no
// matter which directory it happened to be refreshing.
type DirListing struct {
	Root      string
	Path      string // the relative directory listed; "" = the root itself
	Entries   []FileTreeEntry
	Truncated bool
}

// ListDir lists one directory of the active target's working tree — the
// same directory relative tool paths resolve against, so the sidebar
// shows exactly what the agent's tools see, local or remote. Errors
// degrade rather than fail: an unknown cwd roots at ".", an unreadable
// or vanished directory just lists empty.
func ListDir(rel string) DirListing {
	t := target()
	root, err := t.Getwd()
	if err != nil || root == "" {
		root = "."
	}
	dl := DirListing{Root: root, Path: rel}
	if !safeRelDir(rel) {
		return dl
	}
	readPath := rel
	if readPath == "" {
		readPath = "."
	}
	ents, err := t.ReadDir(readPath)
	if err != nil {
		return dl
	}
	// Directories first, then files, each alphabetical case-insensitively
	// — what file explorers do, and stable across polls so the sidebar
	// doesn't reshuffle.
	sort.SliceStable(ents, func(i, j int) bool {
		if ents[i].IsDir() != ents[j].IsDir() {
			return ents[i].IsDir()
		}
		return strings.ToLower(ents[i].Name()) < strings.ToLower(ents[j].Name())
	})
	for _, e := range ents {
		if len(dl.Entries) >= dirListMaxEntries {
			dl.Truncated = true
			break
		}
		if e.Name() == ".git" && e.IsDir() {
			// Huge, machine-managed, and never what a file sidebar is for.
			continue
		}
		dl.Entries = append(dl.Entries, FileTreeEntry{Name: e.Name(), Dir: e.IsDir()})
	}
	return dl
}

// safeRelDir keeps listings rooted at the target's cwd: only clean,
// slash-separated relative paths ("" for the root itself) are accepted —
// no absolute paths, drive letters, empty segments, or "..". Less a
// security boundary (the agent's own tools can already touch the whole
// filesystem) than a guarantee this endpoint can't be steered somewhere
// the sidebar never displays.
func safeRelDir(rel string) bool {
	if rel == "" {
		return true
	}
	if strings.HasPrefix(rel, "/") || strings.HasPrefix(rel, `\`) || strings.Contains(rel, ":") {
		return false
	}
	for seg := range strings.SplitSeq(rel, "/") {
		if seg == "" || seg == "." || seg == ".." || strings.Contains(seg, `\`) {
			return false
		}
	}
	return true
}
