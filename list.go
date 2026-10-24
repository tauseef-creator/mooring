package main

import (
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"
)

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
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintln(tw, strings.Join(columnTitles, "\t"))
	for _, r := range rows {
		cells := r.cells()
		cells[commandColumn] = truncate(cells[commandColumn], maxCommandWidth)
		fmt.Fprintln(tw, strings.Join(cells, "\t"))
	}
	tw.Flush()

	if hidden && os.Geteuid() != 0 {
		fmt.Fprintln(os.Stderr, "\n"+hiddenHint)
	}
	return 0
}
