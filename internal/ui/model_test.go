package ui

import (
	"fmt"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/tkzzzzzz6/pvman/internal/conda"
	"github.com/tkzzzzzz6/pvman/internal/uv"
)

// TestMoveCursorWrapAround covers the reported bug where moving up from the
// first environment crashed with "index out of range [-1]".
func TestMoveCursorWrapAround(t *testing.T) {
	m := &Model{items: []listItem{
		{kind: kindHeader, label: "conda"},
		{kind: kindEnv, envType: "conda", idx: 0},
		{kind: kindEnv, envType: "conda", idx: 1},
	}}

	// From the first selectable item, "up" must land on the last one.
	m.cursor = 1
	m.moveCursor(-1)
	if m.cursor != 2 {
		t.Fatalf("up from first: cursor = %d, want 2", m.cursor)
	}

	// From the last, "down" must wrap to the first selectable item.
	m.moveCursor(1)
	if m.cursor != 1 {
		t.Fatalf("down from last: cursor = %d, want 1", m.cursor)
	}
}

func TestMoveCursorEmptyAndAllHeaders(t *testing.T) {
	empty := &Model{}
	empty.moveCursor(1) // must not panic
	empty.moveCursor(-1)

	headers := &Model{items: []listItem{{kind: kindHeader}, {kind: kindHeader}}}
	headers.moveCursor(1) // must not panic on an all-header list
	headers.moveCursor(-1)
}

// TestPkgStateSafety drives the package view across cursor positions and
// selection maps that no longer match the loaded list, which is what happens
// when the list shrinks after a deletion.
func TestPkgStateSafety(t *testing.T) {
	for _, n := range []int{0, 1, 5, 30} {
		for _, cur := range []int{-3, -1, 0, 1, n - 1, n, n + 5, 30} {
			for _, sel := range []map[int]bool{
				nil,
				{},
				{0: true},
				{5: true, 25: true},
				{100: true},
			} {
				m := Model{
					state:       statePackageList,
					width:       80,
					height:      24,
					packages:    mkPkgs(n),
					pkgCursor:   cur,
					pkgSelected: sel,
					pkgLoaded:   true,
				}
				_ = m.viewPackages()
				_ = m.viewPackageDeleteConfirm()

				for _, k := range []string{"down", "up", " ", "a", "d", "y", "esc"} {
					w := m
					mm, _ := w.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)})
					got := mm.(Model)
					if got.pkgCursor < 0 || (n > 0 && got.pkgCursor >= n) {
						t.Fatalf("n=%d cur=%d key=%q -> cursor %d out of range", n, cur, k, got.pkgCursor)
					}
					for i := range got.pkgSelected {
						if i >= n {
							t.Fatalf("n=%d cur=%d key=%q -> stale selection %d", n, cur, k, i)
						}
					}
				}
			}
		}
	}
}

func TestDeletePackagesCmdWithNoNames(t *testing.T) {
	cmd := deletePackagesCmd("conda", 0, nil, nil, nil)
	if cmd == nil {
		t.Fatal("nil cmd")
	}
	msg, ok := cmd().(packageDeletedMsg)
	if !ok {
		t.Fatal("unexpected message type")
	}
	if msg.err == nil {
		t.Fatal("expected an error when nothing is named")
	}
}

// TestDeleteConfirmSurvivesShrinkingList reproduces a panic: open the delete
// confirmation on the last environment, let a refresh land with a shorter list
// while the dialog is still open, then press y.
func TestDeleteConfirmSurvivesShrinkingList(t *testing.T) {
	m := New()
	m.width, m.height = 100, 30
	m.condaEnvs = []conda.Env{{Name: "A"}, {Name: "B"}, {Name: "C"}}
	m.rebuildItems()
	for i, it := range m.items {
		if it.kind == kindEnv && it.label == "C" {
			m.cursor = i
		}
	}

	mm, _ := m.Update(keyRune('d'))
	m = mm.(Model)
	if m.state != stateDeleteConfirm {
		t.Fatalf("state = %v, want delete confirm", m.state)
	}

	// A refresh resolves with fewer environments than the dialog was built on.
	mm, _ = m.Update(envsLoadedMsg{condaEnvs: []conda.Env{{Name: "A"}}})
	m = mm.(Model)
	if m.state == stateDeleteConfirm {
		t.Fatal("stale confirmation dialog was kept after the list shrank")
	}

	// And even if a stale target reached the command, it must not panic.
	if cmd := deleteEnvCmd(listItem{kind: kindEnv, envType: "conda", idx: 5},
		[]conda.Env{{Name: "A"}}, nil); cmd != nil {
		msg, ok := cmd().(envDeletedMsg)
		if !ok {
			t.Fatalf("unexpected msg %T", cmd())
		}
		if msg.err == nil {
			t.Fatal("expected an error for an out-of-range environment")
		}
	}
}

// TestDetailsForVanishedEnv covers a detail load resolving after a refresh
// removed the environment it was loading for.
func TestDetailsForVanishedEnv(t *testing.T) {
	m := New()
	m.condaEnvs = []conda.Env{{Name: "A"}}
	mm, _ := m.Update(detailsLoadedMsg{envType: "conda", idx: 7})
	_ = mm.(Model) // must not panic
}

// runKey applies one key to the model and returns the result.
func runKey(m Model, k tea.KeyMsg) Model {
	mm, _ := m.Update(k)
	return mm.(Model)
}

func pkgModel(n int) Model {
	m := New()
	m.state = statePackageList
	m.width = 80
	m.height = 24
	m.packages = mkPkgs(n)
	m.pkgSelected = make(map[int]bool)
	m.pkgLoaded = true
	return m
}

