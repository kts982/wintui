package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"charm.land/bubbles/v2/progress"
	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// ── Import flow state machine ──────────────────────────────────────

type importState int

const (
	importScanning   importState = iota // scanning Desktop for export files
	importFileSelect                    // multiple files found, pick one
	importReview                        // review packages with selection
	importConfirm                       // confirm before installing
	importInstalling                    // batch installing
	importDone                          // results
)

// importPkg holds a package from the export JSON annotated with
// install-readiness metadata.
type importPkg struct {
	Name    string `json:"name"`
	ID      string `json:"id"`
	Version string `json:"version"`
	Source  string `json:"source"`

	Installed    bool     `json:"-"`
	NonCanonical bool     `json:"-"`
	Collisions   []string `json:"-"` // installed-pkg IDs whose names match this entry's
}

// ── Messages ───────────────────────────────────────────────────────

type importFilesMsg struct {
	files []string
	err   error
}

type importLoadedMsg struct {
	packages []importPkg
	err      error
}

type singleImportInstallDoneMsg struct {
	index  int
	output string
	err    error
}

// ── Import model ───────────────────────────────────────────────────

type importModel struct {
	active     bool
	state      importState
	files      []string
	fileCursor int
	packages   []importPkg
	selected   map[int]bool
	cursor     int
	showAll    bool
	spinner    spinner.Model
	progress   progressBar
	err        error

	// Batch install
	ctx           context.Context
	cancel        context.CancelFunc
	batchCurrent  int
	batchTotal    int
	batchName     string
	batchIDs      []string
	batchSources  []string
	batchVersions []string // parallel to batchIDs; "" means "install latest"
	batchOutputs  []string
	batchErrs     []error
	batchErr      error

	statusMsg string
}

func newImportModel() importModel {
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(accent)
	return importModel{
		selected: make(map[int]bool),
		spinner:  sp,
		progress: newProgressBar(50),
	}
}

// activate puts the importer into its initial scanning state and returns
// the cmd batch the host screen should run. Callers (workspace) embed an
// importModel as a field, call activate when the user invokes import,
// then pipe further tea messages through update until !active.
func (m importModel) activate() (importModel, tea.Cmd) {
	m.active = true
	m.state = importScanning
	m.files = nil
	m.fileCursor = 0
	m.packages = nil
	m.selected = make(map[int]bool)
	m.cursor = 0
	m.showAll = false
	m.err = nil
	m.statusMsg = ""
	m.batchOutputs = nil
	m.batchErrs = nil
	m.batchErr = nil
	m.batchTotal = 0
	m.batchCurrent = 0
	m.progress, _ = m.progress.start()
	m.ctx, m.cancel = context.WithCancel(context.Background())
	return m, tea.Batch(m.spinner.Tick, tickProgress(), scanImportFilesCmd())
}

// blocksGlobalShortcuts returns true while the importer is active so the
// app router stops sending tab/q/etc. through and the import overlay owns
// the foreground (matches the cleanup-tab executing-state contract).
func (m importModel) blocksGlobalShortcuts() bool {
	return m.active
}

// ── File scanning & loading ────────────────────────────────────────

func loadImportFile(path string, installed []Package) ([]importPkg, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	pkgs, err := parseImportData(data)
	if err != nil {
		return nil, err
	}
	for i := range pkgs {
		pkgs[i].NonCanonical = isNonCanonical(pkgs[i].ID)
		pkgs[i].Installed = importPackageInstalled(pkgs[i], installed)
		if !pkgs[i].Installed {
			pkgs[i].Collisions = findNameCollisions(pkgs[i], installed)
		}
	}
	return pkgs, nil
}

