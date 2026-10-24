package main

import (
	"cmp"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/table"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// refreshInterval is how often the TUI rescans ports.
const refreshInterval = 2 * time.Second

// tuiHiddenHint is a shorter hiddenHint that fits beside the key help.
const tuiHiddenHint = "run with sudo to see every owner"

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

type model struct {
	table   table.Model
	rows    []row
	hidden  bool
	warning error
	err     error
	loaded  bool
	gen     int
	width   int
	height  int

	showDetails bool // false when the window is too short for the panel
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
	return model{table: table.New(table.WithFocused(true), table.WithStyles(styles))}
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
		switch msg.String() {
		case "q", "esc", "ctrl+c":
			return m, tea.Quit
		case "r":
			return m, scanCmd
		}

	case scanMsg:
		m.gen++
		m.err = msg.err
		if msg.err == nil {
			m.loaded = true
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

// setRows replaces the rows, keeping the cursor on the same port if it is
// still listening.
func (m *model) setRows(rows []row, hidden bool) {
	var selected uint16
	if c := m.table.Cursor(); c >= 0 && c < len(m.rows) {
		selected = m.rows[c].Port
	}
	m.rows, m.hidden = rows, hidden

	trows := make([]table.Row, len(rows))
	cursor := 0
	for i, r := range rows {
		trows[i] = r.cells()
		if r.Port == selected {
			cursor = i
		}
	}
	m.table.SetColumns(columns(rows, m.width))
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
	m.table.SetColumns(columns(m.rows, m.width))
	m.table.SetWidth(m.width)
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
	b.WriteString(titleStyle.Render("mooring") + "  " + dimStyle.Render(m.summary()) + "\n\n")

	switch {
	case m.err != nil:
		b.WriteString(errorStyle.Render(m.err.Error()))
	case !m.loaded:
		b.WriteString(dimStyle.Render("Scanning ports…"))
	case len(m.rows) == 0:
		b.WriteString(dimStyle.Render("Nothing is listening."))
	default:
		b.WriteString(m.table.View())
		if m.showDetails {
			b.WriteString("\n" + m.detailsView())
		}
	}

	footer := dimStyle.Render("↑/↓ move   r refresh   q quit")
	if m.warning != nil {
		footer += "   " + warningStyle.Render(m.warning.Error())
	} else if m.hidden && os.Geteuid() != 0 {
		footer += "   " + warningStyle.Render(tuiHiddenHint)
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

// summary describes what the table shows, such as "8 ports · 2 containers".
func (m model) summary() string {
	if !m.loaded {
		return ""
	}
	ids := make(map[string]bool)
	for _, r := range m.rows {
		if r.Container != nil {
			ids[r.Container.ID] = true
		}
	}
	return plural(len(m.rows), "port") + " · " + plural(len(ids), "container")
}

func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return strconv.Itoa(n) + " " + noun + "s"
}

var _ tea.Model = model{}
