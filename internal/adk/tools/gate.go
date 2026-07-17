package tools

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"google.golang.org/genai"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/tool"

	"tui-testing/internal/settings"
)

// This file exists because ADK dispatches every function call in a
// model's turn into its own goroutine with no ordering guarantee (see
// internal/llminternal/base_flow.go's handleFunctionCalls) — including a
// call that pauses for HITL confirmation, which returns almost
// immediately (it just registers the pending confirmation) while a
// sibling call with no confirmation requirement runs its real,
// side-effecting logic right away in the same pass. A tool like
// write_file racing ahead of (or behind) a list_files/read_file call on
// the same path is exactly the "racing tools" problem that made this
// app hold off on filesystem-mutating tools earlier — see
// [[tool-call-concurrency]] in memory.
//
// The fix here is deliberately narrow: serialize only calls whose
// declared resources actually overlap, within the same ADK session, and
// only when at least one side is a write — two reads never block each
// other, and two writes to different files never block each other
// either. A call still awaiting HITL confirmation keeps holding its
// resource(s) until the confirmation is resolved (approved, denied, or
// the resumed call otherwise completes), which is what specifically
// prevents a non-confirmed sibling from completing out from under a
// paused one.
//
// Known scope boundary: this only serializes calls within one ADK
// session. A specialist consulted via agent-as-tool runs its own tool
// calls in a fresh, disposable session (agenttool.Run creates one per
// call — see [[adk-multi-agent-composition]]), so a specialist's
// internal file writes aren't visible to the root session's gate, or to
// another specialist's. Root-vs-specialist and specialist-vs-specialist
// races on the same file aren't caught by this — only races among calls
// the same session directly makes. Revisit only if that combination
// actually causes a problem; today's specialists are user-authored and
// rare enough that it hasn't.
//
// Everything here is unexported: gated/confirmGated and the resourceRef
// helpers (fileRef/dirRef) are only ever used from within this package,
// by each tool's own constructor (see list_files.go/read_file.go/
// write_file.go for the pattern) — nothing outside internal/adk/tools
// needs to know this machinery exists, only the resulting tool.Tool
// values registry.go hands back.

// resourceRef describes one resource a tool call touches. Two refs
// conflict when their keys overlap (exact match, or containment when
// one side is Recursive — a directory scope like list_files) and at
// least one of them is a Write.
type resourceRef struct {
	Key       string // normalized (cleaned, absolute) path
	Write     bool
	Recursive bool // Key is a directory; conflicts with any ref whose Key falls under it
}

func fileRef(path string, write bool) resourceRef {
	return resourceRef{Key: normalizePath(path), Write: write}
}

func dirRef(path string) resourceRef {
	return resourceRef{Key: normalizePath(path), Recursive: true}
}

func normalizePath(p string) string {
	if p == "" {
		p = "."
	}
	if abs, err := filepath.Abs(p); err == nil {
		return abs
	}
	return filepath.Clean(p)
}

func refsConflict(a, b resourceRef) bool {
	if !a.Write && !b.Write {
		return false
	}
	switch {
	case a.Recursive && b.Recursive:
		return isUnderOrEqual(a.Key, b.Key) || isUnderOrEqual(b.Key, a.Key)
	case a.Recursive:
		return isUnderOrEqual(b.Key, a.Key)
	case b.Recursive:
		return isUnderOrEqual(a.Key, b.Key)
	default:
		return a.Key == b.Key
	}
}