// parseImportData decodes either the v1 envelope (top-level JSON object)
// or the legacy flat-array form (top-level JSON array). Dispatch is on
// the first non-whitespace byte so a malformed file gets a clearer error
// than "unknown shape".
func parseImportData(data []byte) ([]importPkg, error) {
	trimmed := bytes.TrimLeft(data, " \t\r\n")
	if len(trimmed) == 0 {
		return nil, fmt.Errorf("empty file")
	}
	switch trimmed[0] {
	case '{':
		var env exportEnvelope
		if err := json.Unmarshal(data, &env); err != nil {
			return nil, fmt.Errorf("invalid envelope JSON: %w", err)
		}
		if env.Version != exportEnvelopeVersion {
			return nil, fmt.Errorf("unsupported export version %d (this build understands version %d)",
				env.Version, exportEnvelopeVersion)
		}
		pkgs := make([]importPkg, len(env.Packages))
		for i, p := range env.Packages {
			pkgs[i] = importPkg{
				Name:    p.Name,
				ID:      p.ID,
				Version: p.Version,
				Source:  p.Source,
			}
		}
		return pkgs, nil
	case '[':
		var pkgs []importPkg
		if err := json.Unmarshal(data, &pkgs); err != nil {
			return nil, fmt.Errorf("invalid JSON array: %w", err)
		}
		return pkgs, nil
	default:
		return nil, fmt.Errorf("expected JSON object or array, got %q", string(trimmed[0]))
	}
}

// normalizePackageName collapses whitespace runs to single spaces, lowercases,
// and trims edges — the comparison key for the conservative name-collision
// detector. Two packages whose normalized names match but IDs differ are
// flagged as a possible collision.
func normalizePackageName(name string) string {
	var b strings.Builder
	prevSpace := true
	for _, r := range strings.ToLower(name) {
		switch r {
		case ' ', '\t', '\r', '\n':
			if !prevSpace {
				b.WriteByte(' ')
			}
			prevSpace = true
		default:
			b.WriteRune(r)
			prevSpace = false
		}
	}
	return strings.TrimRight(b.String(), " ")
}

// findNameCollisions returns IDs of installed packages whose normalized
// names exactly match this import entry's name but whose IDs differ. Empty
// names never collide. Same-ID hits aren't collisions (they're caught by
// importPackageInstalled and surface as `Installed=true` instead).
func findNameCollisions(pkg importPkg, installed []Package) []string {
	if pkg.Name == "" {
		return nil
	}
	want := normalizePackageName(pkg.Name)
	if want == "" {
		return nil
	}
	var hits []string
	for _, ex := range installed {
		if strings.EqualFold(ex.ID, pkg.ID) {
			continue
		}
		if normalizePackageName(ex.Name) == want {
			hits = append(hits, ex.ID)
		}
	}
	return hits
}

func importPackageInstalled(pkg importPkg, installed []Package) bool {
	canonicalPkg := Package{
		Name:    pkg.Name,
		ID:      pkg.ID,
		Version: pkg.Version,
		Source:  resolveImportSource(pkg),
	}
	for _, existing := range installed {
		if strings.EqualFold(existing.ID, pkg.ID) {
			return true
		}
		if !strings.EqualFold(strings.TrimSpace(existing.Name), strings.TrimSpace(pkg.Name)) {
			continue
		}
		if isNonCanonical(existing.ID) && shouldHideNonCanonicalDuplicate(existing, canonicalPkg) {
			return true
		}
	}
	return false
}

func resolveImportSource(pkg importPkg) string {
	if pkg.Source == "winget" || pkg.Source == "msstore" {
		return pkg.Source
	}
	if isNonCanonical(pkg.ID) {
		return ""
	}
	if looksLikeStoreProductID(pkg.ID) {
		return "msstore"
	}
	if strings.Contains(pkg.ID, ".") {
		return "winget"
	}
	return ""
}

func importSourceLabel(pkg importPkg) string {
	if pkg.NonCanonical {
		return ""
	}
	switch resolveImportSource(pkg) {
	case "winget":
		return "winget"
	case "msstore":
		return "msstore"
	default:
		return "default"
	}
}

