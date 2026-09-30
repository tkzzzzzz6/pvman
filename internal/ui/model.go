package ui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/tkzzzzzz6/pvman/internal/conda"
	"github.com/tkzzzzzz6/pvman/internal/uv"
)

type appState int

const (
	stateList appState = iota
	stateLoadingDetails
	stateCreate
	stateCreating
	stateDeleteConfirm
	stateDeleting
	statePackageList
	statePackageDeleteConfirm
	statePackageDeleting
)

type itemKind int

const (
	kindHeader itemKind = iota
	kindEnv
)

type listItem struct {
	kind    itemKind
	label   string // header label or env name
	envType string // "conda" or "uv"
	idx     int    // index into condaEnvs or uvEnvs
}

// messages
type envsLoadedMsg struct {
	condaEnvs []conda.Env
	uvEnvs    []uv.Env
	err       error
}

type detailsLoadedMsg struct {
	envType string
	idx     int
	cenv    conda.Env
	uenv    uv.Env
}

type envCreatedMsg struct{ err error }
type envDeletedMsg struct{ err error }
type activationFinishedMsg struct{ err error }
type packagesLoadedMsg struct {
	envType string
	idx     int
	pkgs    []string
	// deps[p] is what p needs, dependents[p] is what needs p. Both are keyed by
	// the names in pkgs. A nil map means the graph could not be built.
	deps       map[string][]string
	dependents map[string][]string
	err        error
}
type packageDeletedMsg struct {
	count int
	err   error
}

type Model struct {
	state     appState
	condaEnvs []conda.Env
	uvEnvs    []uv.Env
	items     []listItem
	cursor    int
	width     int
	height    int
	cwd       string

	spinner spinner.Model

	// create form
	createInputs [2]textinput.Model // 0=name, 1=python version
	createFocus  int

	// status message
	statusMsg string
	statusErr bool

	// delete confirm target
	deleteTarget listItem

	// package management view
	pkgEnvType  string
	pkgIdx      int
	packages    []string
	pkgCursor   int
	pkgSelected map[int]bool
	pkgLoaded   bool

	// dependency graph of the environment being browsed
	deps       map[string][]string
	dependents map[string][]string

	// how the current selection connects to the rest of the environment;
	// computed when the delete confirmation opens so the dialog and the delete
	// command agree on the same set.
	rel relation

	// filter box, shared by the environment and package lists
	filter    textinput.Model
	filtering bool // the box has focus, so it swallows the keys

	// The filter shows a subset of the source list. Both cursors index the
	// subset, never the source, so a filter change cannot leave a cursor
	// pointing at a row that is no longer on screen. A nil slice means "no
	// filter", which every reader treats as the whole list.
	filteredItems []listItem
	filteredPkgs  []int // indexes into packages
}

func New() Model {
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = spinnerStyle

	nameInput := textinput.New()
	nameInput.Placeholder = "env-name"
	nameInput.Focus()
	nameInput.CharLimit = 64

	verInput := textinput.New()
	verInput.Placeholder = "3.12  (leave blank for default)"
	verInput.CharLimit = 16

	filterInput := textinput.New()
	filterInput.Prompt = "filter: "
	filterInput.Placeholder = "type to filter"
	filterInput.CharLimit = 64

	cwd, _ := os.Getwd()

	return Model{
		state:        stateList,
		spinner:      sp,
		createInputs: [2]textinput.Model{nameInput, verInput},
		filter:       filterInput,
		cwd:          cwd,
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, loadEnvsCmd(m.cwd))
}

// ── commands ──────────────────────────────────────────────────────────────────

func loadEnvsCmd(cwd string) tea.Cmd {
	return func() tea.Msg {
		cenvs, err := conda.ListEnvs()
		if err != nil {
			cenvs = nil
		}
		uenvs, _ := uv.ScanDir(cwd)
		return envsLoadedMsg{condaEnvs: cenvs, uvEnvs: uenvs, err: err}
	}
}

func loadDetailsCmd(envType string, idx int, cenv conda.Env, uenv uv.Env) tea.Cmd {
	return func() tea.Msg {
		if envType == "conda" {
			conda.LoadDetails(&cenv)
			return detailsLoadedMsg{envType: "conda", idx: idx, cenv: cenv}
		}
		uv.LoadDetails(&uenv)
		return detailsLoadedMsg{envType: "uv", idx: idx, uenv: uenv}
	}
}

func createEnvCmd(cwd, name, ver string) tea.Cmd {
	return func() tea.Msg {
		return envCreatedMsg{err: uv.CreateEnv(cwd, name, ver)}
	}
}

func deleteEnvCmd(item listItem, cenvs []conda.Env, uenvs []uv.Env) tea.Cmd {
	return func() tea.Msg {
		// The lists are captured at confirm time and can be replaced by a
		// refresh before the user answers, so re-check the index here.
		var err error
		switch {
		case item.envType == "conda" && item.idx < len(cenvs):
			err = conda.DeleteEnv(cenvs[item.idx])
		case item.envType == "uv" && item.idx < len(uenvs):
			err = uv.DeleteEnv(uenvs[item.idx])
		default:
			err = fmt.Errorf("environment is no longer in the list")
		}
		return envDeletedMsg{err: err}
	}
}

func loadPackagesCmd(envType string, idx int, cenvs []conda.Env, uenvs []uv.Env) tea.Cmd {
	return func() tea.Msg {
		msg := packagesLoadedMsg{envType: envType, idx: idx}
		switch {
		case envType == "conda" && idx < len(cenvs):
			msg.pkgs, msg.err = conda.ListPackages(cenvs[idx])
			if msg.err == nil {
				// The graph is a nicety: if it cannot be built the list still
				// works, it just shows no relations.
				msg.deps, msg.dependents, _ = conda.Dependencies(cenvs[idx])
			}
		case envType == "uv" && idx < len(uenvs):
			msg.pkgs, msg.err = uv.ListPackages(uenvs[idx])
			if msg.err == nil {
				msg.deps, msg.dependents, _ = uv.Dependencies(uenvs[idx])
			}
		}
		return msg
	}
}

// deletePackagesCmd removes exactly the named packages. The caller decides
// whether that set includes the related packages, so there is a single place
// where the user's choice is applied.
func deletePackagesCmd(envType string, idx int, cenvs []conda.Env, uenvs []uv.Env, names []string) tea.Cmd {
	return func() tea.Msg {
		if len(names) == 0 {
			return packageDeletedMsg{err: fmt.Errorf("no packages selected")}
		}
		var (
			count int
			err   error
		)
		switch {
		case envType == "conda" && idx < len(cenvs):
			count, err = conda.RemovePackage(cenvs[idx], names...)
		case envType == "uv" && idx < len(uenvs):
			count, err = uv.RemovePackage(uenvs[idx], names...)
		default:
			err = fmt.Errorf("environment is no longer in the list")
		}
		return packageDeletedMsg{count: count, err: err}
	}
}

// selectedNames returns the ticked package names in list order.
func (m Model) selectedNames() []string {
	names := make([]string, 0, len(m.pkgSelected))
	for i, name := range m.packages {
		if m.pkgSelected[i] {
			names = append(names, name)
		}
	}
	return names
}

// relation describes how the current selection connects to the rest of the
// environment, in both directions.
type relation struct {
	// Breaks holds the transitive dependents of the selection: packages that
	// would be left with a requirement nothing satisfies.
	Breaks []string
	// Orphans holds the selection's own dependencies that nothing else in the
	// environment needs, so they would be left unused.
	Orphans []string
}

// Empty reports whether the selection is connected to nothing else.
func (r relation) Empty() bool { return len(r.Breaks) == 0 && len(r.Orphans) == 0 }

// All returns every package the relation names, for use as the delete set.
func (r relation) All() []string {
	out := make([]string, 0, len(r.Breaks)+len(r.Orphans))
	seen := make(map[string]bool, cap(out))
	for _, group := range [][]string{r.Breaks, r.Orphans} {
		for _, n := range group {
			if !seen[n] {
				seen[n] = true
				out = append(out, n)
			}
		}
	}
	sort.Strings(out)
	return out
}