// TestUntickDropsSelection guards the invariant that len(pkgSelected) is the
// number of ticked packages: unticking must remove the entry rather than store
// false, which would inflate the count, the "N selected" footer, the delete
// confirmation and the "Deleted N package(s)" message.
func TestUntickDropsSelection(t *testing.T) {
	m := pkgModel(3)

	m = runKey(m, keyRune(' ')) // tick 0
	if got := len(m.pkgSelected); got != 1 {
		t.Fatalf("after tick: %d selected, want 1", got)
	}

	m = runKey(m, keyRune(' ')) // untick 0
	if got := len(m.pkgSelected); got != 0 {
		t.Fatalf("after untick: %d selected, want 0", got)
	}
	if m.pkgSelected[0] {
		t.Fatal("index 0 still ticked after untick")
	}
}

// TestSelectAllTogglesCleanly covers the select-all key seeing a stale count.
func TestSelectAllTogglesCleanly(t *testing.T) {
	m := pkgModel(3)

	m = runKey(m, keyRune('a')) // select all
	if got := len(m.pkgSelected); got != 3 {
		t.Fatalf("after select-all: %d selected, want 3", got)
	}

	// Tick one off, then select-all again: it must select, not clear.
	m = runKey(m, keyRune(' '))
	if got := len(m.pkgSelected); got != 2 {
		t.Fatalf("after untick: %d selected, want 2", got)
	}
	m = runKey(m, keyRune('a'))
	if got := len(m.pkgSelected); got != 3 {
		t.Fatalf("select-all on a partly-ticked list: %d selected, want 3", got)
	}

	// A fully-ticked list clears.
	m = runKey(m, keyRune('a'))
	if got := len(m.pkgSelected); got != 0 {
		t.Fatalf("select-all on a fully-ticked list: %d selected, want 0", got)
	}
}

// TestPackageKeysIgnoredBeforeLoad stops a keypress that lands while the list is
// still loading from recording a selection against a package nobody has seen.
func TestPackageKeysIgnoredBeforeLoad(t *testing.T) {
	m := pkgModel(0)
	m.pkgLoaded = false

	for _, k := range []string{" ", "a", "d"} {
		got := runKey(m, keyRune(rune(k[0])))
		if len(got.pkgSelected) != 0 {
			t.Fatalf("key %q before load recorded %d selections", k, len(got.pkgSelected))
		}
		if got.state != statePackageList {
			t.Fatalf("key %q before load moved to state %v", k, got.state)
		}
	}
}

// TestCtrlCQuitsFromPackageViews makes sure ctrl+c still exits the program
// rather than being treated as "back".
func TestCtrlCQuitsFromPackageViews(t *testing.T) {
	ctrlC := tea.KeyMsg{Type: tea.KeyCtrlC}
	for _, st := range []appState{statePackageList, statePackageDeleteConfirm, statePackageDeleting} {
		m := pkgModel(3)
		m.state = st
		mm, cmd := m.Update(ctrlC)
		if cmd == nil {
			t.Fatalf("state %v: ctrl+c produced no command", st)
		}
		if _, ok := cmd().(tea.QuitMsg); !ok {
			t.Fatalf("state %v: ctrl+c did not quit", st)
		}
		if got := mm.(Model); got.state != st {
			t.Fatalf("state %v: ctrl+c changed state to %v", st, got.state)
		}
	}
}

// TestViewPackagesFitsTerminal checks the rendered panel never exceeds the
// terminal height, including when the status and "N selected" lines appear.
func TestViewPackagesFitsTerminal(t *testing.T) {
	for _, h := range []int{8, 12, 24, 40} {
		for _, n := range []int{0, 1, 5, 100} {
			m := pkgModel(n)
			m.height, m.width = h, 80
			m.statusMsg = "Deleted 3 package(s)."
			m.pkgSelected = map[int]bool{0: true, 1: true}
			if got := lipgloss.Height(m.viewPackages()); got > h {
				t.Fatalf("height=%d packages=%d: panel is %d lines", h, n, got)
			}
		}
	}
}

// TestEnvsReloadKeepsPackageView documents that an environment refresh landing
// while the package list is open does not navigate away from it.
func TestEnvsReloadKeepsPackageView(t *testing.T) {
	m := New()
	m.width, m.height = 100, 30
	m.condaEnvs = []conda.Env{{Name: "A"}, {Name: "B"}}
	m.rebuildItems()
	m.cursor = 1
	m.state = statePackageList
	m.pkgEnvType, m.pkgIdx = "conda", 1

	got := runKey(m, keyRune('r'))
	mm, _ := got.Update(envsLoadedMsg{condaEnvs: []conda.Env{{Name: "A"}}})
	if got := mm.(Model); got.state != statePackageList {
		t.Fatalf("state = %v, want the package list to stay open", got.state)
	}
}

// relModel builds a package view over a small dependency graph. packages are
// named a..e; edges are given as "dependent:needed,needed".
func relModel(packages []string, edges map[string][]string, selected ...string) Model {
	deps := make(map[string][]string, len(packages))
	dependents := make(map[string][]string, len(packages))
	for _, p := range packages {
		deps[p] = edges[p]
	}
	for pkg, needs := range deps {
		for _, n := range needs {
			dependents[n] = append(dependents[n], pkg)
		}
	}
	for k := range dependents {
		sort.Strings(dependents[k])
	}

	m := Model{
		state:       statePackageList,
		width:       100,
		height:      30,
		packages:    packages,
		pkgSelected: make(map[int]bool),
		pkgLoaded:   true,
		deps:        deps,
		dependents:  dependents,
	}
	want := make(map[string]bool, len(selected))
	for _, s := range selected {
		want[s] = true
	}
	for i, name := range packages {
		if want[name] {
			m.pkgSelected[i] = true
		}
	}
	return m
}

