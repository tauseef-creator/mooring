package main

import (
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/tauseef-creator/mooring/internal/containers"
)

func filterFixture() []row {
	return []row{
		{Port: 3000, PID: "100", User: "me", Command: "npm run dev", Project: "shop", Dir: "/home/me/shop", Branch: "main", Listen: "127.0.0.1:3000"},
		{Port: 4442, PID: "-", User: "root", Command: "docker: selenium-hub", Project: "~/nascon", Listen: "0.0.0.0:4442",
			Container: &containers.Container{ID: "abc", Image: "selenium/hub:4.24.0"}},
		{Port: 5173, PID: "200", User: "me", Command: "vite", Project: "blog", Dir: "/home/me/blog", Branch: "feature/Dark-Mode", Listen: "[::1]:5173"},
	}
}

func portsOf(rows []row) []uint16 {
	var out []uint16
	for _, r := range rows {
		out = append(out, r.Port)
	}
	return out
}

func TestFilterRows(t *testing.T) {
	tests := []struct {
		query string
		want  []uint16
	}{
		{"", []uint16{3000, 4442, 5173}},
		{"   ", []uint16{3000, 4442, 5173}},
		{"3000", []uint16{3000}},
		{"NPM", []uint16{3000}},            // case-insensitive
		{"docker 44", []uint16{4442}},      // every word must match
		{"docker vite", nil},               // words in different rows
		{"selenium/hub", []uint16{4442}},   // container image
		{"dark-mode", []uint16{5173}},      // branch
		{"/home/me", []uint16{3000, 5173}}, // full path
		{"::1", []uint16{5173}},            // listen address
		{"nothing-like-this", nil},
	}
	for _, tt := range tests {
		if got := portsOf(filterRows(filterFixture(), tt.query)); !slices.Equal(got, tt.want) {
			t.Errorf("filterRows(%q) = %v, want %v", tt.query, got, tt.want)
		}
	}
}

// filterModel returns a model showing filterFixture.
func filterModel(t *testing.T) model {
	t.Helper()
	m := newModel()
	m, _ = update(t, m, tea.WindowSizeMsg{Width: 120, Height: 30})
	m.loaded = true
	m.setRows(filterFixture(), false)
	return m
}

func typeText(t *testing.T, m model, s string) model {
	t.Helper()
	for _, r := range s {
		m, _ = update(t, m, tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	return m
}

var (
	enterKey = tea.KeyPressMsg{Code: tea.KeyEnter}
	escKey   = tea.KeyPressMsg{Code: tea.KeyEscape}
	downKey  = tea.KeyPressMsg{Code: tea.KeyDown}
)

func TestFilterTyping(t *testing.T) {
	m := typeText(t, filterModel(t), "/")
	if !m.filtering {
		t.Fatal("/ did not start filtering")
	}
	m = typeText(t, m, "me")
	if got := portsOf(m.rows); !slices.Equal(got, []uint16{3000, 5173}) {
		t.Errorf("after typing 'me': %v", got)
	}
	if s := m.summary(); !strings.HasPrefix(s, "2 of 3 ports") {
		t.Errorf("summary = %q", s)
	}

	// q and j are text while filtering, not quit or table navigation.
	m, cmd := update(t, m, tea.KeyPressMsg{Code: 'q', Text: "q"})
	if cmd != nil {
		if _, quit := cmd().(tea.QuitMsg); quit {
			t.Fatal("q quit while filtering")
		}
	}
	if m.filter.Value() != "meq" {
		t.Errorf("filter = %q, want %q", m.filter.Value(), "meq")
	}
	if !strings.Contains(ansi.Strip(m.View().Content), `No ports match "meq".`) {
		t.Error("empty result message missing")
	}
}

func TestFilterKeys(t *testing.T) {
	m := typeText(t, filterModel(t), "/me")

	// Arrow keys move the selection while typing.
	m, _ = update(t, m, downKey)
	if r, _ := m.selected(); r.Port != 5173 {
		t.Errorf("down while filtering selected %d, want 5173", r.Port)
	}

	// Enter keeps the filter and returns to the table.
	m, _ = update(t, m, enterKey)
	if m.filtering || m.filter.Value() != "me" || len(m.rows) != 2 {
		t.Errorf("after enter: filtering=%v filter=%q rows=%d", m.filtering, m.filter.Value(), len(m.rows))
	}

	// The first esc clears the filter instead of quitting...
	m, cmd := update(t, m, escKey)
	if cmd != nil {
		t.Fatal("esc with an active filter returned a command; want it to clear the filter")
	}
	if m.filter.Value() != "" || len(m.rows) != 3 {
		t.Errorf("after esc: filter=%q rows=%d", m.filter.Value(), len(m.rows))
	}
	// ...and the selection stayed on the same port.
	if r, _ := m.selected(); r.Port != 5173 {
		t.Errorf("selection moved to %d after clearing, want 5173", r.Port)
	}

	// The next esc quits.
	if _, cmd := update(t, m, escKey); cmd == nil {
		t.Error("esc without a filter did not quit")
	}
}

func TestFilterEscWhileTypingClears(t *testing.T) {
	m := typeText(t, filterModel(t), "/vite")
	m, _ = update(t, m, escKey)
	if m.filtering || m.filter.Value() != "" || len(m.rows) != 3 {
		t.Errorf("after esc: filtering=%v filter=%q rows=%d", m.filtering, m.filter.Value(), len(m.rows))
	}
}

func TestFilterSurvivesRefresh(t *testing.T) {
	m := typeText(t, filterModel(t), "/me")
	m, _ = update(t, m, enterKey)
	m.setRows(filterFixture(), false) // what a scan does
	if got := portsOf(m.rows); !slices.Equal(got, []uint16{3000, 5173}) {
		t.Errorf("after refresh: %v", got)
	}
}
