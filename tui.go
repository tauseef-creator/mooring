package main

import (
	"cmp"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/table"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// refreshInterval is how often the TUI rescans ports.
const refreshInterval = 2 * time.Second

// tuiHiddenHint is a shorter hiddenHint that fits beside the key help.
const tuiHiddenHint = "run with sudo to see every owner"

// statusDuration is how long the outcome of an action stays in the footer.
const statusDuration = 4 * time.Second

// Layout constants for the TUI.
const (
	headerLines  = 2 // title line and a blank line
	footerLines  = 2 // blank line and the help line
	cellPadding  = 2 // the table pads each cell with a space on both sides
	minFlexWidth = 8 // narrowest a shrinkable column may become

	detailsLines  = 6 // details panel: 4 lines of content plus its border
	panelChrome   = 4 // horizontal space the panel's border and padding take
	minTableLines = 5 // hide the details panel rather than shrink the table below this
)

// shortIDLen is the container ID length docker ps shows.
const shortIDLen = 12

// shrinkOrder lists the columns that give up width, first to last, when the
// terminal is too narrow.
var shrinkOrder = []int{commandColumn, projectColumn, addressColumn}

var (
	titleStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6"))
	dimStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	warningStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	errorStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	statusStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	boldStyle    = lipgloss.NewStyle().Bold(true)
	panelStyle   = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("8")).Padding(0, 1)
)

// scanMsg carries the result of a background scan.
type scanMsg struct {
	snap snapshot
	err  error
}

// tickMsg asks for the next scan. gen ties it to the scan that scheduled it,
// so a manual refresh does not start a second refresh loop.
type tickMsg struct{ gen int }

// statusMsg reports the outcome of an action in the footer.
type statusMsg struct {
	text string
	err  error
}

// clearStatusMsg hides the status set with the same id.
type clearStatusMsg struct{ id int }

// stopDoneMsg reports that a kill finished.
type stopDoneMsg statusMsg

// clipboardFallbackMsg asks the terminal to copy text, for systems without
// a clipboard tool.
type clipboardFallbackMsg struct{ text string }

type model struct {
	table   table.Model
	all     []row // every listening port from the last scan
	rows    []row // the rows shown: all, narrowed by the filter
	hidden  bool
	warning error
	err     error
	loaded  bool
	gen     int
	width   int
	height  int

	showDetails bool // false when the window is too short for the panel

	filter    textinput.Model
	filtering bool // the filter input has focus

	snap     snapshot // the last scan, used to work out what k stops
	confirm  *target  // waiting for y/n before stopping this
	stopping bool     // a kill is running in the background

	status    string // outcome of the last action, shown in the footer
	statusErr bool
	statusID  int
}

func runTUI() int {
	if _, err := tea.NewProgram(newModel()).Run(); err != nil {
		errorf("%v", err)
		return 1
	}
	return 0
}

func newModel() model {
	styles := table.DefaultStyles()
	styles.Header = styles.Header.Foreground(lipgloss.Color("8"))
	styles.Selected = styles.Selected.Foreground(lipgloss.Color("0")).Background(lipgloss.Color("6"))

	// Move with the arrow keys only: k stops the selected port.
	keys := table.DefaultKeyMap()
	keys.LineUp = key.NewBinding(key.WithKeys("up"))
	keys.LineDown = key.NewBinding(key.WithKeys("down"))

	filter := textinput.New()
	filter.Prompt = "/ "
	filter.Placeholder = "filter by port, command, project, container…"

	return model{
		table:  table.New(table.WithFocused(true), table.WithStyles(styles), table.WithKeyMap(keys)),
		filter: filter,
	}
}

func scanCmd() tea.Msg {
	snap, err := scan()
	return scanMsg{snap: snap, err: err}
}

func tick(gen int) tea.Cmd {
	return tea.Tick(refreshInterval, func(time.Time) tea.Msg { return tickMsg{gen: gen} })
}

