package main

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"
)

func printChoices(w io.Writer, choices []*gameChoice) {
	for i, g := range choices {
		fmt.Fprintf(w, "  %d) %s / %s\n", i+1, g.Platform, g.Game)
	}
}
func reviewRows(root string, o options, r *discoveryReport, c *discoveryCatalog, ds map[string]reviewDecision, in io.Reader, out io.Writer) error {
	scan := bufio.NewScanner(in)
	scan.Buffer(make([]byte, 4096), 1<<20)
	for _, row := range r.Rows {
		if row.Status != "review" && !(o.ReviewAll && (row.Status == "ready" || row.Status == "excluded")) {
			continue
		}
		choices := row.Candidates
		fmt.Fprintf(out, "\n%s / %s\n%s\n", row.Platform, row.Name, row.Reason)
		fmt.Fprintf(out, "Languages: %s\n", strings.Join(row.Languages, ", "))
		fmt.Fprintf(out, "Published output: %s SHA1=%s size=%d\n", row.File.Kind, row.File.SHA1, row.File.Size)
		for _, link := range row.Links {
			fmt.Fprintf(out, "Source: %v; entry: %v; parent: %v\n", link["source"], link["entry"], link["parent"])
		}
		for {
			printChoices(out, choices)
			fmt.Fprint(out, "number | f search | b [platform/]game | n [platform/]new title | s skip | x exclude | a automatic | q save/quit > ")
			if !scan.Scan() {
				return scan.Err()
			}
			line := strings.TrimSpace(scan.Text())
			if line == "q" {
				return nil
			}
			if line == "s" || line == "" {
				break
			}
			if line == "a" {
				delete(ds, row.ID)
				if err := saveDecisions(root, ds); err != nil {
					return err
				}
				break
			}
			if line == "x" {
				ds[row.ID] = reviewDecision{Exclude: true}
				if err := saveDecisions(root, ds); err != nil {
					return err
				}
				break
			}
			if strings.HasPrefix(line, "f ") {
				choices = c.search(row.Platform, strings.TrimSpace(line[2:]))
				continue
			}
			var g *gameChoice
			if n, e := strconv.Atoi(line); e == nil && n > 0 && n <= len(choices) {
				g = choices[n-1]
			}
			if strings.HasPrefix(line, "b ") || strings.HasPrefix(line, "n ") {
				val := strings.TrimSpace(line[2:])
				p := row.Platform
				if before, after, ok := strings.Cut(val, "/"); ok {
					var err error
					p, err = c.platform(strings.TrimSpace(before))
					if err != nil {
						fmt.Fprintln(out, err)
						continue
					}
					val = strings.TrimSpace(after)
				}
				if err := c.load(p); err != nil {
					return err
				}
				if strings.HasPrefix(line, "b ") {
					g = c.games[gameID(p, val)]
				} else {
					key := safeTitleKey(val)
					e := entry{Platform: p, Game: key, NewGame: &newGame{Title: val}}
					if err := checkNewGame(o, e); err != nil {
						fmt.Fprintln(out, err)
						continue
					}
					if !safeName(key) || strings.HasPrefix(key, ".") || strings.HasPrefix(key, "_") {
						fmt.Fprintln(out, "Choose a nonempty, safe original title")
						continue
					}
					g = &gameChoice{Platform: p, Game: key, Title: val, NewGame: e.NewGame}
					fmt.Fprintln(out, "New original card will contain only this title; metadata enrichment remains a separate step.")
				}
			}
			if g == nil {
				fmt.Fprintln(out, "No such choice or existing game")
				continue
			}
			ds[row.ID] = reviewDecision{Platform: g.Platform, Game: g.Game, NewGame: g.NewGame}
			if err := saveDecisions(root, ds); err != nil {
				return err
			}
			break
		}
	}
	return nil
}