func looksLikeStoreProductID(id string) bool {
	id = strings.TrimSpace(id)
	if len(id) < 12 || len(id) > 16 {
		return false
	}
	for _, r := range id {
		if !unicode.IsDigit(r) && !unicode.IsUpper(r) {
			return false
		}
	}
	return true
}

// scanImportFilesCmd looks for likely export JSON files in the user's
// Desktop and home directory. It surfaces both wintui-prefixed names
// (what `wintui export` produces) and any plain `*.json` so users who
// renamed their exports can still find them. Hidden files are skipped.
func scanImportFilesCmd() tea.Cmd {
	return func() tea.Msg {
		files, err := scanImportFiles()
		return importFilesMsg{files: files, err: err}
	}
}

func scanImportFiles() ([]string, error) {
	var roots []string
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		roots = append(roots, filepath.Join(home, "Desktop"), home)
	}
	if cwd, err := os.Getwd(); err == nil {
		roots = append(roots, cwd)
	}

	seen := make(map[string]bool)
	var found []string
	for _, root := range roots {
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() || strings.HasPrefix(e.Name(), ".") {
				continue
			}
			lower := strings.ToLower(e.Name())
			if !strings.HasSuffix(lower, ".json") {
				continue
			}
			full := filepath.Join(root, e.Name())
			if seen[full] {
				continue
			}
			seen[full] = true
			// Lightly bias: wintui-prefixed names sort first via the
			// returned slice ordering — they're the most likely candidates.
			if strings.HasPrefix(lower, "wintui") {
				found = append([]string{full}, found...)
			} else {
				found = append(found, full)
			}
		}
	}
	if len(found) == 0 {
		return nil, fmt.Errorf("no JSON files found in Desktop, home, or current directory")
	}
	return found, nil
}

// ── Update ─────────────────────────────────────────────────────────