// deleteSet is the package list the confirmation dialog would submit: what was
// ticked, plus — when the user asked for them — every related package.
func (m Model) deleteSet(includeRelated bool) []string {
	names := m.selectedNames()
	if includeRelated {
		names = append(names, m.rel.All()...)
	}
	return names
}

// selectionRelation analyses the ticked packages against the dependency graph.
func (m Model) selectionRelation() relation {
	selected := make(map[string]bool, len(m.pkgSelected))
	for i, name := range m.packages {
		if m.pkgSelected[i] {
			selected[name] = true
		}
	}
	if len(selected) == 0 {
		return relation{}
	}

	installed := make(map[string]bool, len(m.packages))
	for _, name := range m.packages {
		installed[name] = true
	}

	// Walk the reverse edges transitively: whatever needs something in the
	// selection, and whatever needs that, and so on.
	breaks := make(map[string]bool)
	queue := make([]string, 0, len(selected))
	for n := range selected {
		queue = append(queue, n)
	}
	for len(queue) > 0 {
		n := queue[0]
		queue = queue[1:]
		for _, d := range m.dependents[n] {
			// A dependent that is going away anyway does not break, and the
			// visited set also stops dependency cycles from looping forever.
			if selected[d] || breaks[d] || !installed[d] {
				continue
			}
			breaks[d] = true
			queue = append(queue, d)
		}
	}

	// A package is orphaned when the selection needs it and nothing that stays
	// behind does. Removing an orphan can orphan its own dependencies, so this
	// runs to a fixpoint. A package the selection does not reach is left alone:
	// it is something the user installed deliberately, not leftover.
	orphans := make(map[string]bool)
	for changed := true; changed; {
		changed = false
		for _, pkg := range m.packages {
			if selected[pkg] || orphans[pkg] {
				continue
			}
			reachedByDoomed, allDependentsDoomed := false, true
			for _, d := range m.dependents[pkg] {
				if !installed[d] {
					continue
				}
				if selected[d] || orphans[d] {
					reachedByDoomed = true
				} else {
					allDependentsDoomed = false
				}
			}
			if reachedByDoomed && allDependentsDoomed {
				orphans[pkg] = true
				changed = true
			}
		}
	}

	return relation{Breaks: sortedKeys(breaks), Orphans: sortedKeys(orphans)}
}

