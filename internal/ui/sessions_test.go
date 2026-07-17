package ui

import "testing"

// TestReplayTranscriptBasic covers the straightforward shape: a user
// message, an agent reply, then a second turn with a tool call/result in
// the middle — replayTranscript should end up with the same message
// shape live streaming would have produced (RoleUser, RoleAgent, RoleTool,
// RoleAgent), not a flattened or duplicated one.
func TestReplayTranscriptBasic(t *testing.T) {
	a := newTestApp()
	a.replayTranscript([]TranscriptEntry{
		{UserText: "hello"},
		{Text: "hi there"},
		{UserText: "read foo.txt"},
		{ToolCall: &ToolCall{ID: "c1", Name: "read_file", Args: map[string]any{"path": "foo.txt"}}},
		{ToolResult: &ToolResult{ID: "c1", Name: "read_file", Result: map[string]any{"content": "hello world"}}},
		{Text: "here it is"},
	})

	wantRoles := []Role{RoleUser, RoleAgent, RoleUser, RoleTool, RoleAgent}
	if len(a.messages) != len(wantRoles) {
		t.Fatalf("got %d messages, want %d: %+v", len(a.messages), len(wantRoles), a.messages)
	}
	for i, want := range wantRoles {
		if a.messages[i].Role != want {
			t.Errorf("message[%d].Role = %v, want %v", i, a.messages[i].Role, want)
		}
	}
	if a.messages[1].Content != "hi there" {
		t.Errorf("first reply = %q, want %q", a.messages[1].Content, "hi there")
	}
	if a.messages[4].Content != "here it is" {
		t.Errorf("second reply = %q, want %q", a.messages[4].Content, "here it is")
	}
	result := a.messages[3].ToolResult
	if result == nil || result["content"] != "hello world" {
		t.Errorf("tool result = %v, want content=hello world", result)
	}
}

// TestReplayTranscriptDoubleResultSameID covers a real shape confirmed
// against the app's own sqlite session store (not a hypothetical): a
// confirmation-gated tool call persists *two* ToolResult events under
// the same call ID — an auto-generated "requires confirmation" error
// placeholder first, then the real result once approved. Replay must
// land on the final one, not get stuck on (or duplicate a bubble for)
// the placeholder — completeToolMessage's plain overwrite-by-ID handles
// this by construction, but that's non-obvious enough from the code
// alone to pin down with a test.
func TestReplayTranscriptDoubleResultSameID(t *testing.T) {
	a := newTestApp()
	a.replayTranscript([]TranscriptEntry{
		{UserText: "write it"},
		{ToolCall: &ToolCall{ID: "c1", Name: "write_file", Args: map[string]any{"path": "x.txt"}}},
		{ToolResult: &ToolResult{ID: "c1", Name: "write_file", Result: map[string]any{"error": "requires confirmation"}}},
		{ToolResult: &ToolResult{ID: "c1", Name: "write_file", Result: map[string]any{"bytesWritten": 115}}},
		{Text: "done"},
	})

	var toolMsgs []ChatMessage
	for _, m := range a.messages {
		if m.Role == RoleTool {
			toolMsgs = append(toolMsgs, m)
		}
	}
	if len(toolMsgs) != 1 {
		t.Fatalf("got %d RoleTool messages, want exactly 1 (no duplicate bubble): %+v", len(toolMsgs), toolMsgs)
	}
	if _, hasErr := toolMsgs[0].ToolResult["error"]; hasErr {
		t.Errorf("tool result still shows the placeholder error, want the final result: %v", toolMsgs[0].ToolResult)
	}
	if toolMsgs[0].ToolResult["bytesWritten"] != 115 {
		t.Errorf("tool result = %v, want bytesWritten=115", toolMsgs[0].ToolResult)
	}
}

// TestReplayTranscriptTrailingToolCallNoText covers a session that ends
// right after a tool result with no closing prose (e.g. the app was
// closed mid-turn) — the trailing empty agent placeholder replayTranscript
// opens after every user/tool entry should be dropped, not left behind
// as a stray empty bubble.
func TestReplayTranscriptTrailingToolCallNoText(t *testing.T) {
	a := newTestApp()
	a.replayTranscript([]TranscriptEntry{
		{UserText: "list files"},
		{ToolCall: &ToolCall{ID: "c1", Name: "list_files", Args: nil}},
		{ToolResult: &ToolResult{ID: "c1", Name: "list_files", Result: map[string]any{"files": []any{"a.go"}}}},
	})

	if len(a.messages) == 0 {
		t.Fatal("expected at least the user + tool messages")
	}
	last := a.messages[len(a.messages)-1]
	if last.Role == RoleAgent && last.Content == "" {
		t.Errorf("trailing empty agent placeholder was not dropped: %+v", a.messages)
	}
}
