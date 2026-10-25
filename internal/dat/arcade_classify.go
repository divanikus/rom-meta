package dat

import (
	"archive/zip"
	"bufio"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"strings"
)

var arcadeVersionRE = regexp.MustCompile("^0\\.[0-9]+")
var arcadeShortNameRE = regexp.MustCompile("^[a-z0-9_]+$")

type arcadeClassification struct {
	arcade     map[string]bool
	other      map[string]bool
	categories map[string]string
}

func readArcadeClassification(path, version string) (arcadeClassification, error) {
	result := arcadeClassification{map[string]bool{}, map[string]bool{}, map[string]string{}}
	z, err := zip.OpenReader(path)
	if err != nil {
		return result, fmt.Errorf("open classifications: %w", err)
	}
	defer z.Close()
	wanted := map[string]bool{"mame.ini": false, "mess.ini": false, "category.ini": false}
	for _, f := range z.File {
		name := strings.ToLower(filepath.Base(strings.ReplaceAll(f.Name, "\\", "/")))
		if _, ok := wanted[name]; !ok {
			continue
		}
		if wanted[name] {
			return result, fmt.Errorf("duplicate classification member %s", name)
		}
		wanted[name] = true
		if f.UncompressedSize64 > 16<<20 {
			return result, fmt.Errorf("classification member too large: %s", name)
		}
		r, err := f.Open()
		if err != nil {
			return result, err
		}
		err = scanArcadeClassification(io.LimitReader(r, (16<<20)+1), name, version, &result)
		closeErr := r.Close()
		if err != nil {
			return result, err
		}
		if closeErr != nil {
			return result, closeErr
		}
	}
	for name, found := range wanted {
		if !found {
			return result, fmt.Errorf("classification ZIP is missing %s", name)
		}
	}
	if len(result.arcade) == 0 || len(result.other) == 0 || len(result.categories) == 0 {
		return result, fmt.Errorf("classification lists must not be empty")
	}
	return result, nil
}

func scanArcadeClassification(r io.Reader, name, version string, c *arcadeClassification) error {
	scanner := bufio.NewScanner(r)
	section := ""
	headerFound := false
	for scanner.Scan() {
		line := strings.TrimSpace(strings.TrimPrefix(scanner.Text(), "\ufeff"))
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, ";") {
			fields := strings.Fields(strings.TrimLeft(line, "; "))
			if len(fields) >= 2 && strings.EqualFold(fields[0], name) {
				if fields[1] != version {
					return fmt.Errorf("%s version %q does not match MAME %s", name, fields[1], version)
				}
				headerFound = true
			}
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = line[1 : len(line)-1]
			continue
		}
		if section == "" || section == "FOLDER_SETTINGS" {
			continue
		}
		if !arcadeShortNameRE.MatchString(line) {
			return fmt.Errorf("invalid set name in %s: %q", name, line)
		}
		switch name {
		case "mame.ini", "mess.ini":
			if section != "ROOT_FOLDER" {
				continue
			}
			if name == "mame.ini" {
				c.arcade[line] = true
			} else {
				c.other[line] = true
			}
		case "category.ini":
			if section == "ROOT_FOLDER" {
				continue
			}
			if previous, ok := c.categories[line]; ok {
				return fmt.Errorf("multiple categories for %s: %s, %s", line, previous, section)
			}
			c.categories[line] = section
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	if !headerFound {
		return fmt.Errorf("%s has no version header", name)
	}
	return nil
}

func classifyArcade(g datGame, c arcadeClassification) (string, string) {
	if g.IsDevice {
		return "support", "device"
	}
	if g.IsBIOS {
		return "support", "bios"
	}
	if strings.EqualFold(g.Runnable, "no") {
		return "excluded", "not runnable"
	}
	a, o := c.arcade[g.Name], c.other[g.Name]
	category := c.categories[g.Name]
	if a == o || category == "" {
		return "review", "missing or contradictory classification"
	}
	if o {
		return "excluded", "home/other system"
	}
	top := strings.SplitN(strings.TrimPrefix(category, "TTL * "), " / ", 2)[0]
	switch top {
	case "Computer", "Computer Graphic Workstation", "Game Console", "Game Console/Computer", "Handheld", "Non Arcade":
		return "review", "arcade list conflicts with home/other category"
	}
	if g.IsMechanical {
		return "excluded", "mechanical"
	}
	if strings.Contains(strings.ToLower(category), "unknown") {
		return "review", "unknown category"
	}
	switch top {
	case "Ball & Paddle", "Climbing", "Driving", "Fighter", "Maze", "MultiGame", "Multiplay", "Music Game",
		"Platform", "Puzzle", "Quiz", "Shooter", "Sports", "Tabletop", "Card Games", "Touchscreen":
		return "eligible", "arcade video game"
	case "Arcade":
		if category == "Arcade / Pinball" {
			return "eligible", "arcade video pinball"
		}
	}
	return "excluded", "deferred category: " + category
}
