package cmd

import (
	"reflect"
	"testing"

	"github.com/isometry/gobottle/internal/bottle"
)

// TestIgnoredSymlinkWarnings covers the dedup logic runBuild relies on: the
// same archive symlink recurs across every artifact and platform in a run,
// so it must warn exactly once, keyed on "name -> target".
func TestIgnoredSymlinkWarnings(t *testing.T) {
	warned := map[string]bool{}

	b1 := &bottle.Bottle{IgnoredSymlinks: []bottle.IgnoredSymlink{
		{Name: "kubectl_complete-mytool", Target: "mytool"},
	}}
	want := []string{
		"artifact contains symlink kubectl_complete-mytool -> mytool, which is not declared in binaries[].links and will not be bottled",
	}
	if msgs := ignoredSymlinkWarnings(b1, warned); !reflect.DeepEqual(msgs, want) {
		t.Errorf("messages = %v, want %v", msgs, want)
	}

	// The same symlink recurring in another artifact/platform's bottle must
	// not warn again.
	b2 := &bottle.Bottle{IgnoredSymlinks: []bottle.IgnoredSymlink{
		{Name: "kubectl_complete-mytool", Target: "mytool"},
	}}
	if msgs := ignoredSymlinkWarnings(b2, warned); len(msgs) != 0 {
		t.Errorf("messages = %v, want none (already warned)", msgs)
	}
}

// TestIgnoredSymlinkWarningsDistinctTargets covers two symlinks that share a
// name but not a target (e.g. differing per platform): each is a distinct
// key and both must warn.
func TestIgnoredSymlinkWarningsDistinctTargets(t *testing.T) {
	warned := map[string]bool{}
	b := &bottle.Bottle{IgnoredSymlinks: []bottle.IgnoredSymlink{
		{Name: "a", Target: "x"},
		{Name: "a", Target: "y"},
	}}
	if msgs := ignoredSymlinkWarnings(b, warned); len(msgs) != 2 {
		t.Errorf("messages = %v, want 2", msgs)
	}
}

// TestIgnoredSymlinkWarningsNone covers a bottle with no ignored symlinks.
func TestIgnoredSymlinkWarningsNone(t *testing.T) {
	warned := map[string]bool{}
	if msgs := ignoredSymlinkWarnings(&bottle.Bottle{}, warned); msgs != nil {
		t.Errorf("messages = %v, want nil", msgs)
	}
}