// isUnderOrEqual reports whether child is dir itself or a path beneath it.
func isUnderOrEqual(child, dir string) bool {
	if child == dir {
		return true
	}
	rel, err := filepath.Rel(dir, child)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// sessionGate serializes conflicting tool calls for one ADK session.
// active holds every call currently running (or paused on a HITL
// confirmation) along with the resources it's holding; parked holds the
// release func for a call that's paused on confirmation, so the resumed
// call can find and eventually invoke it instead of re-acquiring (which
// would deadlock against its own still-active entry).
type sessionGate struct {
	mu     sync.Mutex
	cond   *sync.Cond
	active []heldRefs
	parked map[string]func()
}

type heldRefs struct {
	callID string
	refs   []resourceRef
}

var (
	gatesMu sync.Mutex
	gates   = map[string]*sessionGate{}
)

// gateFor returns the gate for a session, creating it on first use.
// Gates live for the process's lifetime — cheap enough (an empty
// mutex+cond+nil slices when idle) that per-session eviction isn't
// worth the complexity for a single-user app's session count.
func gateFor(sessionID string) *sessionGate {
	gatesMu.Lock()
	defer gatesMu.Unlock()
	g, ok := gates[sessionID]
	if !ok {
		g = &sessionGate{parked: map[string]func(){}}
		g.cond = sync.NewCond(&g.mu)
		gates[sessionID] = g
	}
	return g
}

// acquire blocks until refs don't conflict with anything currently held
// in this session, then registers them as held and returns a release
// func. Safe to call again for a callID that's already held (the
// defensive fallback path in gatedTool.Run) — any stale entry for it is
// dropped before conflicts are evaluated.
func (g *sessionGate) acquire(callID string, refs []resourceRef) func() {
	g.mu.Lock()
	g.removeActiveLocked(callID)
	for g.conflictsLocked(refs) {
		g.cond.Wait()
	}
	g.active = append(g.active, heldRefs{callID: callID, refs: refs})
	g.mu.Unlock()

	return func() {
		g.mu.Lock()
		g.removeActiveLocked(callID)
		g.mu.Unlock()
		g.cond.Broadcast()
	}
}

// park stashes release for a call that's paused awaiting HITL
// confirmation — its resources stay in active (still blocking
// conflicting siblings) until takeParked retrieves this func and the
// caller invokes it once the resumed call finishes.
func (g *sessionGate) park(callID string, release func()) {
	g.mu.Lock()
	g.parked[callID] = release
	g.mu.Unlock()
}

// takeParked retrieves and forgets the release func parked for callID,
// or nil if none is parked (a fresh call, not a confirmation resume).
func (g *sessionGate) takeParked(callID string) func() {
	g.mu.Lock()
	defer g.mu.Unlock()
	release, ok := g.parked[callID]
	if ok {
		delete(g.parked, callID)
	}
	return release
}

func (g *sessionGate) conflictsLocked(refs []resourceRef) bool {
	for _, h := range g.active {
		for _, a := range h.refs {
			for _, b := range refs {
				if refsConflict(a, b) {
					return true
				}
			}
		}
	}
	return false
}

func (g *sessionGate) removeActiveLocked(callID string) {
	for i, h := range g.active {
		if h.callID == callID {
			g.active = append(g.active[:i], g.active[i+1:]...)
			return
		}
	}
}

// funcTool is the subset of tool.Tool that every tool built in this
// package (all via functiontool.New) actually implements — mirrors
// ADK's own internal toolinternal.FunctionTool + RequestProcessor
// interfaces, which live in an internal package we can't import, so
// gatedTool restates just enough of the shape to forward everything
// ADK's flow needs beyond the bare tool.Tool interface.
type funcTool interface {
	tool.Tool
	Declaration() *genai.FunctionDeclaration
	ProcessRequest(ctx agent.Context, req *model.LLMRequest) error
	Run(ctx agent.Context, args any) (map[string]any, error)
}

// gatedTool wraps a tool.Tool with resource-conflict-aware
// serialization — see this file's package-level doc comment.
type gatedTool struct {
	funcTool
	resources func(args map[string]any) []resourceRef
}

// gated wraps t so that calls whose declared resources overlap serialize
// within the same session instead of racing. resources extracts what a
// call's raw args touch — return nil for a tool with no meaningful
// resource shape to run it unguarded. Panics if t isn't built the way
// every tool in this package is (via functiontool.New) — a programmer
// error to catch immediately, not a runtime condition.
func gated(t tool.Tool, resources func(args map[string]any) []resourceRef) tool.Tool {
	ft, ok := t.(funcTool)
	if !ok {
		panic(fmt.Sprintf("adk/tools: gated: %q does not implement the expected function-tool interface", t.Name()))
	}
	return &gatedTool{funcTool: ft, resources: resources}
}

func (g *gatedTool) Run(ctx agent.Context, args any) (map[string]any, error) {
	m, _ := args.(map[string]any)
	refs := g.resources(m)
	if len(refs) == 0 {
		return g.funcTool.Run(ctx, args)
	}

	gate := gateFor(ctx.SessionID())
	callID := ctx.FunctionCallID()

	release := gate.takeParked(callID)
	if release == nil {
		release = gate.acquire(callID, refs)
	}

	result, err := g.funcTool.Run(ctx, args)

	if errors.Is(err, tool.ErrConfirmationRequired) {
		gate.park(callID, release)
		return result, err
	}
	release()
	return result, err
}

// ProcessRequest must be overridden, not left to promote through to the
// wrapped tool: functiontool.ProcessRequest calls the internal
// toolutils.PackTool(req, f), which registers *its own receiver* — i.e.
// the innermost raw tool — into req.Tools, the map ADK's flow later
// turns into the actual execution dict (see base_flow.go's "tools" map
// built from req.Tools around its handleFunctionCalls call sites). Left
// unoverridden, every wrapper in this file would be silently bypassed
// for real dispatch — Declaration/Name would still work as promoted,
// but Run would never be reached at all — see [[tool-call-concurrency]]
// in memory for the full story of how this was caught. The fix: call
// the wrapped tool's ProcessRequest first (it still does the real work
// of populating req.Config/the schema), then overwrite req.Tools[name]
// to point at this wrapper instead.
func (g *gatedTool) ProcessRequest(ctx agent.Context, req *model.LLMRequest) error {
	if err := g.funcTool.ProcessRequest(ctx, req); err != nil {
		return err
	}
	req.Tools[g.Name()] = g
	return nil
}

// confirmGatedTool wraps a tool.Tool so whether it actually asks for
// human confirmation is decided fresh on every call, instead of being
// fixed at construction time via functiontool.Config.RequireConfirmation
// — ADK's own RequireConfirmationProvider hook can't do this, since its
// signature (func(TArgs) bool) never receives agent.Context, so it can
// neither read the active permission mode nor tell a root call apart
// from a sub-agent one. This wrapper can, because it sees ctx directly.
//
// Composition matters: confirmGated must wrap the *raw* tool, with
// gated (resource-conflict serialization) wrapping *that* — i.e.
// gated(confirmGated(t, ...), resources), never the other way around.
// gatedTool.Run already knows how to hold a resource lock through a
// paused confirmation (see its ErrConfirmationRequired handling above);
// if confirmGated wrapped the outside instead, it would return
// ErrConfirmationRequired before the resource gate ever engaged, and
// that hold-through-confirmation guarantee would never trigger.
type confirmGatedTool struct {
	funcTool
	// normallyRequires is what "normal" mode uses for this specific
	// tool — e.g. true for write_file, false for a read-only tool.
	normallyRequires bool
	// rootName is the app's actual root agent's configured name (see
	// internal/adk/rootagent.go) — a call whose ctx.AgentName() doesn't
	// match it is happening inside a sub-agent's own disposable run,
	// which can never resolve a confirmation at all (see this file's
	// package doc and [[adk-multi-agent-composition]] in memory), so it
	// always auto-accepts regardless of the active permission mode.
	// AgentName(), not Agent().Name() — agent.Context's Agent() is
	// deliberately stubbed to nil for a tool context (confirmed in
	// agent/tool_context_wrapper.go: "Agent() is not supported for tool
	// context", logged and returns nil, which panics on .Name()).
	// AgentName() is the one ADK actually forwards correctly here.
	rootName string
}

// confirmGated wraps t so a confirmation is requested before it runs
// according to the active permission mode (see internal/settings'
// PermissionMode) — full-auto never confirms, normal uses
// normallyRequires. A sub-agent's own tool calls always auto-accept
// unconditionally, as a stopgap until sub-agents can participate in
// HITL for real. Panics if t isn't built the way every tool in this
// package is (via functiontool.New) — a programmer error to catch
// immediately, not a runtime condition.
func confirmGated(t tool.Tool, normallyRequires bool, rootName string) tool.Tool {
	ft, ok := t.(funcTool)
	if !ok {
		panic(fmt.Sprintf("adk/tools: confirmGated: %q does not implement the expected function-tool interface", t.Name()))
	}
	return &confirmGatedTool{funcTool: ft, normallyRequires: normallyRequires, rootName: rootName}
}

func (c *confirmGatedTool) Run(ctx agent.Context, args any) (map[string]any, error) {
	// ctx.ToolConfirmation() being non-nil means a decision already
	// exists for this call (we requested one below on an earlier pass,
	// and the human has since answered).
	tc := ctx.ToolConfirmation()
	if tc == nil && c.requiresConfirmation(ctx.AgentName(), settings.Load().Agent.PermissionMode) {
		hint := fmt.Sprintf("Please approve or reject the tool call %s() by responding with a FunctionResponse with an expected ToolConfirmation payload.", c.Name())
		if err := ctx.RequestConfirmation(hint, nil); err != nil {
			return nil, err
		}
		ctx.Actions().SkipSummarization = true
		return nil, fmt.Errorf("error tool %q %w", c.Name(), tool.ErrConfirmationRequired)
	}
	if tc != nil && !tc.Confirmed {
		// Caught here, before this ever reaches the wrapped tool: ADK's own
		// functiontool.Run would report a rejection as "error tool %q call
		// is rejected" — wording that reads like a generic system/
		// permission failure, and live testing showed a model treating it
		// as exactly that (something to retry, or find a workaround for)
		// rather than a human's deliberate decision. This says plainly who
		// rejected it and that retrying won't help.
		return nil, fmt.Errorf("the user was asked to approve %s() and explicitly REJECTED it — this was a deliberate human decision. Do not retry this call or attempt another way to accomplish the same thing; ask how they'd like to continue", c.Name())
	}
	return c.funcTool.Run(ctx, args)
}

// ProcessRequest is overridden for the same reason gatedTool's is — see
// gatedTool.ProcessRequest's doc comment for the full explanation.
func (c *confirmGatedTool) ProcessRequest(ctx agent.Context, req *model.LLMRequest) error {
	if err := c.funcTool.ProcessRequest(ctx, req); err != nil {
		return err
	}
	req.Tools[c.Name()] = c
	return nil
}

// requiresConfirmation is pure decision logic, deliberately taking the
// current agent's name and the active permission mode as plain values
// rather than agent.Context/a settings.Load() call inside it — keeps
// this testable without needing to fake ADK's context interface.
func (c *confirmGatedTool) requiresConfirmation(currentAgentName, mode string) bool {
	if currentAgentName != c.rootName {
		return false
	}
	if mode == settings.ModeFullAuto {
		return false
	}
	return c.normallyRequires
}
