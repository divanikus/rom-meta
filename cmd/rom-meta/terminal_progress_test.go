package main

import (
	"os"
	"strconv"
	"strings"
	"testing"
)

// progressScreen interprets only the cursor/erase operations emitted by this
// renderer, so assertions cover visible rows rather than raw escape spelling.
func progressScreen(t *testing.T, text string) ([]string, int, int) {
	t.Helper()
	rows := [][]rune{{}}
	row, col := 0, 0
	data := []rune(text)
	ensure := func() {
		for row >= len(rows) {
			rows = append(rows, []rune{})
		}
	}
	for i := 0; i < len(data); i++ {
		switch data[i] {
		case '\r':
			col = 0
		case '\n':
			row++
			ensure()
		case '\x1b':
			if i+1 >= len(data) || data[i+1] != '[' {
				t.Fatal("unexpected escape")
			}
			j := i + 2
			for j < len(data) && data[j] >= '0' && data[j] <= '9' {
				j++
			}
			if j >= len(data) {
				t.Fatal("truncated escape")
			}
			n, _ := strconv.Atoi(string(data[i+2 : j]))
			switch data[j] {
			case 'K':
				if n != 2 {
					t.Fatal("unsupported erase")
				}
				rows[row] = nil
			case 'A':
				row -= n
				if row < 0 {
					t.Fatal("cursor above owned rows")
				}
			default:
				t.Fatal("unsupported cursor operation", string(data[j]))
			}
			i = j
		default:
			ensure()
			for len(rows[row]) <= col {
				rows[row] = append(rows[row], ' ')
			}
			rows[row][col] = data[i]
			col++
		}
	}
	result := make([]string, len(rows))
	for i, line := range rows {
		result[i] = strings.TrimRight(string(line), " ")
	}
	return result, row, col
}

func TestProgressRendererClaimsRowAcrossIndependentStages(t *testing.T) {
	for _, finish := range []bool{false, true} {
		t.Run(strconv.FormatBool(finish), func(t *testing.T) {
			file, err := os.CreateTemp(t.TempDir(), "terminal-*.txt")
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			old := &terminalProgress{out: file, interactive: true}
			old.render("export write complete / very long previous filename")
			if finish {
				old.Done()
			}
			next := &terminalProgress{out: file, interactive: true}
			next.render("export prepare 1/100")
			next.render("export prepare 32/100")
			data, err := os.ReadFile(file.Name())
			if err != nil {
				t.Fatal(err)
			}
			rows, row, _ := progressScreen(t, string(data))
			expectedRow := 0
			if finish {
				expectedRow = 1
			}
			if row != expectedRow || rows[row] != "export prepare 32/100" {
				t.Fatalf("stages overlap: %#v at %d", rows, row)
			}
			if finish && rows[0] != "export write complete / very long previous filename" {
				t.Fatal("finished stage changed", rows)
			}
		})
	}
}

func TestProgressRendererClearsShorterAndRemovedRows(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "terminal-*.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	p := &terminalProgress{out: file, interactive: true}
	p.renderLines([]string{"a very long task title", "a very long worker filename"})
	p.renderLines([]string{"short"})
	p.Done()
	data, err := os.ReadFile(file.Name())
	if err != nil {
		t.Fatal(err)
	}
	rows, row, col := progressScreen(t, string(data))
	if len(rows) != 2 || rows[0] != "short" || rows[1] != "" || row != 1 || col != 0 {
		t.Fatalf("stale rows/cursor: %#v at %d:%d", rows, row, col)
	}
}
