package ui

import (
	"strings"
	"testing"
	"time"
)

// These pin refreshTranscript's cache behavior directly — build+vet
// can't catch a caching bug (stale content silently sticking around, or
// the opposite failure of never actually caching anything), and this is
// exactly the kind of invalidation logic worth distrusting until proven,
// per this repo's own testing norms.

// TestRefreshTranscriptReusesCacheWhenNothingChanged proves the cache is
// genuinely reused, not just producing identical output by coincidence:
// mutating a.messages directly without calling touchMessages() must NOT
// be picked up, since the whole point is that refreshTranscript never
// re-derives "did anything change" from messages itself — only from the
// revision touchMessages bumps.
func TestRefreshTranscriptReusesCacheWhenNothingChanged(t *testing.T) {
	a := newTestApp()
	a.messages = []ChatMessage{{Role: RoleSystem, Content: "original", At: time.Now()}}
	a.touchMessages()
	a.refreshTranscript()
	original := a.transcriptCacheContent

	a.messages[0].Content = "mutated but not touched"
	a.refreshTranscript()

	if a.transcriptCacheContent != original {
		t.Errorf("cache was not reused: content changed despite no touchMessages() call")
	}
	if !strings.Contains(a.transcriptCacheContent, "original") {
		t.Errorf("expected cached content to still show the pre-mutation text, got %q", a.transcriptCacheContent)
	}
}

// TestRefreshTranscriptRecomputesAfterTouchMessages is the other half:
// a genuine mutation, correctly reported via touchMessages(), must
// actually show up.
func TestRefreshTranscriptRecomputesAfterTouchMessages(t *testing.T) {
	a := newTestApp()
	a.messages = []ChatMessage{{Role: RoleSystem, Content: "original", At: time.Now()}}
	a.touchMessages()
	a.refreshTranscript()

	a.messages[0].Content = "updated"
	a.touchMessages()
	a.refreshTranscript()

	if !strings.Contains(a.transcriptCacheContent, "updated") {
		t.Errorf("expected recompute after touchMessages(), content = %q", a.transcriptCacheContent)
	}
	if strings.Contains(a.transcriptCacheContent, "original") {
		t.Errorf("stale content still present after touchMessages()+refresh: %q", a.transcriptCacheContent)
	}
}

// TestRefreshTranscriptRecomputesWhenGlobalsChange covers the other half
// of transcriptCacheKey: a setting that changes how a message renders
// (verboseTools, here) must still trigger a recompute even though
// nothing about messages itself changed — the cache key isn't just the
// revision counter.
func TestRefreshTranscriptRecomputesWhenGlobalsChange(t *testing.T) {
	a := newTestApp()
	a.messages = []ChatMessage{{
		Role:       RoleTool,
		ToolName:   "list_files",
		ToolResult: map[string]any{"files": []any{"a.go", "b.go"}},
		At:         time.Now(),
	}}
	a.touchMessages()
	a.verboseTools = false
	a.refreshTranscript()
	lean := a.transcriptCacheContent

	a.verboseTools = true // deliberately no touchMessages() — messages didn't change, a setting did
	a.refreshTranscript()
	verbose := a.transcriptCacheContent

	if lean == verbose {
		t.Errorf("expected a verboseTools toggle to change rendered content without touchMessages()")
	}
}