func (m importModel) update(msg tea.Msg, installed []Package) (importModel, tea.Cmd, bool) {
	if !m.active {
		return m, nil, false
	}

	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch m.state {
		case importScanning:
			if msg.String() == "esc" {
				if m.cancel != nil {
					m.cancel()
				}
				m.active = false
				return m, nil, true
			}
			return m, nil, true

		case importFileSelect:
			switch msg.String() {
			case "up", "k":
				if m.fileCursor > 0 {
					m.fileCursor--
				}
			case "down", "j":
				if m.fileCursor < len(m.files)-1 {
					m.fileCursor++
				}
			case "enter":
				m.state = importScanning
				m.ctx, m.cancel = context.WithCancel(context.Background())
				m.progress, _ = m.progress.start()
				path := m.files[m.fileCursor]
				return m, tea.Batch(m.spinner.Tick, tickProgress(), func() tea.Msg {
					if m.ctx.Err() != nil {
						return importLoadedMsg{err: fmt.Errorf("cancelled")}
					}
					pkgs, err := loadImportFile(path, installed)
					if err != nil {
						return importLoadedMsg{err: err}
					}
					if m.ctx.Err() != nil {
						return importLoadedMsg{err: fmt.Errorf("cancelled")}
					}
					return importLoadedMsg{packages: pkgs}
				}), true
			case "esc":
				m.active = false
				return m, nil, true
			}
			return m, nil, true

		case importReview:
			switch msg.String() {
			case "up", "k":
				m.moveCursor(-1)
			case "down", "j":
				m.moveCursor(1)
			case "space", "x":
				m.toggleCurrentSelection()
			case "a":
				m.toggleAllSelectable()
			case "v":
				if m.skippedCount() > 0 {
					m.showAll = !m.showAll
					m.clampCursor()
				}
			case "enter":
				if m.selectedCount() > 0 {
					m.state = importConfirm
				}
			case "esc":
				m.active = false
				return m, nil, true
			}
			return m, nil, true

		case importConfirm:
			switch msg.String() {
			case "y", "Y":
				m.state = importInstalling
				m.progress, _ = m.progress.start()
				ctx, cancel := context.WithCancel(context.Background())
				m.cancel = cancel
				m.ctx = ctx

				var ids, sources, versions []string
				for i, sel := range m.selected {
					if sel && i < len(m.packages) {
						ids = append(ids, m.packages[i].ID)
						sources = append(sources, resolveImportSource(m.packages[i]))
						// Empty Version means "install latest" — the
						// without-versions export path or a legacy raw
						// array that omitted versions. Pinned versions
						// are honored verbatim.
						versions = append(versions, m.packages[i].Version)
					}
				}
				m.batchIDs = ids
				m.batchSources = sources
				m.batchVersions = versions
				m.batchTotal = len(ids)
				m.batchCurrent = 0
				m.batchOutputs = nil
				m.batchErrs = nil
				m.batchErr = nil
				m.progress.active = false
				m.progress.percent = 0

				if m.batchTotal > 0 {
					m.batchName = m.batchIDs[0]
					return m, tea.Batch(m.spinner.Tick,
						importInstallSingleCmd(ctx, ids[0], sources[0], versions[0], 0)), true
				}
			case "n", "N", "esc":
				m.state = importReview
			}
			return m, nil, true

		case importInstalling:
			if msg.String() == "esc" {
				if m.cancel != nil {
					m.cancel()
				}
				m.state = importDone
				m.statusMsg = warnStyle.Render("Cancelled")
				m.progress = m.progress.stop()
			}
			return m, nil, true

		case importDone:
			if msg.String() == "enter" || msg.String() == "esc" {
				m.active = false
				return m, nil, true
			}
			return m, nil, true
		}
		// Absorb all key events while import is active
		return m, nil, true

	case importFilesMsg:
		if m.state != importScanning {
			return m, nil, true
		}
		m.progress = m.progress.stop()
		if msg.err != nil {
			m.err = msg.err
			m.state = importDone
			return m, nil, true
		}
		m.files = msg.files
		m.fileCursor = 0
		m.state = importFileSelect
		return m, nil, true

	case importLoadedMsg:
		if m.state != importScanning {
			return m, nil, true
		}
		m.progress = m.progress.stop()
		if msg.err != nil {
			m.err = msg.err
			m.state = importDone
			return m, nil, true
		}
		m.packages = msg.packages
		m.selected = make(map[int]bool)
		for i, pkg := range m.packages {
			// Collision rows are listed but not auto-selected — the user
			// must intentionally tick them. Prevents accidental duplicates
			// when the same software is already installed under another ID.
			if !pkg.Installed && !pkg.NonCanonical && len(pkg.Collisions) == 0 {
				m.selected[i] = true
			}
		}
		m.cursor = 0
		m.showAll = false
		m.clampCursor()
		m.state = importReview
		return m, nil, true

	case singleImportInstallDoneMsg:
		if m.state != importInstalling {
			return m, nil, true
		}
		m.batchOutputs = append(m.batchOutputs, msg.output)
		m.batchErrs = append(m.batchErrs, msg.err)
		if msg.err != nil {
			m.batchErr = msg.err
		}
		m.batchCurrent = msg.index + 1
		if m.batchTotal > 0 {
			m.progress.percent = float64(m.batchCurrent) / float64(m.batchTotal)
		}
		if m.batchCurrent < m.batchTotal {
			m.batchName = m.batchIDs[m.batchCurrent]
			return m, importInstallSingleCmd(m.ctx,
				m.batchIDs[m.batchCurrent],
				m.batchSources[m.batchCurrent],
				m.batchVersions[m.batchCurrent],
				m.batchCurrent), true
		}
		m.progress = m.progress.stop()
		m.state = importDone
		cache.invalidate()
		return m, nil, true

	case spinner.TickMsg:
		if m.state == importScanning || m.state == importInstalling {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd, true
		}

	case progressTickMsg:
		if m.state == importScanning || m.state == importInstalling {
			var cmd tea.Cmd
			m.progress, cmd = m.progress.update(msg)
			return m, cmd, true
		}

	case progress.FrameMsg:
		if m.state == importScanning || m.state == importInstalling {
			var cmd tea.Cmd
			m.progress, cmd = m.progress.update(msg)
			return m, cmd, true
		}
	}

	return m, nil, false
}