// TestSelectionRelationBreaksAndOrphans covers the graph shape the dialog is
// built on: a -> {b, c} -> d, and e -> a. Selecting a breaks e and orphans
// b, c and (transitively) d.
func TestSelectionRelationBreaksAndOrphans(t *testing.T) {
	m := relModel(
		[]string{"a", "b", "c", "d", "e"},
		map[string][]string{
			"a": {"b", "c"},
			"b": {"d"},
			"c": {"d"},
			"d": nil,
			"e": {"a"},
		},
		"a",
	)

	got := m.selectionRelation()
	if want := []string{"e"}; !reflect.DeepEqual(got.Breaks, want) {
		t.Errorf("Breaks = %v, want %v", got.Breaks, want)
	}
	// d is not a direct dependency of a; it is orphaned by b and c going away.
	if want := []string{"b", "c", "d"}; !reflect.DeepEqual(got.Orphans, want) {
		t.Errorf("Orphans = %v, want %v", got.Orphans, want)
	}
	if want := []string{"b", "c", "d", "e"}; !reflect.DeepEqual(got.All(), want) {
		t.Errorf("All = %v, want %v", got.All(), want)
	}
}

// TestSelectionRelationLeavesUnrelatedPackages keeps a deliberately installed
// top-level package out of the orphan list: f depends on nothing the selection
// touches, so deleting a must not propose removing f.
func TestSelectionRelationLeavesUnrelatedPackages(t *testing.T) {
	m := relModel(
		[]string{"a", "b", "f"},
		map[string][]string{
			"a": {"b"},
			"b": nil,
			"f": nil,
		},
		"a",
	)

	got := m.selectionRelation()
	if want := []string{"b"}; !reflect.DeepEqual(got.Orphans, want) {
		t.Errorf("Orphans = %v, want %v", got.Orphans, want)
	}
	if got.Empty() {
		t.Fatal("expected a non-empty relation")
	}
}

// TestSelectionRelationCycle terminates on a dependency cycle, which conda
// environments do contain.
func TestSelectionRelationCycle(t *testing.T) {
	m := relModel(
		[]string{"a", "b", "c"},
		map[string][]string{
			"a": {"b"},
			"b": {"c"},
			"c": {"a"},
		},
		"a",
	)

	got := m.selectionRelation()
	// b and c both depend, directly or not, on a; whichever way the cycle is
	// read, the walk must finish and name each of them exactly once.
	all := got.All()
	if len(all) != len(dedupe(all)) {
		t.Fatalf("All = %v contains duplicates", all)
	}
	for _, want := range []string{"b", "c"} {
		if !contains(all, want) {
			t.Errorf("All = %v, want it to contain %q", all, want)
		}
	}
}

// TestSelectionRelationDependentAlsoSelected makes sure a package that both
// depends on and is depended on by the selection is not offered for deletion.
func TestSelectionRelationDependentAlsoSelected(t *testing.T) {
	m := relModel(
		[]string{"a", "b", "c"},
		map[string][]string{
			"a": {"b"}, // a needs b
			"b": {"a"}, // and b needs a
			"c": {"b"},
		},
		"a", "b",
	)

	got := m.selectionRelation()
	for _, n := range got.All() {
		if n == "a" || n == "b" {
			t.Errorf("All = %v names a package that is already selected", got.All())
		}
	}
	// c is a dependent of the selection and stays behind, so it breaks.
	if want := []string{"c"}; !reflect.DeepEqual(got.Breaks, want) {
		t.Errorf("Breaks = %v, want %v", got.Breaks, want)
	}
}

func TestSelectionRelationNoGraph(t *testing.T) {
	m := pkgModel(3)
	m.pkgSelected = map[int]bool{0: true}
	if got := m.selectionRelation(); !got.Empty() {
		t.Errorf("relation = %+v, want empty when there is no graph", got)
	}
}

// TestDeleteConfirmYNESemantics pins the three-way confirmation: y deletes the
// selection plus the related packages, n deletes only the ticked ones, and esc
// returns to the list with nothing deleted.
func TestDeleteConfirmYNESemantics(t *testing.T) {
	build := func() Model {
		m := relModel(
			[]string{"a", "b", "e"},
			map[string][]string{
				"a": {"b"},
				"b": nil,
				"e": {"a"},
			},
			"a",
		)
		m = runKey(m, keyRune('d'))
		if m.state != statePackageDeleteConfirm {
			t.Fatalf("state = %v, want delete confirm", m.state)
		}
		return m
	}

	// y: a (ticked) + b (orphan) + e (broken dependent).
	m := build()
	if want := []string{"a", "b", "e"}; !reflect.DeepEqual(m.deleteSet(true), want) {
		t.Errorf("deleteSet(true) = %v, want %v", m.deleteSet(true), want)
	}
	mm, cmd := m.Update(keyRune('y'))
	if cmd == nil {
		t.Fatal("y produced no command")
	}
	if got := mm.(Model); got.state != statePackageDeleting {
		t.Errorf("y left state %v, want deleting", got.state)
	}

	// n: only a.
	m = build()
	if got := m.deleteSet(false); !reflect.DeepEqual(got, []string{"a"}) {
		t.Errorf("deleteSet(false) = %v, want [a]", got)
	}
	mm, cmd = m.Update(keyRune('n'))
	if cmd == nil {
		t.Fatal("n produced no command")
	}
	if got := mm.(Model); got.state != statePackageDeleting {
		t.Errorf("n left state %v, want deleting", got.state)
	}

	// esc: back to the list, relation cleared, nothing deleted.
	m = build()
	mm, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = mm.(Model)
	if cmd != nil {
		t.Error("esc produced a command")
	}
	if m.state != statePackageList {
		t.Errorf("esc left state %v, want the package list", m.state)
	}
	if !m.rel.Empty() {
		t.Errorf("esc kept relation %+v", m.rel)
	}
}

