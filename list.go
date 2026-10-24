package main

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
	"golang.org/x/term"
)

// listGap is the space between columns in the plain table.
const listGap = 3

// runList prints a table of listening ports.
func runList() int {
	snap, err := scan()
	if err != nil {
		errorf("%v", err)
		return 1
	}
	if snap.warning != nil {
		errorf("warning: %v", snap.warning)
	}

	rows, hidden := buildRows(snap, time.Now())
	printTable(os.Stdout, rows, terminalWidth(os.Stdout))

	if hidden && os.Geteuid() != 0 {
		fmt.Fprintln(os.Stderr, "\n"+hiddenHint)
	}
	return 0
}

// terminalWidth returns the width of f when it is a terminal, or 0 when it
// is not (a pipe or a file), meaning lines may be as long as they need.
func terminalWidth(f *os.File) int {
	fd := int(f.Fd())
	if !term.IsTerminal(fd) {
		return 0
	}
	width, _, err := term.GetSize(fd)
	if err != nil {
		return 0
	}
	return width
}

// printTable writes rows as aligned columns. With width > 0, columns shrink
// so each line fits; with 0, lines keep their full length for pipes and files.
func printTable(w io.Writer, rows []row, width int) {
	lines := [][]string{columnTitles}
	for _, r := range rows {
		cells := r.cells()
		cells[commandColumn] = truncate(cells[commandColumn], maxCommandWidth)
		lines = append(lines, cells)
	}
	widths := fitWidths(lines, width, listGap*(len(columnTitles)-1))

	for _, line := range lines {
		var b strings.Builder
		for i, cell := range line {
			cell = ansi.Truncate(cell, widths[i], "…")
			b.WriteString(cell)
			if i < len(line)-1 {
				b.WriteString(strings.Repeat(" ", widths[i]-ansi.StringWidth(cell)+listGap))
			}
		}
		fmt.Fprintln(w, b.String())
	}
}