func importInstallSingleCmd(ctx context.Context, id, source, version string, index int) tea.Cmd {
	return func() tea.Msg {
		// Imported IDs come from the export envelope or legacy raw array —
		// they're complete by construction (and exports resolve truncated
		// IDs upfront), so resolveTruncatedPackage inside the wrapper is
		// a no-op. Version "" means install latest; a non-empty version
		// pins to the exported snapshot.
		out, err := installPackageSourceCtx(ctx, Package{ID: id, Source: source}, version)
		return singleImportInstallDoneMsg{output: out, err: err, index: index}
	}
}

func (m importModel) selectedCount() int {
	count := 0
	for _, v := range m.selected {
		if v {
			count++
		}
	}
	return count
}

func (m importModel) reviewCounts() (installable, installed, nonCanonical, collisions int) {
	for _, pkg := range m.packages {
		switch {
		case pkg.Installed:
			installed++
		case pkg.NonCanonical:
			nonCanonical++
		case len(pkg.Collisions) > 0:
			collisions++
		default:
			installable++
		}
	}
	return installable, installed, nonCanonical, collisions
}

func (m importModel) skippedCount() int {
	_, installed, nonCanonical, _ := m.reviewCounts()
	return installed + nonCanonical
}

func (m importModel) visiblePackageIndices() []int {
	indices := make([]int, 0, len(m.packages))
	for i, pkg := range m.packages {
		if !m.showAll && (pkg.Installed || pkg.NonCanonical) {
			continue
		}
		indices = append(indices, i)
	}
	return indices
}