// TestLeavingPackageViewDropsGraph stops the dependency panel from pairing one
// environment's relations with another environment's package list.
func TestLeavingPackageViewDropsGraph(t *testing.T) {
	m := relModel([]string{"a", "b"}, map[string][]string{"a": {"b"}}, "a")
	m.deps, m.dependents = map[string][]string{"a": {"b"}}, map[string][]string{"b": {"a"}}

	for _, k := range []string{"esc", "q"} {
		got := m
		if k == "esc" {
			mm, _ := got.Update(tea.KeyMsg{Type: tea.KeyEsc})
			got = mm.(Model)
		} else {
			got = runKey(got, keyRune('q'))
		}
		if got.state != stateList {
			t.Fatalf("%q left state %v, want the environment list", k, got.state)
		}
		if got.deps != nil || got.dependents != nil {
			t.Errorf("%q kept the dependency graph", k)
		}
		if !got.rel.Empty() {
			t.Errorf("%q kept relation %+v", k, got.rel)
		}
	}
}

// TestDeleteConfirmFitsTerminalAndKeepsHints pins the invariant that matters
// most in this dialog: whatever the terminal size and however long the lists,
// the box fits and the key legend survives. Chrome clipping the hints off the
// bottom would leave the user in a dialog with no visible way out.
func TestDeleteConfirmFitsTerminalAndKeepsHints(t *testing.T) {
	// A graph big enough to overflow any terminal under test.
	packages := mkPkgs(120)
	edges := map[string][]string{}
	for i := 1; i < len(packages); i++ {
		edges[packages[i]] = []string{packages[0]}
	}

	for _, h := range []int{8, 10, 14, 16, 17, 18, 24, 40} {
		for _, w := range []int{50, 80, 120, 200} {
			for _, sel := range [][]string{
				{packages[0]},         // both groups: 119 breaks, 0 orphans
				{packages[1]},         // one break, no orphans
				{packages[0], "nope"}, // a selection that is not in the list
			} {
				m := relModel(packages, edges, sel...)
				m.width, m.height = w, h
				m = runKey(m, keyRune('d'))

				got := m.viewPackageDeleteConfirm()
				if n := lipgloss.Height(got); n > h {
					t.Fatalf("w=%d h=%d sel=%v: dialog is %d lines", w, h, sel, n)
				}
				// The legend must be present even when the lists had to go.
				for _, hint := range []string{"esc"} {
					if !strings.Contains(got, hint) {
						t.Fatalf("w=%d h=%d sel=%v: dialog lost the %q hint:\n%s", w, h, sel, hint, got)
					}
				}
			}
		}
	}
}

// TestNameLinesFoldsAndFits covers the list packing directly: lines stay within
// the width, the row cap is honoured, and what is dropped is counted.
func TestNameLinesFoldsAndFits(t *testing.T) {
	names := mkPkgs(40) // pkg0 .. pkg39, so ", " separated names all differ

	for _, width := range []int{10, 20, 41, 200} {
		for _, maxRows := range []int{1, 2, 5, 100} {
			lines := nameLines(names, width, maxRows)
			if len(lines) > maxRows {
				t.Errorf("width=%d rows=%d: got %d lines", width, maxRows, len(lines))
			}
			for _, l := range lines {
				if n := len([]rune(l)); n > width {
					t.Errorf("width=%d rows=%d: line %q is %d runes", width, maxRows, l, n)
				}
			}
			// The invariant that matters: nothing is dropped silently. If a name
			// is missing the output has to say how many more there are.
			joined := strings.Join(lines, ", ")
			dropped := 0
			for _, n := range names {
				if !strings.Contains(joined, n) {
					dropped++
				}
			}
			if dropped > 0 && !strings.Contains(joined, "more") {
				t.Errorf("width=%d rows=%d: %d names dropped with no count: %q",
					width, maxRows, dropped, joined)
			}
		}
	}

	if got := nameLines(nil, 40, 3); got != nil {
		t.Errorf("nameLines(nil) = %v, want nil", got)
	}
	if got := nameLines(names, 0, 3); got != nil {
		t.Errorf("nameLines(width=0) = %v, want nil", got)
	}
}

// TestDepSectionHonoursBudget covers the panel's row arithmetic: a section
// must never emit more lines than it was given, or lipgloss clips the section
// below it and the panel silently loses content.
func TestDepSectionHonoursBudget(t *testing.T) {
	for _, n := range []int{0, 1, 3, 40} {
		names := mkPkgs(n)
		for _, maxLines := range []int{1, 2, 3, 10} {
			for _, width := range []int{10, 30} {
				lines := depSection("needs", names, maxLines, width)
				if len(lines) > maxLines {
					t.Errorf("n=%d maxLines=%d width=%d: %d lines: %q",
						n, maxLines, width, len(lines), lines)
				}
				for _, l := range lines {
					if w := lipgloss.Width(l); w > width {
						t.Errorf("n=%d maxLines=%d width=%d: line is %d wide: %q",
							n, maxLines, width, w, l)
					}
				}
				// Whether or not the names fit, the header has to state how
				// many there are — that is what keeps a cut list honest.
				joined := strings.Join(lines, " ")
				if !strings.Contains(joined, fmt.Sprintf("(%d)", n)) {
					t.Errorf("n=%d maxLines=%d width=%d: header lost the count: %q",
						n, maxLines, width, joined)
				}
				// And anything cut short has to say so.
				shown := 0
				for _, name := range names {
					if strings.Contains(joined, name) {
						shown++
					}
				}
				if shown < n && maxLines > 1 && !strings.Contains(joined, "more") {
					t.Errorf("n=%d maxLines=%d width=%d: %d of %d shown with no count: %q",
						n, maxLines, width, shown, n, joined)
				}
			}
		}
	}
}