func (m model) Init() tea.Cmd {
	return scanCmd
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.layout()
		return m, nil

	case tea.KeyPressMsg:
		if m.confirm != nil {
			return m.updateConfirm(msg)
		}
		if m.filtering {
			return m.updateFilter(msg)
		}
		switch msg.String() {
		case "/":
			m.filtering = true
			m.table.Blur() // so letters go to the filter, not table shortcuts
			return m, m.filter.Focus()
		case "esc":
			if m.filter.Value() != "" {
				m.clearFilter()
				return m, nil
			}
			return m, tea.Quit
		case "q", "ctrl+c":
			return m, tea.Quit
		case "r":
			return m, scanCmd
		case "k":
			return m.startKill()
		case "o":
			if r, ok := m.selected(); ok {
				return m, openCmd(localURL(r))
			}
		case "c":
			if r, ok := m.selected(); ok {
				return m, copyCmd(localURL(r))
			}
		}

	case statusMsg:
		return m, m.setStatus(msg.text, msg.err)

	case clearStatusMsg:
		if msg.id == m.statusID {
			m.status = ""
		}
		return m, nil

	case clipboardFallbackMsg:
		cmd := m.setStatus("Asked the terminal to copy "+msg.text, nil)
		return m, tea.Batch(tea.SetClipboard(msg.text), cmd)

	case stopDoneMsg:
		m.stopping = false
		return m, tea.Batch(m.setStatus(msg.text, msg.err), scanCmd)

	case scanMsg:
		m.gen++
		m.err = msg.err
		if msg.err == nil {
			m.loaded = true
			m.snap = msg.snap
			m.warning = msg.snap.warning
			rows, hidden := buildRows(msg.snap, time.Now())
			m.setRows(rows, hidden)
		}
		return m, tick(m.gen)

	case tickMsg:
		if msg.gen != m.gen {
			return m, nil // superseded by a manual refresh
		}
		return m, scanCmd
	}

	var cmd tea.Cmd
	m.table, cmd = m.table.Update(msg)
	return m, cmd
}

// startKill asks to confirm stopping the selected port.
func (m model) startKill() (tea.Model, tea.Cmd) {
	if m.stopping {
		return m, m.setStatus("", errors.New("still stopping the previous port"))
	}
	r, ok := m.selected()
	if !ok {
		return m, nil
	}
	t, err := findTarget(m.snap, r.Port)
	if err != nil {
		return m, m.setStatus("", err)
	}
	m.confirm = &t
	return m, nil
}

// updateConfirm handles the answer to the kill prompt: y stops the target,
// any other key cancels.
func (m model) updateConfirm(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	t := *m.confirm
	m.confirm = nil
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "y", "Y":
		m.stopping = true
		m.statusID++ // a stop can outlast the status timeout, so do not expire this one
		m.status, m.statusErr = "Stopping "+t.what+"…", false
		return m, func() tea.Msg {
			text, err := t.stop(defaultKillTimeout)
			return stopDoneMsg{text: text, err: err}
		}
	}
	return m, m.setStatus("Cancelled", nil)
}

// setStatus shows text, or err when set, in the footer for statusDuration.
func (m *model) setStatus(text string, err error) tea.Cmd {
	m.statusID++
	m.status, m.statusErr = text, err != nil
	if err != nil {
		m.status = err.Error()
	}
	id := m.statusID
	return tea.Tick(statusDuration, func(time.Time) tea.Msg { return clearStatusMsg{id: id} })
}

func openCmd(u string) tea.Cmd {
	return func() tea.Msg {
		if err := openURL(u); err != nil {
			return statusMsg{err: fmt.Errorf("opening %s: %w", u, err)}
		}
		return statusMsg{text: "Opened " + u}
	}
}

