package main

import "strings"

// filterRows returns the rows that match every word of query, ignoring case.
// A word matches if it appears anywhere in the row's visible cells, its full
// path, branch or container image. An empty query matches every row.
func filterRows(rows []row, query string) []row {
	terms := strings.Fields(strings.ToLower(query))
	if len(terms) == 0 {
		return rows
	}
	var out []row
	for _, r := range rows {
		if r.matches(terms) {
			out = append(out, r)
		}
	}
	return out
}

func (r row) matches(terms []string) bool {
	fields := append(r.cells(), r.Dir, r.Branch, r.Listen)
	if r.Container != nil {
		fields = append(fields, r.Container.Image)
	}
	text := strings.ToLower(strings.Join(fields, " "))
	for _, t := range terms {
		if !strings.Contains(text, t) {
			return false
		}
	}
	return true
}