// TestActiveMarkerSitsBesideName covers the marker's position: it labels the
// environment, so it belongs against the name rather than out past the version.
// Its column is reserved on every row so the versions stay aligned.
func TestActiveMarkerSitsBesideName(t *testing.T) {
	m := New()
	m.width, m.height = 90, 20
	m.condaEnvs = []conda.Env{
		{Name: "base", PythonVer: "3.12.9", Active: true},
		{Name: "agent", PythonVer: "3.11.4"},
		{Name: "data", PythonVer: "3.10.1", Active: true},
	}
	m.rebuildItems()

	rows := map[string]string{}
	versions := map[string]int{}
	for _, line := range strings.Split(stripANSI(m.renderList(80, 20)), "\n") {
		for _, name := range []string{"base", "agent", "data"} {
			if !strings.Contains(line, name) {
				continue
			}
			rows[name] = line
			if i := strings.Index(line, "3.1"); i >= 0 {
				versions[name] = i
			}
		}
	}

	for _, name := range []string{"base", "agent", "data"} {
		if rows[name] == "" {
			t.Fatalf("no row rendered for %q", name)
		}
	}
	if !strings.Contains(rows["base"], "* base") {
		t.Errorf("base row does not lead with the marker: %q", rows["base"])
	}
	if !strings.Contains(rows["data"], "* data") {
		t.Errorf("data row does not lead with the marker: %q", rows["data"])
	}
	if strings.Contains(rows["agent"], "*") {
		t.Errorf("inactive agent row carries a marker: %q", rows["agent"])
	}
	if versions["base"] != versions["agent"] || versions["base"] != versions["data"] {
		t.Errorf("versions are not aligned: base=%d agent=%d data=%d",
			versions["base"], versions["agent"], versions["data"])
	}
}

// TestPackageCursorWraps matches the package list to the environment list,
// where the arrows wrap rather than stopping at the ends.
func TestPackageCursorWraps(t *testing.T) {
	up := tea.KeyMsg{Type: tea.KeyUp}
	down := tea.KeyMsg{Type: tea.KeyDown}

	m := pkgModel(4)
	m.pkgCursor = 0
	if got := runKey(m, up).pkgCursor; got != 3 {
		t.Errorf("up from the first package: cursor %d, want 3", got)
	}

	m.pkgCursor = 3
	if got := runKey(m, down).pkgCursor; got != 0 {
		t.Errorf("down from the last package: cursor %d, want 0", got)
	}

	// A single package wraps onto itself rather than moving out of range.
	single := pkgModel(1)
	if got := runKey(single, up).pkgCursor; got != 0 {
		t.Errorf("up with one package: cursor %d, want 0", got)
	}
	if got := runKey(single, down).pkgCursor; got != 0 {
		t.Errorf("down with one package: cursor %d, want 0", got)
	}

	// An empty list must not produce a negative or out-of-range cursor.
	empty := pkgModel(0)
	for _, k := range []tea.KeyMsg{up, down} {
		if got := runKey(empty, k).pkgCursor; got != 0 {
			t.Errorf("empty list, key %v: cursor %d, want 0", k, got)
		}
	}
}

// ── filtering ─────────────────────────────────────────────────────────────────

// typeFilter presses f and then types s, leaving the box open.
func typeFilter(t *testing.T, m Model, s string) Model {
	t.Helper()
	m = runKey(m, keyRune('f'))
	if !m.filtering {
		t.Fatal("f did not open the filter box")
	}
	for _, r := range s {
		m = runKey(m, keyRune(r))
	}
	return m
}

// envModel builds an environment list with a conda and a uv section, the shape
// rebuildItems produces from a real scan.
func envModel(condaNames, uvNames []string) Model {
	// Built through New() like the real model, so the embedded text inputs are
	// constructed rather than zero.
	m := New()
	m.width = 100
	m.height = 24
	for _, n := range condaNames {
		m.condaEnvs = append(m.condaEnvs, conda.Env{Name: n})
	}
	for _, n := range uvNames {
		m.uvEnvs = append(m.uvEnvs, uv.Env{Name: n})
	}
	m.rebuildItems()
	m.skipHeaders()
	return m
}

// listShape renders a list as names, with "=" standing in for a section header,
// so a test can assert on what is on screen without counting indices.
func listShape(list []listItem) []string {
	out := make([]string, 0, len(list))
	for _, it := range list {
		if it.kind == kindHeader {
			out = append(out, "=")
		} else {
			out = append(out, it.label)
		}
	}
	return out
}

func TestMatchFilter(t *testing.T) {
	cases := []struct {
		s, needle string
		want      bool
	}{
		{"requests", "quest", true}, // not anchored to either end
		{"requests", "REQ", true},   // case-insensitive, both directions
		{"Requests", "que", true},
		{"libgcc-ng", "gcc", true}, // the meaning is in the middle
		{"requests", "flask", false},
		{"requests", "", true}, // no needle matches everything
		{"", "", true},
		{"", "x", false},
	}
	for _, c := range cases {
		if got := matchFilter(c.s, c.needle); got != c.want {
			t.Errorf("matchFilter(%q, %q) = %v, want %v", c.s, c.needle, got, c.want)
		}
	}
}