func sortedKeys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for n := range set {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// ── update ────────────────────────────────────────────────────────────────────

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case envsLoadedMsg:
		m.condaEnvs = msg.condaEnvs
		m.uvEnvs = msg.uvEnvs
		m.rebuildItems()
		// The items this rebuild produced are new to the filter, so the visible
		// list has to be derived from them again — both to match the new arrivals
		// and to re-anchor the cursor onto whatever it was pointing at.
		m.applyEnvFilter()
		// A confirmation dialog holds an index into the old lists. Cancel it
		// rather than let "y" act on an environment that may be gone.
		if m.state == stateDeleteConfirm {
			m.state = stateList
			m.statusMsg = ""
		}
		return m, m.triggerDetailLoad()

	case detailsLoadedMsg:
		// The environment may have disappeared while its details were loading.
		if msg.envType == "conda" && msg.idx < len(m.condaEnvs) {
			m.condaEnvs[msg.idx] = msg.cenv
		} else if msg.envType == "uv" && msg.idx < len(m.uvEnvs) {
			m.uvEnvs[msg.idx] = msg.uenv
		}
		if m.state == stateLoadingDetails {
			m.state = stateList
		}
		return m, nil

	case envCreatedMsg:
		if msg.err != nil {
			m.statusMsg = "Error: " + msg.err.Error()
			m.statusErr = true
		} else {
			m.statusMsg = "Environment created."
			m.statusErr = false
		}
		m.state = stateList
		return m, loadEnvsCmd(m.cwd)

	case envDeletedMsg:
		if msg.err != nil {
			m.statusMsg = "Error: " + msg.err.Error()
			m.statusErr = true
		} else {
			m.statusMsg = "Environment deleted."
			m.statusErr = false
		}
		m.state = stateList
		return m, loadEnvsCmd(m.cwd)

	case activationFinishedMsg:
		if msg.err != nil {
			m.statusMsg = "Activation failed: " + msg.err.Error()
			m.statusErr = true
		} else {
			m.statusMsg = ""
		}
		return m, nil

	case packagesLoadedMsg:
		if m.state == statePackageList && msg.envType == m.pkgEnvType && msg.idx == m.pkgIdx {
			if msg.err != nil {
				m.statusMsg = "Failed to load packages: " + msg.err.Error()
				m.statusErr = true
				m.packages = nil
			} else {
				m.packages = msg.pkgs
				m.statusMsg = ""
			}
			// The list may have shrunk since the last render (a delete, or a
			// refresh after one), so drop cursor/selection entries that now
			// point past the end. A filter in force has to be re-derived from the
			// new list too, or it would keep showing rows that are now gone.
			m.applyPkgFilter()
			m.clampPkgCursor()
			m.pkgLoaded = true
			// The graph is keyed by these very package names, so it is stored
			// only alongside the list it was built from: a load resolving for an
			// environment the user has already left must not repoint the panel.
			m.deps, m.dependents = msg.deps, msg.dependents
		}
		return m, nil

	case packageDeletedMsg:
		if msg.err != nil {
			m.statusMsg = "Failed to delete packages: " + msg.err.Error()
			m.statusErr = true
			m.state = statePackageList
		} else {
			// conda's solver removes more than it was asked to, so report what
			// actually happened rather than what was requested.
			m.statusMsg = fmt.Sprintf("Deleted %d package(s).", msg.count)
			m.statusErr = false
			m.pkgSelected = make(map[int]bool)
			m.pkgCursor = 0
			m.pkgLoaded = false
			m.rel = relation{}
			m.state = statePackageList
			// refresh packages and env details
			if m.pkgEnvType == "conda" && m.pkgIdx < len(m.condaEnvs) {
				m.condaEnvs[m.pkgIdx].Loaded = false
				return m, tea.Batch(
					loadPackagesCmd(m.pkgEnvType, m.pkgIdx, m.condaEnvs, m.uvEnvs),
					loadDetailsCmd("conda", m.pkgIdx, m.condaEnvs[m.pkgIdx], uv.Env{}),
				)
			}
			if m.pkgEnvType == "uv" && m.pkgIdx < len(m.uvEnvs) {
				m.uvEnvs[m.pkgIdx].Loaded = false
				return m, tea.Batch(
					loadPackagesCmd(m.pkgEnvType, m.pkgIdx, m.condaEnvs, m.uvEnvs),
					loadDetailsCmd("uv", m.pkgIdx, conda.Env{}, m.uvEnvs[m.pkgIdx]),
				)
			}
			return m, nil
		}
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	return m, nil
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// While the box has focus it owns the keyboard, including the keys that
	// would otherwise quit, navigate or delete. Nothing else may run on a
	// keystroke the user meant as text.
	if m.filtering {
		return m.handleFilterKey(msg)
	}

	switch m.state {

	case stateDeleteConfirm:
		switch {
		case key.Matches(msg, keys.Confirm):
			m.state = stateDeleting
			return m, deleteEnvCmd(m.deleteTarget, m.condaEnvs, m.uvEnvs)
		case key.Matches(msg, keys.Cancel) || msg.String() == "n":
			m.state = stateList
		}
		return m, nil

	case stateCreate:
		switch {
		case key.Matches(msg, keys.Cancel):
			m.state = stateList
			m.resetCreateForm()
			return m, nil

		case key.Matches(msg, keys.Tab):
			m.createFocus = (m.createFocus + 1) % 2
			m.createInputs[0].Blur()
			m.createInputs[1].Blur()
			m.createInputs[m.createFocus].Focus()
			return m, nil

		case msg.String() == "enter":
			name := strings.TrimSpace(m.createInputs[0].Value())
			if name == "" {
				m.statusMsg = "Name cannot be empty."
				m.statusErr = true
				return m, nil
			}
			ver := strings.TrimSpace(m.createInputs[1].Value())
			m.state = stateCreating
			return m, createEnvCmd(m.cwd, name, ver)
		}

		var cmd tea.Cmd
		m.createInputs[m.createFocus], cmd = m.createInputs[m.createFocus].Update(msg)
		return m, cmd

	case statePackageList:
		// The list can shrink under us (a delete finished, a refresh returned
		// fewer packages), so re-anchor the cursor and drop dead selections
		// before any key touches them.
		m.clampPkgCursor()
		switch {
		case msg.String() == "ctrl+c":
			return m, tea.Quit

		case key.Matches(msg, keys.Filter):
			m.openFilter()
			return m, nil

		case key.Matches(msg, keys.Cancel), key.Matches(msg, keys.Packages), msg.String() == "q":
			m.state = stateList
			m.packages = nil
			m.pkgSelected = nil
			// The graph belongs to the environment being left; keeping it would
			// let the next environment's list show stale relations. The filter is
			// the same kind of leftover: it was typed against a list of packages
			// this environment does not share.
			m.deps, m.dependents = nil, nil
			m.rel = relation{}
			m.clearFilter()
			m.pkgLoaded = false
			return m, nil

		case key.Matches(msg, keys.Up):
			// Wrapping matches the environment list, and takes the long way
			// round from the top of a several-hundred package list to the
			// bottom, which is quicker than holding the key down.
			if n := m.pkgCount(); n > 0 {
				m.pkgCursor = wrap(m.pkgCursor-1, n)
			}
			return m, nil

		case key.Matches(msg, keys.Down):
			if n := m.pkgCount(); n > 0 {
				m.pkgCursor = wrap(m.pkgCursor+1, n)
			}
			return m, nil

		case key.Matches(msg, keys.Toggle):
			// Without these guards an empty or not-yet-loaded list would record
			// a selection for a package that does not exist, and `d` would then
			// offer to delete it.
			if !m.pkgLoaded || m.pkgCount() == 0 {
				return m, nil
			}
			// The cursor counts visible rows; the selection counts source rows.
			// Ticking the first match must mark that package, not package 0.
			orig, ok := m.pkgAt(m.pkgCursor)
			if !ok {
				return m, nil
			}
			if m.pkgSelected == nil {
				m.pkgSelected = make(map[int]bool)
			}
			// Unticking removes the entry rather than storing false, so that
			// len(m.pkgSelected) is always the number of ticked packages.
			if m.pkgSelected[orig] {
				delete(m.pkgSelected, orig)
			} else {
				m.pkgSelected[orig] = true
			}
			return m, nil

		case key.Matches(msg, keys.All):
			n := m.pkgCount()
			if !m.pkgLoaded || n == 0 {
				return m, nil
			}
			if m.pkgSelected == nil {
				m.pkgSelected = make(map[int]bool)
			}
			// "All" means every row on screen — what the user can see they are
			// choosing. A hidden row keeps whatever it had, so `a` under a filter
			// never reaches past the list and never quietly drops a tick the user
			// made before typing.
			allVisible := true
			for v := 0; v < n; v++ {
				orig, ok := m.pkgAt(v)
				if !ok || !m.pkgSelected[orig] {
					allVisible = false
					break
				}
			}
			for v := 0; v < n; v++ {
				orig, ok := m.pkgAt(v)
				if !ok {
					continue
				}
				if allVisible {
					delete(m.pkgSelected, orig)
				} else {
					m.pkgSelected[orig] = true
				}
			}
			return m, nil

		case key.Matches(msg, keys.Delete):
			if !m.pkgLoaded {
				m.statusMsg = "Still loading packages..."
				m.statusErr = true
				return m, nil
			}
			if len(m.pkgSelected) == 0 {
				m.statusMsg = "No packages selected. Use space to select, a to select all."
				m.statusErr = true
				return m, nil
			}
			// Freeze the analysis now: the dialog and the command must both act
			// on the same set even if a refresh lands in between.
			m.rel = m.selectionRelation()
			m.state = statePackageDeleteConfirm
			return m, nil
		}
		return m, nil

	case statePackageDeleteConfirm, statePackageDeleting:
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
		if m.state == statePackageDeleting {
			return m, nil // wait for the delete to finish
		}
		switch {
		case key.Matches(msg, keys.Confirm):
			// y: the selection plus everything the analysis flagged with it.
			m.state = statePackageDeleting
			return m, deletePackagesCmd(m.pkgEnvType, m.pkgIdx, m.condaEnvs, m.uvEnvs, m.deleteSet(true))
		case msg.String() == "n":
			// n: only what was ticked, leaving the rest as it falls.
			m.state = statePackageDeleting
			return m, deletePackagesCmd(m.pkgEnvType, m.pkgIdx, m.condaEnvs, m.uvEnvs, m.deleteSet(false))
		case key.Matches(msg, keys.Cancel):
			m.state = statePackageList
			m.rel = relation{}
		}
		return m, nil

	case stateList, stateLoadingDetails:
		switch {
		case key.Matches(msg, keys.Quit):
			return m, tea.Quit

		case key.Matches(msg, keys.Up):
			m.moveCursor(-1)
			return m, m.triggerDetailLoad()

		case key.Matches(msg, keys.Down):
			m.moveCursor(1)
			return m, m.triggerDetailLoad()

		case key.Matches(msg, keys.Activate):
			if sel := m.selectedItem(); sel != nil && sel.kind == kindEnv {
				return m, m.activateCmd(*sel)
			}
			return m, nil

		case key.Matches(msg, keys.Filter):
			m.openFilter()
			return m, nil

		case key.Matches(msg, keys.New):
			m.resetCreateForm()
			m.state = stateCreate
			return m, nil

		case key.Matches(msg, keys.Delete):
			if sel := m.selectedItem(); sel != nil && sel.kind == kindEnv {
				m.deleteTarget = *sel
				m.state = stateDeleteConfirm
			}
			return m, nil

		case key.Matches(msg, keys.Packages):
			sel := m.selectedItem()
			if sel == nil || sel.kind != kindEnv {
				return m, nil
			}
			m.pkgEnvType = sel.envType
			m.pkgIdx = sel.idx
			m.packages = nil
			m.pkgSelected = make(map[int]bool)
			m.pkgCursor = 0
			m.pkgLoaded = false
			// The environment filter named environments, which say nothing about
			// this environment's packages. Start the new list unfiltered.
			m.clearFilter()
			// Drop the previous environment's graph so the panel cannot pair its
			// relations with this environment's list while the load is in flight.
			m.deps, m.dependents = nil, nil
			m.rel = relation{}
			m.statusMsg = ""
			m.state = statePackageList
			return m, loadPackagesCmd(sel.envType, sel.idx, m.condaEnvs, m.uvEnvs)

		case key.Matches(msg, keys.Refresh):
			m.statusMsg = ""
			return m, loadEnvsCmd(m.cwd)
		}
	}

	return m, nil
}

// ── view ──────────────────────────────────────────────────────────────────────

func (m Model) View() string {
	if m.width == 0 {
		return "Loading..."
	}

	switch m.state {
	case stateCreate:
		return m.viewCreate()
	case stateDeleteConfirm:
		return m.viewDeleteConfirm()
	case statePackageList:
		return m.viewPackages()
	case statePackageDeleteConfirm:
		return m.viewPackageDeleteConfirm()
	case stateCreating, stateDeleting:
		action := "Creating"
		if m.state == stateDeleting {
			action = "Deleting"
		}
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center,
			m.spinner.View()+" "+action+"...")
	case statePackageDeleting:
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center,
			m.spinner.View()+" Deleting packages...")
	}

	return m.viewMain()
}

func (m Model) viewMain() string {
	leftW := 36
	if m.width < 80 {
		leftW = m.width / 2
	}
	rightW := m.width - leftW - 3 // 3 for borders gap

	panelH := m.height - 3 // leave room for status bar

	left := m.renderList(leftW, panelH)
	right := m.renderDetail(rightW, panelH)

	cols := lipgloss.JoinHorizontal(lipgloss.Top, left, " ", right)
	status := m.renderStatusBar()
	return lipgloss.JoinVertical(lipgloss.Left, cols, status)
}