func copyCmd(u string) tea.Cmd {
	return func() tea.Msg {
		if err := copyText(u); err != nil {
			return clipboardFallbackMsg{text: u}
		}
		return statusMsg{text: "Copied " + u}
	}
}

// updateFilter handles a key while the filter input has focus.
func (m model) updateFilter(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "enter":
		m.stopFiltering()
		return m, nil
	case "esc":
		m.clearFilter()
		return m, nil
	case "up":
		m.table.MoveUp(1)
		return m, nil
	case "down":
		m.table.MoveDown(1)
		return m, nil
	}
	var cmd tea.Cmd
	m.filter, cmd = m.filter.Update(msg)
	m.applyFilter()
	return m, cmd
}

func (m *model) stopFiltering() {
	m.filtering = false
	m.filter.Blur()
	m.table.Focus()
}

func (m *model) clearFilter() {
	m.filter.SetValue("")
	m.stopFiltering()
	m.applyFilter()
}

// setRows replaces the scanned rows and reapplies the filter.
func (m *model) setRows(rows []row, hidden bool) {
	m.all, m.hidden = rows, hidden
	m.applyFilter()
}

// applyFilter shows the rows matching the filter, keeping the cursor on the
// same port if it is still shown.
func (m *model) applyFilter() {
	var selected uint16
	if r, ok := m.selected(); ok {
		selected = r.Port
	}
	m.rows = filterRows(m.all, m.filter.Value())

	trows := make([]table.Row, len(m.rows))
	cursor := 0
	for i, r := range m.rows {
		trows[i] = r.cells()
		if r.Port == selected {
			cursor = i
		}
	}
	// Size columns from every row, so they do not jump around while typing.
	m.table.SetColumns(columns(m.all, m.width))
	m.table.SetRows(trows)
	m.table.SetCursor(cursor)
}

// layout fits the table and the details panel to the window.
func (m *model) layout() {
	tableLines := m.height - headerLines - footerLines
	m.showDetails = m.width > panelChrome && tableLines-detailsLines >= minTableLines
	if m.showDetails {
		tableLines -= detailsLines
	}
	m.table.SetColumns(columns(m.all, m.width))
	m.table.SetWidth(m.width)
	m.filter.SetWidth(max(m.width-lipgloss.Width(m.filter.Prompt)-1, 1))
	m.table.SetHeight(max(tableLines, 1))
}

// selected returns the row under the cursor.
func (m model) selected() (row, bool) {
	c := m.table.Cursor()
	if c < 0 || c >= len(m.rows) {
		return row{}, false
	}
	return m.rows[c], true
}

// detailsView renders the panel describing the selected row.
func (m model) detailsView() string {
	r, ok := m.selected()
	if !ok {
		return ""
	}
	var title string
	switch {
	case r.Container != nil:
		c := r.Container
		id := c.ID[:min(len(c.ID), shortIDLen)]
		title = boldStyle.Render(c.Endpoint.Runtime+" container "+c.Name) + "  " +
			dimStyle.Render(fmt.Sprintf("(image %s, id %s)", cmp.Or(c.Image, "?"), id))
	case r.PID == "-":
		title = boldStyle.Render("Process of user "+r.User) + "  " + dimStyle.Render("(run with sudo to see it)")
	default:
		title = boldStyle.Render(r.Command) + "  " + dimStyle.Render(fmt.Sprintf("(PID %s, user %s)", r.PID, r.User))
	}

	lines := []string{
		title,
		dimStyle.Render("path    ") + cmp.Or(r.Dir, "-"),
		dimStyle.Render("branch  ") + cmp.Or(r.Branch, "-"),
		dimStyle.Render("listen  ") + r.Listen,
	}
	clip := lipgloss.NewStyle().MaxWidth(m.width - panelChrome)
	for i, l := range lines {
		lines[i] = clip.Render(l)
	}
	return panelStyle.Width(m.width).Render(strings.Join(lines, "\n"))
}