// TestEnvFilterKeepsOnlyMatchingSections covers the header rule: a section keeps
// its title only when one of its own environments matched, so a filter cannot
// leave a heading sitting above nothing.
func TestEnvFilterKeepsOnlyMatchingSections(t *testing.T) {
	newModel := func() Model {
		return envModel([]string{"base", "torch", "myenv"}, []string{"venv-a", "torch-uv"})
	}

	cases := []struct {
		filter string
		want   []string
	}{
		// Both sections survive, in their original order.
		{"torch", []string{"=", "torch", "=", "torch-uv"}},
		// Only conda matches, so the uv heading must not appear.
		{"base", []string{"=", "base"}},
		// Only uv matches: the conda heading must not appear instead.
		{"venv", []string{"=", "venv-a"}},
		{"zzz", []string{}},
		// Case-insensitive, as the box promises.
		{"TORCH", []string{"=", "torch", "=", "torch-uv"}},
	}

	for _, c := range cases {
		m := typeFilter(t, newModel(), c.filter)
		if got := listShape(m.envList()); !reflect.DeepEqual(got, c.want) {
			t.Errorf("filter %q: list = %v, want %v", c.filter, got, c.want)
		}
	}
}

// TestEnvFilterReanchorsCursor checks that the highlighted environment stays
// highlighted while the filter changes, instead of the cursor sliding onto
// whatever happens to land at the same row number.
func TestEnvFilterReanchorsCursor(t *testing.T) {
	m := envModel([]string{"base", "torch", "myenv"}, nil)
	// items: [= conda, base, torch, myenv]
	m.cursor = 3 // myenv
	if got := m.selectedItem(); got == nil || got.label != "myenv" {
		t.Fatalf("setup: cursor is on %v, want myenv", got)
	}

	// "e" matches base and myenv; myenv was at 3 and is now at 2.
	m = typeFilter(t, m, "e")
	if got := listShape(m.envList()); !reflect.DeepEqual(got, []string{"=", "base", "myenv"}) {
		t.Fatalf("list = %v", got)
	}
	if sel := m.selectedItem(); sel == nil || sel.label != "myenv" {
		t.Errorf("after filtering, cursor is on %v, want myenv", sel)
	}

	// Narrowing past the anchor drops it, and the cursor falls back to the top.
	m = runKey(m, keyRune('z'))
	if sel := m.selectedItem(); sel != nil {
		t.Errorf("with no matches the cursor should select nothing, got %v", sel.label)
	}

	// esc clears, and the cursor returns to where that environment now sits.
	m = runKey(m, tea.KeyMsg{Type: tea.KeyEsc})
	if got := m.cursor; got != 1 {
		t.Errorf("after clearing, cursor = %d, want 1 (base)", got)
	}
	if sel := m.selectedItem(); sel == nil || sel.label != "base" {
		t.Errorf("after clearing, cursor is on %v, want base", sel)
	}
}

// TestPkgFilterKeepsTicksOnSourceIndex is the invariant the whole design rests
// on: the selection is keyed by the package's own index, so changing the filter
// cannot lose a tick or move it onto a different package.
func TestPkgFilterKeepsTicksOnSourceIndex(t *testing.T) {
	m := pkgModel(20)
	m.pkgCursor = 7
	m = runKey(m, keyRune(' '))
	if !m.pkgSelected[7] {
		t.Fatal("setup: pkg7 was not ticked")
	}

	// pkg1 and pkg10..pkg19 match; pkg7 does not.
	m = typeFilter(t, m, "pkg1")
	if got := m.pkgCount(); got != 11 {
		t.Fatalf("visible rows = %d, want 11", got)
	}
	if !m.pkgSelected[7] {
		t.Error("filtering dropped a tick that was off screen")
	}

	// The cursor was on pkg7, which is gone; it falls back to the first match —
	// and a tick there must mark pkg1, not source row 0 of an unfiltered list.
	if orig, ok := m.pkgAt(m.pkgCursor); !ok || m.packages[orig] != "pkg1" {
		t.Errorf("cursor maps to %q, want pkg1", m.packages[orig])
	}
	m = runKey(m, tea.KeyMsg{Type: tea.KeyEnter}) // close the box, keep the filter
	m = runKey(m, keyRune(' '))
	if !m.pkgSelected[1] {
		t.Error("ticking the first match did not tick pkg1")
	}
	if m.pkgSelected[0] {
		t.Error("ticking the first match ticked pkg0 instead")
	}
}

// TestPackageAllUnderFilter covers the decision that "all" means every row on
// screen: it must neither reach packages the filter hides nor drop a tick the
// user made before typing.
func TestPackageAllUnderFilter(t *testing.T) {
	m := pkgModel(20)
	m.pkgCursor = 5
	m = runKey(m, keyRune(' ')) // tick pkg5, which "pkg1" will hide
	m = typeFilter(t, m, "pkg1")
	m = runKey(m, tea.KeyMsg{Type: tea.KeyEnter}) // close the box, keep the filter

	m = runKey(m, keyRune('a'))
	if got := len(m.pkgSelected); got != 12 {
		t.Fatalf("after a: %d ticked, want 12 (11 matches + the hidden pkg5)", got)
	}
	for i := range m.pkgSelected {
		if i != 5 && !matchFilter(m.packages[i], "pkg1") {
			t.Errorf("a ticked %q, which the filter does not match", m.packages[i])
		}
	}

	// Pressing it again clears the visible rows only; the hidden tick stays.
	m = runKey(m, keyRune('a'))
	if len(m.pkgSelected) != 1 || !m.pkgSelected[5] {
		t.Errorf("second a left %v, want only the hidden pkg5", m.pkgSelected)
	}
}