func (m Model) renderList(w, h int) string {
	innerW := w - 2
	innerH := h - 2

	var sb strings.Builder
	list := m.envList()
	for i, item := range list {
		if item.kind == kindHeader {
			line := sectionHeaderStyle.Width(innerW).Render("  " + item.label)
			sb.WriteString(line + "\n")
			continue
		}

		var name, ver, activeMarker string
		if item.envType == "conda" && item.idx < len(m.condaEnvs) {
			e := m.condaEnvs[item.idx]
			name = e.Name
			ver = e.PythonVer
			if e.Active {
				activeMarker = activeMarkerStyle.Render("*")
			}
		} else if item.envType == "uv" && item.idx < len(m.uvEnvs) {
			e := m.uvEnvs[item.idx]
			name = e.Name
			ver = e.PythonVer
			if e.Active {
				activeMarker = activeMarkerStyle.Render("*")
			}
		}

		verStr := ""
		if ver != "" && ver != "unknown" {
			// Show only major.minor
			parts := strings.Split(ver, ".")
			if len(parts) >= 2 {
				verStr = parts[0] + "." + parts[1]
			} else {
				verStr = ver
			}
		}

		// The marker belongs beside the name, not out past the version. Its
		// column is reserved on every row — blank when the environment is not
		// the active one — so the versions still line up.
		marker := "  "
		if activeMarker != "" {
			marker = activeMarker + " "
		}

		nameW := innerW - 10
		if nameW < 10 {
			nameW = 10
		}

		line := marker + fmt.Sprintf("%-*s %s", nameW, name, verStr)

		if i == m.cursor {
			line = selectedItemStyle.Width(innerW).Render(" " + line)
		} else {
			line = itemStyle.Width(innerW).Render(line)
		}
		sb.WriteString(line + "\n")
	}

	// An empty panel with a filter on would read as a failed load rather than as
	// a search that found nothing.
	if sb.Len() == 0 && m.filterText() != "" {
		sb.WriteString(itemStyle.Render("  no matches") + "\n")
	}

	content := sb.String()
	// Trim to fit height
	lines := strings.Split(content, "\n")
	if len(lines) > innerH {
		// Simple scroll: keep cursor visible
		start := 0
		if m.cursor > innerH-1 {
			start = m.cursor - innerH + 1
		}
		end := start + innerH
		if end > len(lines) {
			end = len(lines)
		}
		lines = lines[start:end]
	}
	// Pad to fill panel
	for len(lines) < innerH {
		lines = append(lines, "")
	}
	content = strings.Join(lines[:innerH], "\n")

	style := panelStyle.Width(w).Height(h)
	if m.state == stateList {
		style = panelActiveStyle.Width(w).Height(h)
	}
	return style.Render(content)
}

func (m Model) renderDetail(w, h int) string {
	innerW := w - 2

	sel := m.selectedItem()
	if sel == nil || sel.kind == kindHeader {
		// Truncated to the panel before it is placed: Place pads a narrow box but
		// never clips the text, and a line wider than the panel makes the panel
		// wrap and grow a row, which pushes the whole layout off the screen.
		return panelStyle.Width(w).Height(h).Render(
			lipgloss.Place(innerW, h-2, lipgloss.Center, lipgloss.Center,
				statusBarStyle.Render(truncate("Select an environment", innerW))),
		)
	}

	var name, path, pythonVer, envType string
	var pkgCount int
	var sizeBytes int64
	var createdAt time.Time
	var loaded bool
	var activateCmd string

	if sel.envType == "conda" && sel.idx < len(m.condaEnvs) {
		e := m.condaEnvs[sel.idx]
		name, path, pythonVer = e.Name, e.Path, e.PythonVer
		pkgCount, sizeBytes, loaded = e.PkgCount, e.SizeBytes, e.Loaded
		createdAt = e.CreatedAt
		envType = "conda"
		activateCmd = conda.ActivateCmd(e)
	} else if sel.envType == "uv" && sel.idx < len(m.uvEnvs) {
		e := m.uvEnvs[sel.idx]
		name, path, pythonVer = e.Name, e.Path, e.PythonVer
		pkgCount, sizeBytes, loaded = e.PkgCount, e.SizeBytes, e.Loaded
		createdAt = e.CreatedAt
		envType = "uv"
		activateCmd = uv.ActivateCmd(e)
	}

	// Shorten path for display
	home, _ := os.UserHomeDir()
	displayPath := strings.Replace(path, home, "~", 1)

	var typeBadge string
	if envType == "conda" {
		typeBadge = condaBadgeStyle.Render("conda")
	} else {
		typeBadge = uvBadgeStyle.Render("uv")
	}

	var sb strings.Builder
	sb.WriteString(detailTitleStyle.Render(name) + "  " + typeBadge + "\n\n")

	row := func(k, v string) string {
		return detailKeyStyle.Render(k) + detailValStyle.Render(v) + "\n"
	}

	if loaded {
		sb.WriteString(row("Python", pythonVer))
		sb.WriteString(row("Packages", fmt.Sprintf("%d", pkgCount)))
		sb.WriteString(row("Size", formatSize(sizeBytes)))
		sb.WriteString(row("Created", formatAge(createdAt, time.Now())))
	} else {
		sb.WriteString(m.spinner.View() + " loading details...\n")
	}

	sb.WriteString(row("Path", ""))
	// Wrap path across full width
	sb.WriteString("  " + statusBarStyle.Render(displayPath) + "\n")

	sb.WriteString("\n")
	sb.WriteString(detailKeyStyle.Render("Activate") + "\n")
	sb.WriteString(activeCmdStyle.Render(activateCmd) + "\n")

	return panelStyle.Width(w).Height(h).Render(
		lipgloss.NewStyle().Width(innerW).Render(sb.String()),
	)
}

func (m Model) viewCreate() string {
	var sb strings.Builder
	sb.WriteString(detailTitleStyle.Render("New uv Environment") + "\n\n")
	sb.WriteString(inputLabelStyle.Render("Name") + "\n")
	sb.WriteString(m.createInputs[0].View() + "\n\n")
	sb.WriteString(inputLabelStyle.Render("Python Version") + "\n")
	sb.WriteString(m.createInputs[1].View() + "\n\n")
	sb.WriteString(statusBarStyle.Render("tab") + " next  " +
		statusBarStyle.Render("enter") + " create  " +
		statusBarStyle.Render("esc") + " cancel")

	if m.statusMsg != "" {
		sb.WriteString("\n\n")
		if m.statusErr {
			sb.WriteString(errorStyle.Render(m.statusMsg))
		} else {
			sb.WriteString(successStyle.Render(m.statusMsg))
		}
	}

	box := panelActiveStyle.Width(50).Padding(1, 2).Render(sb.String())
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}

func (m Model) viewDeleteConfirm() string {
	item := m.deleteTarget
	var envName string
	if item.envType == "conda" && item.idx < len(m.condaEnvs) {
		envName = m.condaEnvs[item.idx].Name
	} else if item.envType == "uv" && item.idx < len(m.uvEnvs) {
		envName = m.uvEnvs[item.idx].Name
	}

	msg := confirmStyle.Render("Delete ") +
		dangerStyle.Render(envName) +
		confirmStyle.Render("?") + "\n\n" +
		keyStyle.Render("y") + " confirm  " +
		keyStyle.Render("esc") + " cancel"

	box := panelActiveStyle.Width(40).Padding(1, 2).Render(msg)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}

// clampPkgCursor keeps the package cursor and selection within the current list.
// The cursor is measured against the visible rows and the selection against the
// source list, because that is what each one is keyed by.
func (m *Model) clampPkgCursor() {
	if n := m.pkgCount(); m.pkgCursor >= n {
		m.pkgCursor = n - 1
	}
	if m.pkgCursor < 0 {
		m.pkgCursor = 0
	}
	for i := range m.pkgSelected {
		if i >= len(m.packages) {
			delete(m.pkgSelected, i)
		}
	}
}

// pkgEnvName returns the display name of the environment whose packages are shown.
func (m Model) pkgEnvName() string {
	if m.pkgEnvType == "conda" && m.pkgIdx < len(m.condaEnvs) {
		return m.condaEnvs[m.pkgIdx].Name
	}
	if m.pkgEnvType == "uv" && m.pkgIdx < len(m.uvEnvs) {
		return m.uvEnvs[m.pkgIdx].Name
	}
	return "?"
}

