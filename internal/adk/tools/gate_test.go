package tools

import (
	"path/filepath"
	"testing"
	"time"
)

func TestRefsConflict(t *testing.T) {
	tests := []struct {
		name string
		a, b resourceRef
		want bool
	}{
		{
			name: "two reads on the same file never conflict",
			a:    resourceRef{Key: "/a/b.txt"},
			b:    resourceRef{Key: "/a/b.txt"},
			want: false,
		},
		{
			name: "a read and a write on the same file conflict",
			a:    resourceRef{Key: "/a/b.txt"},
			b:    resourceRef{Key: "/a/b.txt", Write: true},
			want: true,
		},
		{
			name: "two writes on the same file conflict",
			a:    resourceRef{Key: "/a/b.txt", Write: true},
			b:    resourceRef{Key: "/a/b.txt", Write: true},
			want: true,
		},
		{
			name: "writes on different files never conflict",
			a:    resourceRef{Key: "/a/b.txt", Write: true},
			b:    resourceRef{Key: "/a/c.txt", Write: true},
			want: false,
		},
		{
			name: "a write under a recursive listing conflicts",
			a:    resourceRef{Key: "/a", Recursive: true},
			b:    resourceRef{Key: "/a/b.txt", Write: true},
			want: true,
		},
		{
			name: "a write outside a recursive listing never conflicts",
			a:    resourceRef{Key: "/a", Recursive: true},
			b:    resourceRef{Key: "/other/b.txt", Write: true},
			want: false,
		},
		{
			name: "two recursive refs over overlapping dirs conflict if either writes",
			a:    resourceRef{Key: "/a", Recursive: true, Write: true},
			b:    resourceRef{Key: "/a/sub", Recursive: true},
			want: true,
		},
		{
			name: "two recursive reads over overlapping dirs never conflict",
			a:    resourceRef{Key: "/a", Recursive: true},
			b:    resourceRef{Key: "/a/sub", Recursive: true},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := refsConflict(tt.a, tt.b); got != tt.want {
				t.Errorf("refsConflict(a, b) = %v, want %v", got, tt.want)
			}
			// Conflict detection has to be symmetric — which side a
			// caller happens to pass first is arbitrary.
			if got := refsConflict(tt.b, tt.a); got != tt.want {
				t.Errorf("refsConflict(b, a) = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsUnderOrEqual(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "workspace")
	tests := []struct {
		name        string
		child, base string
		want        bool
	}{
		{"identical paths", dir, dir, true},
		{"direct child", filepath.Join(dir, "file.txt"), dir, true},
		{"nested child", filepath.Join(dir, "a", "b", "c.txt"), dir, true},
		{"unrelated sibling", filepath.Join(filepath.Dir(dir), "other"), dir, false},
		{"parent is not under its own child", filepath.Dir(dir), dir, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isUnderOrEqual(tt.child, tt.base); got != tt.want {
				t.Errorf("isUnderOrEqual(%q, %q) = %v, want %v", tt.child, tt.base, got, tt.want)
			}
		})
	}
}

// TestSessionGateBlocksConflictingAcquire is the load-bearing behavior
// gate.go exists for: a call holding a write on a path has to keep a
// conflicting sibling call waiting until it releases, not let them race.
func TestSessionGateBlocksConflictingAcquire(t *testing.T) {
	g := gateFor("test-session-" + t.Name())
	path := filepath.Join(t.TempDir(), "shared.txt")
	refs := []resourceRef{fileRef(path, true)}

	release1 := g.acquire("call-1", refs)

	acquired := make(chan func())
	go func() {
		acquired <- g.acquire("call-2", refs)
	}()

	select {
	case <-acquired:
		t.Fatal("acquire for a conflicting ref returned before the holder released")
	case <-time.After(50 * time.Millisecond):
		// Still blocked, as expected.
	}

	release1()

	select {
	case release2 := <-acquired:
		release2()
	case <-time.After(time.Second):
		t.Fatal("acquire never unblocked after the conflicting holder released")
	}
}

// TestSessionGateAllowsNonConflictingAcquire makes sure the gate isn't
// accidentally serializing everything — only calls whose resources
// actually overlap should ever wait on each other.
func TestSessionGateAllowsNonConflictingAcquire(t *testing.T) {
	g := gateFor("test-session-" + t.Name())
	dir := t.TempDir()
	refsA := []resourceRef{fileRef(filepath.Join(dir, "a.txt"), true)}
	refsB := []resourceRef{fileRef(filepath.Join(dir, "b.txt"), true)}

	done := make(chan func(), 2)
	go func() { done <- g.acquire("call-1", refsA) }()
	go func() { done <- g.acquire("call-2", refsB) }()

	for range 2 {
		select {
		case release := <-done:
			release()
		case <-time.After(time.Second):
			t.Fatal("non-conflicting acquires should not block each other")
		}
	}
}

// TestSessionGateAcquireDropsStaleEntryForSameCallID covers the
// defensive fallback acquire's doc comment describes: re-acquiring under
// a callID that's still (incorrectly) held must not deadlock against its
// own earlier entry.
func TestSessionGateAcquireDropsStaleEntryForSameCallID(t *testing.T) {
	g := gateFor("test-session-" + t.Name())
	dir := t.TempDir()

	g.acquire("call-1", []resourceRef{fileRef(filepath.Join(dir, "a.txt"), true)})
	// Deliberately never released — simulates a stale entry left behind
	// for this callID.

	done := make(chan func())
	go func() {
		done <- g.acquire("call-1", []resourceRef{fileRef(filepath.Join(dir, "b.txt"), true)})
	}()

	select {
	case release := <-done:
		release()
	case <-time.After(time.Second):
		t.Fatal("re-acquiring the same callID deadlocked against its own stale entry")
	}
}

func TestSessionGateParkAndTakeParked(t *testing.T) {
	g := gateFor("test-session-" + t.Name())

	if release := g.takeParked("never-parked"); release != nil {
		t.Fatal("takeParked on a callID that was never parked should return nil")
	}

	called := false
	g.park("call-1", func() { called = true })

	release := g.takeParked("call-1")
	if release == nil {
		t.Fatal("takeParked did not return the parked release func")
	}
	release()
	if !called {
		t.Fatal("takeParked returned a different func than the one park was given")
	}

	if release := g.takeParked("call-1"); release != nil {
		t.Fatal("takeParked should forget the entry after it's retrieved once")
	}
}