// TestFilterBoxSwallowsKeys is the guarantee that typing a filter can never
// delete anything: every key belongs to the box while it has focus.
func TestFilterBoxSwallowsKeys(t *testing.T) {
	m := typeFilter(t, pkgModel(20), "pkg1")

	// d would open the delete dialog, q would leave the view, space and a would
	// tick packages, and j/k would walk the cursor — all of them are text here.
	for _, r := range "d qjka" {
		m = runKey(m, keyRune(r))
	}

	if m.state != statePackageList {
		t.Errorf("state = %v, want the package list", m.state)
	}
	if m.pkgCursor != 0 {
		t.Errorf("j/k moved the cursor to %d while typing", m.pkgCursor)
	}
	if len(m.pkgSelected) != 0 {
		t.Errorf("space or a ticked %v while typing", m.pkgSelected)
	}
	if got := m.filterText(); got != "pkg1d qjka" {
		t.Errorf("filter = %q, want %q — a key escaped the box", got, "pkg1d qjka")
	}
}

// TestFilterArrowsMoveTheList checks the escape hatch: matches can be stepped
// through without closing the box.
func TestFilterArrowsMoveTheList(t *testing.T) {
	down := tea.KeyMsg{Type: tea.KeyDown}

	m := typeFilter(t, pkgModel(20), "pkg1")
	if m.pkgCursor != 0 {
		t.Fatalf("setup: cursor = %d, want 0", m.pkgCursor)
	}
	m = runKey(m, down)
	if m.pkgCursor != 1 {
		t.Errorf("down in the box: cursor = %d, want 1", m.pkgCursor)
	}
	// Wrapping still holds inside a filtered list.
	m.pkgCursor = m.pkgCount() - 1
	if got := runKey(m, down).pkgCursor; got != 0 {
		t.Errorf("down from the last match: cursor = %d, want 0", got)
	}

	// The environment list moves the same way and keeps loading details.
	e := envModel([]string{"base", "torch", "myenv"}, nil)
	e = typeFilter(t, e, "e")
	before := e.cursor
	e = runKey(e, down)
	if e.cursor == before {
		t.Error("down in the environment filter box did not move the cursor")
	}
}

// TestFilterWithNoMatchesStaysSafe drives the empty-result case, where the
// cursor has no row to sit on at all.
func TestFilterWithNoMatchesStaysSafe(t *testing.T) {
	m := typeFilter(t, envModel([]string{"base"}, []string{"venv"}), "zzz")
	if len(m.envList()) != 0 {
		t.Fatalf("visible rows = %d, want 0", len(m.envList()))
	}
	for _, k := range []tea.KeyMsg{{Type: tea.KeyUp}, {Type: tea.KeyDown}} {
		m = runKey(m, k)
		if m.cursor != 0 {
			t.Errorf("cursor = %d with nothing to select, want 0", m.cursor)
		}
	}
	if m.selectedItem() != nil {
		t.Error("selectedItem returned a row when the list is empty")
	}
	// Rendering an empty filtered panel must say so rather than look like a
	// failed load.
	if view := stripANSI(m.View()); !strings.Contains(view, "no matches") {
		t.Error("an empty filtered list does not report that nothing matched")
	}

	p := typeFilter(t, pkgModel(10), "zzz")
	for _, k := range []tea.KeyMsg{{Type: tea.KeyUp}, {Type: tea.KeyDown}} {
		p = runKey(p, k)
		if p.pkgCursor != 0 {
			t.Errorf("package cursor = %d with nothing to select, want 0", p.pkgCursor)
		}
	}
	// A tick and a select-all on an empty view must be no-ops, not panics.
	if got := runKey(runKey(p, keyRune(' ')), keyRune('a')); len(got.pkgSelected) != 0 {
		t.Errorf("keys on an empty filtered list selected %v", got.pkgSelected)
	}
	// Both package layouts have to say it: the two-column one (wide terminal)
	// and the single centred box (narrow), which are separate render paths.
	if view := stripANSI(p.View()); !strings.Contains(view, "no matches") {
		t.Errorf("the two-column package list does not report that nothing matched:\n%s", view)
	}
	p.width = 60
	if view := stripANSI(p.View()); !strings.Contains(view, "No matching packages.") {
		t.Errorf("the single-column package box does not report that nothing matched:\n%s", view)
	}
}

// TestFilterStatusRowStaysOneLine keeps the box from growing the layout: it
// replaces the key hints, so the list above it keeps its height and position.
func TestFilterStatusRowStaysOneLine(t *testing.T) {
	// A status bar that wraps to two rows steals a line from the list above it,
	// and the whole layout stops matching the terminal. Every width has to hold
	// one row, filtered or not.
	for _, width := range []int{20, 40, 60, 100, 200} {
		m := envModel([]string{"base"}, nil)
		m.width = width

		check := func(what string, got int) {
			t.Helper()
			if got != 1 {
				t.Errorf("width %d, %s: status row is %d lines tall, want 1", width, what, got)
			}
		}
		check("plain", lipgloss.Height(m.renderStatusBar()))

		// A long filter text and a long status message, each of which has to be
		// trimmed rather than wrapped.
		m = runKey(m, keyRune('f'))
		for _, r := range strings.Repeat("x", 200) {
			m = runKey(m, keyRune(r))
		}
		check("filtering", lipgloss.Height(m.renderStatusBar()))
		check("filtering, whole view", lipgloss.Height(m.View())-m.height+1)

		m = runKey(m, tea.KeyMsg{Type: tea.KeyEnter})
		check("filter applied", lipgloss.Height(m.renderStatusBar()))

		m.statusMsg = strings.Repeat("boom ", 40)
		m.statusErr = true
		check("long error", lipgloss.Height(m.renderStatusBar()))
		if got := lipgloss.Height(m.View()); got != m.height {
			t.Errorf("width %d: view is %d lines tall, want %d", width, got, m.height)
		}
	}
}