// columns sizes each column to its widest cell. When the total is wider than
// width, the columns in shrinkOrder give up space, down to minFlexWidth.
func columns(rows []row, width int) []table.Column {
	widths := make([]int, len(columnTitles))
	for i, t := range columnTitles {
		widths[i] = lipgloss.Width(t)
	}
	for _, r := range rows {
		for i, c := range r.cells() {
			widths[i] = max(widths[i], lipgloss.Width(c))
		}
	}

	total := 0
	for _, w := range widths {
		total += w + cellPadding
	}
	for _, i := range shrinkOrder {
		if width <= 0 || total <= width {
			break
		}
		cut := min(total-width, widths[i]-minFlexWidth)
		if cut > 0 {
			widths[i] -= cut
			total -= cut
		}
	}

	cols := make([]table.Column, len(columnTitles))
	for i, t := range columnTitles {
		cols[i] = table.Column{Title: t, Width: widths[i]}
	}
	return cols
}

func (m model) View() tea.View {
	var b strings.Builder
	b.WriteString(titleStyle.Render("mooring") + "  " + dimStyle.Render(m.summary()) + "\n")
	// The second header line holds the filter while it is in use.
	if m.filtering || m.filter.Value() != "" {
		b.WriteString(m.filter.View())
	}
	b.WriteString("\n")

	switch {
	case m.err != nil:
		b.WriteString(errorStyle.Render(m.err.Error()))
	case !m.loaded:
		b.WriteString(dimStyle.Render("Scanning ports…"))
	case len(m.all) == 0:
		b.WriteString(dimStyle.Render("Nothing is listening."))
	case len(m.rows) == 0:
		b.WriteString(dimStyle.Render(fmt.Sprintf("No ports match %q.", m.filter.Value())))
	default:
		b.WriteString(m.table.View())
		if m.showDetails {
			b.WriteString("\n" + m.detailsView())
		}
	}

	var footer string
	switch {
	case m.confirm != nil:
		footer = warningStyle.Render(m.confirm.question() + "  y/n")
	case m.status != "" && m.statusErr:
		footer = errorStyle.Render(m.status)
	case m.status != "":
		footer = statusStyle.Render(m.status)
	default:
		footer = dimStyle.Render(m.help())
		if m.warning != nil {
			footer += "   " + warningStyle.Render(m.warning.Error())
		} else if m.hidden && os.Geteuid() != 0 {
			footer += "   " + warningStyle.Render(tuiHiddenHint)
		}
	}
	if m.width > 0 {
		footer = lipgloss.NewStyle().MaxWidth(m.width).Render(footer) // never wrap
	}
	b.WriteString("\n\n" + footer)

	v := tea.NewView(b.String())
	v.AltScreen = true
	v.WindowTitle = "mooring"
	return v
}

// help lists the keys that work in the current mode.
func (m model) help() string {
	switch {
	case m.filtering:
		return "type to filter   ↑/↓ move   enter done   esc clear"
	case m.filter.Value() != "":
		return "↑/↓ move   / edit filter   esc clear filter   k kill   o open   c copy URL   q quit"
	default:
		return "↑/↓ move   / filter   k kill   o open   c copy URL   r refresh   q quit"
	}
}

// summary describes what the table shows, such as "8 ports · 2 containers",
// or "3 of 8 ports · 1 container" when filtered.
func (m model) summary() string {
	if !m.loaded {
		return ""
	}
	portCount := plural(len(m.all), "port")
	if len(m.rows) != len(m.all) {
		portCount = fmt.Sprintf("%d of %s", len(m.rows), portCount)
	}
	ids := make(map[string]bool)
	for _, r := range m.rows {
		if r.Container != nil {
			ids[r.Container.ID] = true
		}
	}
	return portCount + " · " + plural(len(ids), "container")
}

func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return strconv.Itoa(n) + " " + noun + "s"
}

var _ tea.Model = model{}