// viewPackages renders the package list, shrinking the visible window until the
// box fits the terminal. The chrome around the list (border, padding, title,
// hint and the optional status lines) adds a variable number of rows, so the
// height is measured rather than assumed.
func (m Model) viewPackages() string {
	// Two columns need room for both; below that the list gets the whole width.
	if len(m.packages) > 0 && m.width >= 72 {
		return m.viewPackagesTwoColumn()
	}

	// Sized to the rows on screen, not to the environment's total: with a filter
	// on, the panel should be as tall as what it is showing.
	maxRows := m.pkgCount()
	if maxRows < 1 {
		maxRows = 1
	}

	box := m.renderPackagesBox(maxRows)
	if h := lipgloss.Height(box); h > m.height && m.pkgCount() > 0 {
		// Every row dropped removes exactly one line.
		maxRows -= h - m.height
		if maxRows < 1 {
			maxRows = 1
		}
		box = m.renderPackagesBox(maxRows)
	}
	// On a very short terminal even the chrome does not fit; clip rather than
	// write a panel taller than the screen.
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, clipHeight(box, m.height))
}

// viewPackagesTwoColumn shows the package list beside the highlighted package's
// relations, mirroring the environment view's list/detail split.
func (m Model) viewPackagesTwoColumn() string {
	leftW := (m.width - 3) * 2 / 5
	if leftW < 30 {
		leftW = 30
	}
	if leftW > 56 {
		leftW = 56
	}
	rightW := m.width - leftW - 3
	panelH := m.height - 3

	cols := lipgloss.JoinHorizontal(lipgloss.Top,
		m.renderPackageList(leftW, panelH), " ", m.renderDepPanel(rightW, panelH))
	status := clipHeight(m.renderPackageStatusBar(), 1)
	return lipgloss.JoinVertical(lipgloss.Left, cols, status)
}

func (m Model) renderPackageList(w, h int) string {
	innerW, innerH := w-2, h-2

	// title, its blank line, and the footer
	maxRows := innerH - 3
	if maxRows < 1 {
		maxRows = 1
	}

	n := m.pkgCount()

	var sb strings.Builder
	title := fmt.Sprintf("%s  %s", m.pkgEnvName(), countLabel(n, len(m.packages), "packages"))
	sb.WriteString(detailTitleStyle.Render(truncate(title, innerW)) + "\n\n")

	start := 0
	if m.pkgCursor >= maxRows {
		start = m.pkgCursor - maxRows + 1
	}
	end := start + maxRows
	if end > n {
		end = n
	}

	for v := start; v < end; v++ {
		orig, ok := m.pkgAt(v)
		if !ok {
			continue
		}
		mark := "[ ]"
		if m.pkgSelected[orig] {
			mark = "[x]"
		}
		line := truncate(fmt.Sprintf("%s %s", mark, m.packages[orig]), innerW)
		switch {
		case v == m.pkgCursor:
			line = selectedItemStyle.Render(" " + line)
		case m.pkgSelected[orig]:
			line = successStyle.Render("  " + line)
		default:
			line = itemStyle.Render(line)
		}
		sb.WriteString(line + "\n")
	}

	if n == 0 && m.filterText() != "" {
		sb.WriteString(itemStyle.Render("  no matches") + "\n")
	}

	var footer string
	if n > maxRows {
		footer = fmt.Sprintf("%d-%d of %d", start+1, end, n)
	}
	if n := len(m.pkgSelected); n > 0 {
		if footer != "" {
			footer += "  "
		}
		footer += fmt.Sprintf("%d selected", n)
	}
	sb.WriteString(statusBarStyle.Render(truncate("  "+footer, innerW)))

	return panelActiveStyle.Width(innerW).Height(innerH).Render(sb.String())
}

// renderDepPanel lists what the highlighted package needs and what needs it.
// Only packages present in the environment are shown, since those are the only
// ones a delete could act on.
func (m Model) renderDepPanel(w, h int) string {
	innerW, innerH := w-2, h-2
	if innerW < 8 || innerH < 4 {
		return panelStyle.Width(max(innerW, 1)).Height(max(innerH, 1)).Render("")
	}
	orig, ok := m.pkgAt(m.pkgCursor)
	if !ok {
		return panelStyle.Width(innerW).Height(innerH).Render("")
	}

	name := m.packages[orig]

	var sb strings.Builder
	sb.WriteString(detailTitleStyle.Render(truncate(name, innerW)) + "\n\n")

	if m.deps == nil && m.dependents == nil {
		sb.WriteString(statusBarStyle.Render("  dependency data unavailable"))
		return panelStyle.Width(innerW).Height(innerH).Render(sb.String())
	}

	needs := m.installedOnly(m.deps[name])
	usedBy := m.installedOnly(m.dependents[name])

	// The title, its blank line and the blank between the sections are already
	// spent; the sections divide what is left, each counting its own header.
	lines := max(innerH-3, 2)
	needLines, usedLines := (lines+1)/2, lines/2
	switch {
	case len(needs) == 0 && len(usedBy) == 0:
		needLines, usedLines = 1, 1
	case len(needs) == 0:
		needLines, usedLines = 1, lines-1
	case len(usedBy) == 0:
		needLines, usedLines = lines-1, 1
	}

	out := depSection("needs", needs, needLines, innerW)
	out = append(out, "")
	out = append(out, depSection("needed by", usedBy, usedLines, innerW)...)

	return panelStyle.Width(innerW).Height(innerH).Render(strings.Join(out, "\n"))
}

// depSection renders one titled group of package names, marking any that did
// not fit. maxLines is the total the section may occupy, header included, so
// the caller can budget rows exactly rather than leaving them to be clipped.
func depSection(title string, names []string, maxLines, width int) []string {
	maxLines = max(maxLines, 1)
	// The header states the true total, which is what keeps a list cut to a
	// single line honest. The style indents it by one column, so the text has
	// one column less than the budget, and the title gives way before the
	// count: a clipped number reads as a smaller, wrong one.
	room := width - 1
	suffix := fmt.Sprintf(" (%d)", len(names))
	header := truncate(truncate(title, max(room-len([]rune(suffix)), 0))+suffix, room)
	lines := []string{sectionHeaderStyle.Render(header)}
	if maxLines == 1 {
		return lines
	}
	if len(names) == 0 {
		return append(lines, statusBarStyle.Render("  -"))
	}

	// A truncated list needs a line for the count of what did not fit.
	show := len(names)
	if room := maxLines - 1; show > room {
		show = room - 1
	}
	shown := 0
	for _, n := range names {
		if shown == show {
			break
		}
		lines = append(lines, itemStyle.Render(truncate(n, width-2)))
		shown++
	}
	if rest := len(names) - shown; rest > 0 {
		lines = append(lines, statusBarStyle.Render(fmt.Sprintf("  +%d more", rest)))
	}
	return lines
}

// installedOnly drops names absent from the current list.
func (m Model) installedOnly(names []string) []string {
	if len(names) == 0 {
		return nil
	}
	installed := make(map[string]bool, len(m.packages))
	for _, n := range m.packages {
		installed[n] = true
	}
	out := make([]string, 0, len(names))
	for _, n := range names {
		if installed[n] {
			out = append(out, n)
		}
	}
	return out
}

// countLabel writes "matched/total unit", or just the total when nothing is
// filtered out — a bare "631 packages" reads better than "631/631 packages".
func countLabel(matched, total int, unit string) string {
	if matched == total {
		return fmt.Sprintf("%d %s", total, unit)
	}
	return fmt.Sprintf("%d/%d %s", matched, total, unit)
}