// TestStatusHintsShedWholePairs checks the other half of the one-row rule: the
// legend has to give ground a pair at a time, rather than vanishing the moment
// the row is tight or being cut mid-escape.
func TestStatusHintsShedWholePairs(t *testing.T) {
	m := envModel([]string{"torch-a", "other"}, nil)

	// Wide enough for everything: the filter note does not cost any hints.
	m.width = 140
	full := stripANSI(m.renderStatusBar())
	m = typeFilter(t, m, "torch")
	m = runKey(m, tea.KeyMsg{Type: tea.KeyEnter})
	if got := stripANSI(m.renderStatusBar()); !strings.Contains(got, "q quit") {
		t.Errorf("a filter note crowded out the legend at 140 columns: %q\nwas: %q", got, full)
	}

	// Narrow: hints go from the end, whole, and never mid-word.
	m.width = 60
	got := stripANSI(m.renderStatusBar())
	for _, want := range []string{"↵ activate", "p packages", `filter: "torch"`} {
		if !strings.Contains(got, want) {
			t.Errorf("at 60 columns the row lost %q: %q", want, got)
		}
	}
	if strings.Contains(got, "q qui") && !strings.Contains(got, "q quit") {
		t.Errorf("a hint was cut mid-word: %q", got)
	}
}

// TestFilterLeavesWithTheList checks that a filter typed for one list does not
// follow the user into the next one, where it would hide rows for no visible
// reason.
func TestFilterLeavesWithTheList(t *testing.T) {
	m := envModel([]string{"base", "torch"}, nil)
	m = typeFilter(t, m, "torch")
	m = runKey(m, tea.KeyMsg{Type: tea.KeyEnter}) // keep it, close the box

	// Enter leaves the environment filtered, and the status bar says so.
	if got := m.filterText(); got != "torch" {
		t.Fatalf("enter dropped the filter: %q", got)
	}
	if view := stripANSI(m.renderStatusBar()); !strings.Contains(view, `filter: "torch"`) {
		t.Errorf("the status bar does not mention the active filter: %q", view)
	}

	// The count is of environments, not of rows: the two surviving section
	// headers are not something the user can pick.
	two := envModel([]string{"torch-a", "other"}, []string{"torch-b", "else"})
	two.width = 200
	two = typeFilter(t, two, "torch")
	two = runKey(two, tea.KeyMsg{Type: tea.KeyEnter})
	if view := stripANSI(two.renderStatusBar()); !strings.Contains(view, "2 shown") {
		t.Errorf("the match count counts headers: %q", view)
	}

	// Opening a package list for that environment starts clean.
	m.cursor = 1
	m = runKey(m, keyRune('p'))
	if m.state != statePackageList {
		t.Fatalf("state = %v, want the package list", m.state)
	}
	if got := m.filterText(); got != "" {
		t.Errorf("the environment filter followed into the package list: %q", got)
	}

	// And leaving the package list clears the one typed there.
	m.packages = mkPkgs(20)
	m.pkgLoaded = true
	m = typeFilter(t, m, "pkg1")
	m = runKey(m, tea.KeyMsg{Type: tea.KeyEsc})
	if got := m.filterText(); got != "" {
		t.Errorf("the package filter survived the return: %q", got)
	}
}

// TestPackageCountLabel covers the list title: it reports the subset against the
// total only when the two differ.
func TestPackageCountLabel(t *testing.T) {
	cases := []struct {
		matched, total int
		want           string
	}{
		{631, 631, "631 packages"},
		{3, 631, "3/631 packages"},
		{0, 631, "0/631 packages"},
	}
	for _, c := range cases {
		if got := countLabel(c.matched, c.total, "packages"); got != c.want {
			t.Errorf("countLabel(%d, %d) = %q, want %q", c.matched, c.total, got, c.want)
		}
	}
}

// stripANSI removes SGR sequences so layout can be measured in plain columns.
var ansiRE = regexp.MustCompile("\x1b\\[[0-9;]*m")

func stripANSI(s string) string { return ansiRE.ReplaceAllString(s, "") }

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

func dedupe(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

func keyRune(r rune) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
}

func mkPkgs(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf("pkg%d", i)
	}
	return out
}

func TestFormatAge(t *testing.T) {
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.Local)
	cases := []struct {
		ago  time.Duration
		want string
	}{
		{-time.Hour, "just now"}, // file dated ahead of the clock
		{30 * time.Second, "just now"},
		{5 * time.Minute, "5m ago"},
		{3 * time.Hour, "3h ago"},
		{29 * 24 * time.Hour, "29d ago"},
		{95 * 24 * time.Hour, "3mo ago"},
		{800 * 24 * time.Hour, "2y ago"},
	}
	for _, c := range cases {
		created := now.Add(-c.ago)
		want := created.Format("2006-01-02") + " (" + c.want + ")"
		if got := formatAge(created, now); got != want {
			t.Errorf("formatAge(now-%v) = %q, want %q", c.ago, got, want)
		}
	}
	if got := formatAge(time.Time{}, now); got != "unknown" {
		t.Errorf("formatAge(zero) = %q, want %q", got, "unknown")
	}
}