func (m *importModel) clampCursor() {
	visible := m.visiblePackageIndices()
	if len(visible) == 0 {
		m.cursor = 0
		return
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	if m.cursor >= len(visible) {
		m.cursor = len(visible) - 1
	}
}

func (m *importModel) moveCursor(delta int) {
	if len(m.visiblePackageIndices()) == 0 {
		m.cursor = 0
		return
	}
	m.cursor += delta
	m.clampCursor()
}

func (m importModel) currentVisiblePackageIndex() (int, bool) {
	visible := m.visiblePackageIndices()
	if len(visible) == 0 || m.cursor < 0 || m.cursor >= len(visible) {
		return 0, false
	}
	return visible[m.cursor], true
}

func (m *importModel) toggleCurrentSelection() bool {
	index, ok := m.currentVisiblePackageIndex()
	if !ok {
		return false
	}
	pkg := m.packages[index]
	if pkg.Installed || pkg.NonCanonical {
		return false
	}
	m.selected[index] = !m.selected[index]
	return true
}

func (m *importModel) toggleAllSelectable() {
	installable, _, _, collisions := m.reviewCounts()
	// "All selectable" deliberately includes collision rows — pressing `a`
	// is a permissive bulk action; the user accepts the duplicates risk.
	totalSelectable := installable + collisions
	if totalSelectable == 0 {
		return
	}

	allSelected := true
	for i, pkg := range m.packages {
		if pkg.Installed || pkg.NonCanonical {
			continue
		}
		if !m.selected[i] {
			allSelected = false
			break
		}
	}

	if allSelected {
		m.selected = make(map[int]bool)
		return
	}

	selected := make(map[int]bool, totalSelectable)
	for i, pkg := range m.packages {
		if !pkg.Installed && !pkg.NonCanonical {
			selected[i] = true
		}
	}
	m.selected = selected
}

// ── View ───────────────────────────────────────────────────────────

func (m importModel) view(width, height int) string {
	var b strings.Builder
	b.WriteString("  " + sectionTitleStyle.Render("Import Packages") + "\n\n")

	switch m.state {
	case importScanning:
		fmt.Fprintf(&b, "  %s Scanning for export files...\n\n", m.spinner.View())
		b.WriteString("  " + m.progress.view() + "\n")

	case importFileSelect:
		var lines []string
		for i, f := range m.files {
			cursor := cursorBlankStr
			style := itemStyle
			if i == m.fileCursor {
				cursor = cursorStr
				style = itemActiveStyle
			}
			name := filepath.Base(f)
			parent := helpStyle.Render("  " + filepath.Dir(f))
			lines = append(lines, cursor+style.Render(name)+parent)
		}
		title := fmt.Sprintf("Select an export file (%s found)", pluralize(len(m.files), "file"))
		panelW := min(width-4, 100)
		b.WriteString(renderTitledPanel(title, strings.Join(lines, "\n"), panelW, len(lines), accent) + "\n")
		b.WriteString("  " + helpStyle.Render("↑↓ move · enter open · esc cancel") + "\n")

	case importReview:
		installable, installed, nonCanonical, collisions := m.reviewCounts()
		skipped := installed + nonCanonical
		visible := m.visiblePackageIndices()

		b.WriteString(fmt.Sprintf("  %s\n",
			infoStyle.Render(fmt.Sprintf("%d package(s) in file", len(m.packages)))))
		if installable > 0 && !m.showAll {
			b.WriteString(fmt.Sprintf("  %s\n",
				helpStyle.Render(fmt.Sprintf("Showing %d actionable package(s)", installable))))
		}
		if m.showAll && len(m.packages) > 0 {
			b.WriteString(fmt.Sprintf("  %s\n",
				helpStyle.Render(fmt.Sprintf("Showing all %d package(s)", len(m.packages)))))
		}
		if installed > 0 {
			b.WriteString(fmt.Sprintf("  %s\n",
				helpStyle.Render(fmt.Sprintf("%d already installed (skipped)", installed))))
		}
		if nonCanonical > 0 {
			b.WriteString(fmt.Sprintf("  %s\n",
				warnStyle.Render(fmt.Sprintf("%d non-restorable raw identity (flagged)", nonCanonical))))
		}
		if collisions > 0 {
			b.WriteString(fmt.Sprintf("  %s\n",
				warnStyle.Render(fmt.Sprintf("%d possible name match — review before checking", collisions))))
		}
		selCount := m.selectedCount()
		if selCount > 0 {
			b.WriteString(fmt.Sprintf("  %s\n",
				successStyle.Render(fmt.Sprintf("%d selected for install — press enter to proceed", selCount))))
		}
		if skipped > 0 {
			hint := "Press v to show skipped entries"
			if m.showAll {
				hint = "Press v to focus installable packages"
			}
			b.WriteString(fmt.Sprintf("  %s\n", helpStyle.Render(hint)))
		}
		b.WriteString("\n")

		if len(visible) == 0 {
			switch {
			case len(m.packages) == 0:
				b.WriteString("  " + warnStyle.Render("No packages found in this export.") + "\n")
			case !m.showAll && skipped > 0:
				b.WriteString("  " + warnStyle.Render("Nothing to install from this file.") + "\n")
				b.WriteString("  " + helpStyle.Render("All entries are already installed or non-restorable.") + "\n")
			default:
				b.WriteString("  " + warnStyle.Render("No packages to show.") + "\n")
			}
			break
		}

		maxVisible := max(height-12, 5)
		start, end := scrollWindow(m.cursor, len(visible), maxVisible)

		var rows []string
		for i := start; i < end; i++ {
			index := visible[i]
			pkg := m.packages[index]
			cursor := cursorBlankStr
			style := itemStyle
			if i == m.cursor {
				cursor = cursorStr
				style = itemActiveStyle
			}
			var status string
			switch {
			case pkg.Installed:
				status = chipStyle.Render("[installed]")
			case pkg.NonCanonical:
				status = warnStyle.Render("[raw]      ")
			default:
				status = checkbox(m.selected[index])
			}
			label := pkg.Name + "  " + chipStyle.Render("("+pkg.ID+")")
			if pkg.Version != "" {
				label += "  " + stateStyle.Render(pkg.Version)
			}
			if source := importSourceLabel(pkg); source != "" {
				label += "  " + chipStyle.Render("["+source+"]")
			}
			line := cursor + status + " " + style.Render(label)
			if len(pkg.Collisions) > 0 {
				// Warn-styled outside the row's cursor highlight so the
				// chip stays the same color whether or not the row is focused.
				line += "  " + warnStyle.Render("[name match: "+strings.Join(pkg.Collisions, ", ")+"]")
			}
			rows = append(rows, line)
		}

		title := fmt.Sprintf("Import (%s)", pluralize(len(visible), "package", "packages"))
		panelW := min(width-4, 110)
		b.WriteString(renderTitledPanel(title, strings.Join(rows, "\n"), panelW, len(rows), accent) + "\n")
		b.WriteString("  " + helpStyle.Render("space toggle · a select all · v "+
			func() string {
				if m.showAll {
					return "focus actionable"
				}
				return "show skipped"
			}()+" · enter install · esc cancel") + "\n")

	case importConfirm:
		count := m.selectedCount()
		var modal strings.Builder
		modal.WriteString(sectionTitleStyle.Render("Confirm Import") + "\n")
		modal.WriteString(helpStyle.Render(strings.Repeat("─", 40)) + "\n")
		fmt.Fprintf(&modal, "Install %s from this export?\n\n", pluralize(count, "package"))
		modal.WriteString(helpStyle.Render(
			"winget will run for each package; pre-existing software may be overwritten if it shares an ID.") + "\n\n")
		modal.WriteString(lipgloss.NewStyle().Bold(true).Foreground(accent).Render("y") + " confirm  ·  " +
			lipgloss.NewStyle().Bold(true).Foreground(accent).Render("n") + " cancel")
		style := lipgloss.NewStyle().
			Width(min(width-8, 60)).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(accent).
			Padding(1, 2)
		return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, style.Render(modal.String()))

	case importInstalling:
		if m.batchTotal > 0 {
			b.WriteString(fmt.Sprintf("  %s Installing %d of %d: %s\n\n",
				m.spinner.View(), m.batchCurrent+1, m.batchTotal, m.batchName))
		} else {
			fmt.Fprintf(&b, "  %s Installing...\n\n", m.spinner.View())
		}
		b.WriteString("  " + m.progress.view() + "\n")

	case importDone:
		if m.err != nil {
			b.WriteString("  " + errorStyle.Render("Error: "+m.err.Error()) + "\n")
		} else if m.statusMsg != "" {
			b.WriteString("  " + m.statusMsg + "\n")
		} else if m.batchTotal > 0 {
			successCount, failCount := batchResultCounts(m.batchErrs)
			if failCount == 0 {
				b.WriteString(fmt.Sprintf("  %s\n\n",
					successStyle.Render(fmt.Sprintf("%d package(s) installed from this export.", successCount))))
			} else {
				b.WriteString(fmt.Sprintf("  %s\n\n",
					warnStyle.Render(fmt.Sprintf("Import finished: %d succeeded, %d failed", successCount, failCount))))
			}
			output := formatBatchResults(m.batchIDs, m.batchErrs)
			lines := strings.Split(output, "\n")
			maxLines := height - 8
			if maxLines < 5 {
				maxLines = 5
			}
			if len(lines) > maxLines {
				lines = lines[:maxLines]
				lines = append(lines, helpStyle.Render("  ... (output truncated)"))
			}
			for _, line := range lines {
				b.WriteString(line + "\n")
			}
		} else {
			b.WriteString("  " + successStyle.Render("Import complete!") + "\n")
		}
		b.WriteString("\n  " + helpStyle.Render("Press enter or esc to return to Installed packages") + "\n")
	}

	return b.String()
}