// truncate shortens s to at most w runes, marking the cut with an ellipsis.
// It runs before styling, so plain rune arithmetic is enough.
func truncate(s string, w int) string {
	if w <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= w {
		return s
	}
	if w == 1 {
		return string(r[:1])
	}
	return string(r[:w-1]) + "…"
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// clipHeight drops trailing lines so s is at most h lines tall.
func clipHeight(s string, h int) string {
	if h <= 0 {
		return ""
	}
	lines := strings.Split(s, "\n")
	if len(lines) <= h {
		return s
	}
	return strings.Join(lines[:h], "\n")
}

// renderPackagesBox builds the package panel showing at most maxRows list rows.
func (m Model) renderPackagesBox(maxRows int) string {
	var badge string
	if m.pkgEnvType == "conda" {
		badge = condaBadgeStyle.Render("conda")
	} else {
		badge = uvBadgeStyle.Render("uv")
	}

	selected := len(m.pkgSelected)
	n := m.pkgCount()

	var sb strings.Builder
	sb.WriteString(detailTitleStyle.Render(m.pkgEnvName()) + "  " + badge + "  " +
		statusBarStyle.Render(countLabel(n, len(m.packages), "packages")) + "\n\n")

	if !m.pkgLoaded {
		sb.WriteString(m.spinner.View() + " loading packages...\n")
	} else if n == 0 {
		// A filter that matches nothing and an empty environment are different
		// problems, and only one of them is the user's to act on.
		if m.filterText() != "" {
			sb.WriteString(statusBarStyle.Render("No matching packages.") + "\n")
		} else {
			sb.WriteString(statusBarStyle.Render("No packages found.") + "\n")
		}
	} else {
		start := 0
		if m.pkgCursor >= maxRows {
			start = m.pkgCursor - maxRows + 1
		}
		end := start + maxRows
		if end > n {
			end = n
		}

		for v := start; v < end; v++ {
			orig, ok := m.pkgAt(v)
			if !ok {
				continue
			}
			mark := "[ ]"
			if m.pkgSelected[orig] {
				mark = "[x]"
			}
			line := fmt.Sprintf("%s %s", mark, m.packages[orig])
			if v == m.pkgCursor {
				line = selectedItemStyle.Render(" " + line)
			} else if m.pkgSelected[orig] {
				line = successStyle.Render("  " + line)
			} else {
				line = itemStyle.Render(line)
			}
			sb.WriteString(line + "\n")
		}

		if n > maxRows {
			sb.WriteString(statusBarStyle.Render(
				fmt.Sprintf("\n  %d-%d of %d", start+1, end, n)) + "\n")
		}
	}

	sb.WriteString("\n")
	sb.WriteString(keyStyle.Render("space") + " toggle  " +
		keyStyle.Render("a") + " all  " +
		keyStyle.Render("f") + " filter  " +
		keyStyle.Render("d") + " delete selected  " +
		keyStyle.Render("esc") + " back")

	if m.statusMsg != "" {
		sb.WriteString("\n")
		if m.statusErr {
			sb.WriteString(errorStyle.Render(m.statusMsg))
		} else {
			sb.WriteString(successStyle.Render(m.statusMsg))
		}
	}

	if selected > 0 {
		sb.WriteString("\n" + confirmStyle.Render(fmt.Sprintf("%d selected", selected)))
	}

	width := m.width - 4
	if width < 4 {
		width = 4
	}
	return panelActiveStyle.Width(width).Padding(1, 2).Render(sb.String())
}

func (m Model) viewPackageDeleteConfirm() string {
	names := m.selectedNames()

	// Below this a terminal cannot show the lists and the decision together.
	// The dialog collapses to the decision alone, because the alternative is
	// chrome clipping the key hints off the bottom — leaving the user in a
	// dialog with no visible way out of it.
	if m.height < deleteDialogFullHeight {
		return m.viewPackageDeleteCompact(names)
	}

	// The box width drives how much of a package name fits on a line, and the
	// terminal height how many lines the name lists may take. The chrome cost
	// is fixed and known, so the budget is arithmetic rather than a guess and
	// clipHeight never has to fire.
	boxW := min(76, max(40, m.width-4))
	innerW := boxW - 6
	rows := max(1, m.height-deleteDialogChrome)

	var sb strings.Builder
	sb.WriteString(confirmStyle.Render(fmt.Sprintf("Delete %d package(s) from ", len(names))) +
		dangerStyle.Render(m.pkgEnvName()) +
		confirmStyle.Render("?") + "\n\n")
	// The selection itself only needs enough rows to recognise it.
	sb.WriteString(linesBlock(nameLines(names, innerW, min(3, rows))) + "\n")

	if m.rel.Empty() {
		sb.WriteString("\n" + statusBarStyle.Render("No other installed package is connected to this selection.") + "\n")
	} else {
		// Share what is left between the two groups when both are present. The
		// broken dependents outrank the orphans, so they take the odd row.
		breakRows, orphanRows := rows, 0
		if len(m.rel.Breaks) > 0 && len(m.rel.Orphans) > 0 {
			breakRows, orphanRows = rows-rows/2, rows/2
		}

		sb.WriteString("\n")
		if n := len(m.rel.Breaks); n > 0 {
			sb.WriteString(dangerStyle.Render(fmt.Sprintf("%d package(s) depend on what you selected", n)) +
				statusBarStyle.Render(" and would be left broken:") + "\n")
			if breakRows > 0 {
				sb.WriteString(linesBlock(nameLines(m.rel.Breaks, innerW, breakRows)) + "\n")
			}
		}
		if n := len(m.rel.Orphans); n > 0 {
			sb.WriteString(confirmStyle.Render(fmt.Sprintf("%d package(s)", n)) +
				statusBarStyle.Render(" are only needed by your selection and would be left unused:") + "\n")
			if orphanRows > 0 {
				sb.WriteString(linesBlock(nameLines(m.rel.Orphans, innerW, orphanRows)) + "\n")
			}
		}
		// conda's solver keeps the environment consistent and so removes more
		// than it is asked to; the graph above is a lower bound, not a promise.
		if m.pkgEnvType == "conda" {
			sb.WriteString("\n" + confirmStyle.Render("note: ") +
				statusBarStyle.Render("conda may also remove further packages to keep the environment consistent.") + "\n")
		}
	}

	sb.WriteString("\n")
	sb.WriteString(m.deleteConfirmHints(names))

	box := panelActiveStyle.Width(boxW).Padding(1, 2).Render(sb.String())
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, clipHeight(box, m.height))
}

// viewPackageDeleteCompact is the dialog for a terminal too short for the
// package lists: what is at stake, and the keys.
func (m Model) viewPackageDeleteCompact(names []string) string {
	var sb strings.Builder
	sb.WriteString(confirmStyle.Render(fmt.Sprintf("Delete %d package(s) from ", len(names))) +
		dangerStyle.Render(m.pkgEnvName()) + confirmStyle.Render("?") + "\n")
	if n := len(m.rel.Breaks); n > 0 {
		sb.WriteString(dangerStyle.Render(fmt.Sprintf("%d dependent(s) would break", n)) + "\n")
	}
	if n := len(m.rel.Orphans); n > 0 {
		sb.WriteString(confirmStyle.Render(fmt.Sprintf("%d dependency(ies) would be orphaned", n)) + "\n")
	}
	sb.WriteString("\n" + m.deleteConfirmHints(names))

	box := panelActiveStyle.Width(min(76, max(40, m.width-4))).Padding(1, 2).Render(sb.String())
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, clipHeight(box, m.height))
}

// deleteConfirmHints renders the key legend, which both dialog sizes show.
func (m Model) deleteConfirmHints(names []string) string {
	if m.rel.Empty() {
		return keyStyle.Render("y") + " delete  " + keyStyle.Render("esc") + " cancel"
	}
	total := len(names) + len(m.rel.All())
	return keyStyle.Render("y") + fmt.Sprintf(" delete all %d  ", total) +
		keyStyle.Render("n") + fmt.Sprintf(" only the %d selected  ", len(names)) +
		keyStyle.Render("esc") + " cancel"
}

// deleteDialogChrome is the number of rows viewPackageDeleteConfirm spends on
// everything that is not a package list: border, padding, the heading and its
// blank line, the selection preview, the group headings, the conda note and the
// key hints. The two group lists get whatever the terminal has left over.
const deleteDialogChrome = 15

// deleteDialogFullHeight is the shortest terminal that can show the chrome
// above plus one row for each group list.
const deleteDialogFullHeight = 17

// linesBlock renders name lines in the muted style used for secondary text.
func linesBlock(lines []string) string {
	if len(lines) == 0 {
		return statusBarStyle.Render("-")
	}
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = statusBarStyle.Render(l)
	}
	return strings.Join(out, "\n")
}

// nameLines flows names into comma-separated lines of at most width runes. When
// more than maxRows lines are needed it keeps the first maxRows and folds the
// count of what is not shown into the last one, so a long list is still
// recognisable rather than reduced to a truncated stub.
func nameLines(names []string, width, maxRows int) []string {
	if len(names) == 0 || maxRows < 1 || width <= 0 {
		return nil
	}

	var lines []string
	cur, inLine := "", 0
	for i, n := range names {
		if inLine > 0 && len([]rune(cur))+2+len([]rune(n)) > width {
			lines = append(lines, truncate(cur, width))
			cur, inLine = "", 0
		}
		if inLine == 0 {
			if len(lines) >= maxRows {
				// The count is the whole point of the last line, so reserve its
				// room before trimming the names to fit beside it.
				suffix := fmt.Sprintf(" +%d more", len(names)-i)
				keep := width - len([]rune(suffix))
				if keep < 1 {
					lines[len(lines)-1] = truncate(strings.TrimSpace(suffix), width)
				} else {
					lines[len(lines)-1] = truncate(lines[len(lines)-1], keep) + suffix
				}
				return lines
			}
			cur, inLine = n, 1
			continue
		}
		cur += ", " + n
		inLine++
	}
	if inLine > 0 {
		lines = append(lines, truncate(cur, width))
	}
	return lines
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func (m Model) renderStatusBar() string {
	// The box takes the row rather than adding one, so the list above keeps its
	// height and its position while the user types.
	if m.filtering {
		return m.filterLine()
	}

	msg, msgStyle := m.statusText()
	return m.statusLine(m.hints(), msg, msgStyle)
}

// hint is one key and what it does, as shown in the status legend.
type hint struct{ key, label string }

func (m Model) hints() []hint {
	switch m.state {
	case statePackageList:
		return []hint{
			{"space", "toggle"}, {"a", "all"}, {"f", "filter"},
			{"d", "delete"}, {"esc", "back"},
		}
	case stateList:
		return []hint{
			{"↵", "activate"}, {"p", "packages"}, {"n", "new"}, {"d", "delete"},
			{"f", "filter"}, {"r", "refresh"}, {"q", "quit"},
		}
	}
	return nil
}

// renderHints draws as many pairs as fit in room columns, giving up whole pairs
// from the end. Trimming the finished string instead would cut through the
// escape sequences the key labels are styled with.
func renderHints(hints []hint, room int) string {
	var b strings.Builder
	used := 0
	for _, h := range hints {
		piece := keyStyle.Render(h.key) + " " + h.label + "  "
		w := lipgloss.Width(piece)
		if used+w > room {
			break
		}
		used += w
		b.WriteString(piece)
	}
	return b.String()
}

// statusText is what the status bar has to say, and how to say it. A message
// about something that just happened outranks the standing note that a filter is
// narrowing the list.
func (m Model) statusText() (string, lipgloss.Style) {
	switch {
	case m.statusMsg != "" && m.statusErr:
		return m.statusMsg, errorStyle
	case m.statusMsg != "":
		return m.statusMsg, successStyle
	case m.filterText() != "":
		return fmt.Sprintf("filter: %q  %d shown", m.filterText(), m.visibleCount()), filterStatusStyle
	}
	return "", statusBarStyle
}

// statusLine assembles the single row the layout reserves for the status bar.
// lipgloss wraps rather than clips, and a wrapped status bar silently steals a
// row from the list above it, so the parts are measured and trimmed here
// instead. What gives way is the key hints: they describe what could happen
// next, while the message reports what just did.
func (m Model) statusLine(hints []hint, msg string, msgStyle lipgloss.Style) string {
	const prefix = " pvman  "
	room := m.width - lipgloss.Width(prefix)

	// The message is measured as plain text, before it is styled: trimming a
	// styled string would slice through its escape sequences.
	msgW := 0
	if msg != "" {
		msg = truncate(msg, max(room-2, 0))
		msgW = 2 + len([]rune(msg))
		msg = msgStyle.Render("  " + msg)
	}

	return statusBarStyle.Width(m.width).Render(
		prefix + renderHints(hints, max(room-msgW, 0)) + msg)
}

// renderPackageStatusBar renders the key hints and status line under the
// package view.
func (m Model) renderPackageStatusBar() string {
	if m.filtering {
		return m.filterLine()
	}

	msg, msgStyle := m.statusText()
	return m.statusLine(m.hints(), msg, msgStyle)
}

// ── helpers ───────────────────────────────────────────────────────────────────

func (m *Model) rebuildItems() {
	m.items = nil
	if len(m.condaEnvs) > 0 {
		m.items = append(m.items, listItem{kind: kindHeader, label: "conda"})
		for i := range m.condaEnvs {
			m.items = append(m.items, listItem{kind: kindEnv, envType: "conda", idx: i, label: m.condaEnvs[i].Name})
		}
	}
	if len(m.uvEnvs) > 0 {
		m.items = append(m.items, listItem{kind: kindHeader, label: "uv  (" + m.cwd + ")"})
		for i := range m.uvEnvs {
			m.items = append(m.items, listItem{kind: kindEnv, envType: "uv", idx: i, label: m.uvEnvs[i].Name})
		}
	}
}

func (m *Model) skipHeaders() {
	list := m.envList()
	for m.cursor >= 0 && m.cursor < len(list) && list[m.cursor].kind == kindHeader {
		m.cursor++
	}
}

// wrap folds i into [0,n). Both cursors walk their list this way, so the ends
// are neighbours rather than walls.
func wrap(i, n int) int {
	if n <= 0 {
		return 0
	}
	i %= n
	if i < 0 {
		i += n
	}
	return i
}

func (m *Model) moveCursor(dir int) {
	list := m.envList()
	if len(list) == 0 {
		m.cursor = 0
		return
	}

	n := len(list)
	m.cursor = wrap(m.cursor, n)
	next := wrap(m.cursor+dir, n)

	// Skip headers, wrapping around if needed. The hop count bounds the walk, so
	// a list that is all headers — which a filter over an empty section could
	// leave behind — cannot spin here forever.
	for hops := 0; hops < n && list[next].kind == kindHeader; hops++ {
		next = wrap(next+dir, n)
	}

	m.cursor = next
}

// ── filtering ─────────────────────────────────────────────────────────────────

// matchFilter reports whether s contains needle, ignoring case. Containment
// rather than a prefix or a fuzzy score: package names put their meaning in the
// middle as often as at the front, and "ng" should find "libgcc-ng".
func matchFilter(s, needle string) bool {
	if needle == "" {
		return true
	}
	return strings.Contains(strings.ToLower(s), strings.ToLower(needle))
}

// filterText is the active filter, or "" when none is applied. Surrounding
// space is ignored so a stray space does not turn into "no matches".
func (m Model) filterText() string {
	return strings.TrimSpace(m.filter.Value())
}

// envList is the environment list as shown: the filtered subset while a filter
// is on, the whole list otherwise. The cursor indexes this, never m.items.
func (m Model) envList() []listItem {
	if m.filteredItems != nil {
		return m.filteredItems
	}
	return m.items
}

// pkgCount is the number of package rows on screen.
func (m Model) pkgCount() int {
	if m.filteredPkgs != nil {
		return len(m.filteredPkgs)
	}
	return len(m.packages)
}

// pkgAt maps a visible package row to its index in m.packages. The selection map
// is keyed by that source index, so ticking survives a change of filter.
func (m Model) pkgAt(visible int) (int, bool) {
	if visible < 0 || visible >= m.pkgCount() {
		return 0, false
	}
	if m.filteredPkgs != nil {
		return m.filteredPkgs[visible], true
	}
	return visible, true
}

// visibleCount is how many rows whichever list is on screen currently shows.
// Section headers are not counted: they are not something the user can pick, and
// "4 shown" for two surviving environments reads as a bug in the count.
func (m Model) visibleCount() int {
	if m.state == statePackageList {
		return m.pkgCount()
	}
	n := 0
	for _, it := range m.envList() {
		if it.kind == kindEnv {
			n++
		}
	}
	return n
}

// envAnchor identifies the highlighted environment by what it is rather than
// where it sits, so it can be found again after the list changes shape.
type envAnchor struct {
	envType string
	idx     int
}

func (m Model) envAnchorOfCursor() (envAnchor, bool) {
	if it := m.selectedItem(); it != nil && it.kind == kindEnv {
		return envAnchor{it.envType, it.idx}, true
	}
	return envAnchor{}, false
}

// applyEnvFilter rebuilds the visible environment list from the filter text and
// moves the cursor back onto the environment it was on, when that environment
// is still there.
func (m *Model) applyEnvFilter() {
	needle := m.filterText()
	anchor, had := m.envAnchorOfCursor()

	if needle == "" {
		m.filteredItems = nil
	} else {
		out := make([]listItem, 0, len(m.items))
		// A section keeps its title only once one of its environments matches, so
		// a filter cannot leave a heading stranded over an empty stretch.
		var pending listItem
		hasPending := false
		for _, it := range m.items {
			if it.kind == kindHeader {
				pending, hasPending = it, true
				continue
			}
			if !matchFilter(it.label, needle) {
				continue
			}
			if hasPending {
				out = append(out, pending)
				hasPending = false
			}
			out = append(out, it)
		}
		m.filteredItems = out
	}

	list := m.envList()
	m.cursor = 0
	if had {
		for i, it := range list {
			if it.kind == kindEnv && it.envType == anchor.envType && it.idx == anchor.idx {
				m.cursor = i
				break
			}
		}
	}
	m.cursor = wrap(m.cursor, len(list))
	m.skipHeaders()
}

// applyPkgFilter rebuilds the visible package list. pkgSelected is keyed by
// source index and is left untouched: ticking a few rows and then changing the
// filter must not lose them.
func (m *Model) applyPkgFilter() {
	needle := m.filterText()
	anchor, had := m.pkgAt(m.pkgCursor)

	if needle == "" {
		m.filteredPkgs = nil
	} else {
		out := make([]int, 0, len(m.packages))
		for i, name := range m.packages {
			if matchFilter(name, needle) {
				out = append(out, i)
			}
		}
		m.filteredPkgs = out
	}

	m.pkgCursor = 0
	if had {
		for v := 0; v < m.pkgCount(); v++ {
			if orig, ok := m.pkgAt(v); ok && orig == anchor {
				m.pkgCursor = v
				break
			}
		}
	}
	m.pkgCursor = wrap(m.pkgCursor, m.pkgCount())
}

// reapplyFilter rebuilds whichever list is on screen, after the filter text or
// the underlying data changed.
func (m *Model) reapplyFilter() {
	if m.state == statePackageList {
		m.applyPkgFilter()
		return
	}
	m.applyEnvFilter()
}

// clearFilter drops the filter and un-focuses the box. Used whenever the list
// being filtered is left behind.
func (m *Model) clearFilter() {
	m.filter.SetValue("")
	m.filter.Blur()
	m.filtering = false
	m.filteredItems, m.filteredPkgs = nil, nil
}

// closeFilter closes the box, keeping the filter unless clear is set.
func (m *Model) closeFilter(clear bool) {
	if clear {
		m.filter.SetValue("")
	}
	m.filter.Blur()
	m.filtering = false
	m.reapplyFilter()
}

// openFilter gives the box focus without disturbing the filter already in it, so
// f twice returns to what was typed rather than starting over.
func (m *Model) openFilter() {
	m.filtering = true
	m.filter.Width = m.filterWidth()
	m.filter.Focus()
}

// filterWidth is the room the box has on the status row, prompt and cursor
// included. That row is a fixed one line tall, so the text has to fit it rather
// than wrap onto a second.
func (m Model) filterWidth() int {
	if w := m.width - lipgloss.Width(m.filter.Prompt) - 2; w > 8 {
		return w
	}
	return 8
}

// handleFilterKey routes keys to the box while it has focus. Up and down still
// move the list, so matches can be stepped through without leaving the box.
func (m Model) handleFilterKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case msg.String() == "ctrl+c":
		return m, tea.Quit

	case key.Matches(msg, keys.Cancel):
		// esc clears as well as closes. Closing alone would leave rows hidden by
		// a filter the user can no longer see, with nothing on screen to explain
		// where they went.
		m.closeFilter(true)
		return m, nil

	case msg.String() == "enter":
		m.closeFilter(false)
		return m, nil

	// The arrows only, not the j/k aliases the lists use: inside a text box "k"
	// is a letter, and a filter for "keras" must not walk the cursor upwards.
	case msg.Type == tea.KeyUp, msg.Type == tea.KeyDown:
		dir := 1
		if msg.Type == tea.KeyUp {
			dir = -1
		}
		if m.state == statePackageList {
			if n := m.pkgCount(); n > 0 {
				m.pkgCursor = wrap(m.pkgCursor+dir, n)
			}
			return m, nil
		}
		m.moveCursor(dir)
		return m, m.triggerDetailLoad()
	}

	var cmd tea.Cmd
	m.filter, cmd = m.filter.Update(msg)
	m.reapplyFilter()
	return m, cmd
}

// filterLine renders the box in the row the key hints normally occupy, so
// opening it does not change the height of the list above it.
func (m Model) filterLine() string {
	f := m.filter
	// Measured again here rather than trusting the width set when the box
	// opened, so a resize while it is open cannot push the row onto two lines.
	f.Width = m.filterWidth()
	return statusBarStyle.Width(m.width).Render(" " + f.View())
}

func (m Model) activateCmd(item listItem) tea.Cmd {
	var activate string
	switch {
	case item.envType == "conda" && item.idx < len(m.condaEnvs):
		activate = conda.ActivateCmd(m.condaEnvs[item.idx])
	case item.envType == "uv" && item.idx < len(m.uvEnvs):
		activate = uv.ActivateCmd(m.uvEnvs[item.idx])
	default:
		return nil
	}

	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		if os.Getenv("PSModulePath") != "" {
			cmd = exec.Command("powershell", "-NoExit", "-ExecutionPolicy", "Bypass", "-Command", activate)
		} else {
			cmd = exec.Command("cmd", "/K", activate)
		}
	default:
		shell := os.Getenv("SHELL")
		if shell == "" {
			shell = "bash"
		}
		shellName := filepath.Base(shell)
		// Run activation in a non-interactive shell (avoids loading .zshrc/.bashrc twice),
		// then exec an interactive shell so the user keeps their prompt/config.
		if item.envType == "conda" {
			hook := fmt.Sprintf(`eval "$(conda shell.%s hook)"`, shellName)
			cmd = exec.Command(shell, "-c", hook+"; "+activate+"; exec "+shell+" -i")
		} else {
			cmd = exec.Command(shell, "-c", activate+"; exec "+shell+" -i")
		}
	}

	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		return activationFinishedMsg{err: err}
	})
}

func (m *Model) selectedItem() *listItem {
	list := m.envList()
	if m.cursor >= 0 && m.cursor < len(list) {
		item := list[m.cursor]
		return &item
	}
	return nil
}

func (m Model) triggerDetailLoad() tea.Cmd {
	sel := m.selectedItem()
	if sel == nil || sel.kind == kindHeader {
		return nil
	}
	if sel.envType == "conda" && sel.idx < len(m.condaEnvs) && !m.condaEnvs[sel.idx].Loaded {
		m.state = stateLoadingDetails
		return loadDetailsCmd("conda", sel.idx, m.condaEnvs[sel.idx], uv.Env{})
	}
	if sel.envType == "uv" && sel.idx < len(m.uvEnvs) && !m.uvEnvs[sel.idx].Loaded {
		m.state = stateLoadingDetails
		return loadDetailsCmd("uv", sel.idx, conda.Env{}, m.uvEnvs[sel.idx])
	}
	return nil
}

func (m *Model) resetCreateForm() {
	m.createInputs[0].Reset()
	m.createInputs[1].Reset()
	m.createFocus = 0
	m.createInputs[0].Focus()
	m.createInputs[1].Blur()
	m.statusMsg = ""
}
