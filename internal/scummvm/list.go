// Package scummvm creates metadata cards from a ScummVM build's supported games.
// It does not scan game files or import detection fingerprints.
package scummvm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"net/http"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf16"
	"unicode/utf8"

	"rommeta/internal/catalogfs"
)

const maxListBytes = 16 << 20

type Game struct {
	ID      string
	Title   string
	Support string
}

// Runner is injectable so executable acquisition can be tested without ScummVM.
type Runner func(context.Context, string, ...string) ([]byte, error)

type ListOptions struct {
	Executable string
	ListFile   string
	Version    string
	Run        Runner
	Release    string
	Revision   string
	Offline    bool
	CacheRoot  string
	Client     *http.Client
}

type List struct {
	Games       []Game
	Version     string
	Contents    []byte
	Official    *OfficialSource
	SourceFiles map[string][]byte
}

// Load defaults to official website data. An executable or saved --list-games
// output remains an explicit alternative.
func Load(options ListOptions) (List, error) {
	local := options.Executable != "" || options.ListFile != "" || options.Run != nil
	if local && (options.Release != "" || options.Revision != "" || options.Offline) {
		return List{}, fmt.Errorf("--release, --revision and --offline are only used with the official online source")
	}
	if !local {
		if options.Version != "" {
			return List{}, fmt.Errorf("--version is only used with --list-file")
		}
		return loadOfficial(options)
	}
	if options.ListFile != "" && options.Executable != "" {
		return List{}, fmt.Errorf("--list-file and --scummvm are mutually exclusive")
	}
	var contents []byte
	version := strings.TrimSpace(options.Version)
	if options.ListFile != "" {
		if version == "" {
			return List{}, fmt.Errorf("--version is required with --list-file (use scummvm --version)")
		}
		info, err := catalogfs.Stat(options.ListFile)
		if err != nil {
			return List{}, err
		}
		if info.Size() > maxListBytes {
			return List{}, fmt.Errorf("ScummVM game list exceeds 16 MiB")
		}
		contents, err = catalogfs.ReadFile(options.ListFile)
		if err != nil {
			return List{}, err
		}
	} else {
		if version != "" {
			return List{}, fmt.Errorf("--version is only used with --list-file")
		}
		executable := options.Executable
		if executable == "" {
			executable = "scummvm"
		}
		run := options.Run
		if run == nil {
			run = runCommand
		}
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		output, err := run(ctx, executable, "--version")
		if err != nil {
			return List{}, fmt.Errorf("run ScummVM --version: %w; install ScummVM or pass --scummvm <executable>", err)
		}
		version = strings.TrimSpace(strings.SplitN(string(output), "\n", 2)[0])
		if !strings.HasPrefix(version, "ScummVM ") {
			return List{}, fmt.Errorf("unexpected ScummVM --version output")
		}
		contents, err = run(ctx, executable, "--list-games")
		if err != nil {
			return List{}, fmt.Errorf("run ScummVM --list-games: %w", err)
		}
	}
	if len(version) > 512 || strings.ContainsAny(version, "\r\n") {
		return List{}, fmt.Errorf("ScummVM version must be a single line of at most 512 bytes")
	}
	games, err := ParseList(contents)
	if err != nil {
		return List{}, err
	}
	return List{Games: games, Version: version, Contents: contents}, nil
}

// ParseList accepts the plain, qualified-ID --list-games format (also UTF-16
// output redirected by Windows PowerShell 5). Reject partial/malformed input.
func ParseList(contents []byte) ([]Game, error) {
	if len(contents) > maxListBytes {
		return nil, fmt.Errorf("ScummVM game list exceeds 16 MiB")
	}
	if len(contents) >= 2 && ((contents[0] == 0xff && contents[1] == 0xfe) || (contents[0] == 0xfe && contents[1] == 0xff)) {
		if len(contents)%2 != 0 {
			return nil, fmt.Errorf("invalid UTF-16 game list")
		}
		var order binary.ByteOrder = binary.LittleEndian
		if contents[0] == 0xfe {
			order = binary.BigEndian
		}
		units := make([]uint16, (len(contents)-2)/2)
		for i := range units {
			units[i] = order.Uint16(contents[2+i*2:])
		}
		contents = []byte(string(utf16.Decode(units)))
	}
	contents = bytes.TrimPrefix(contents, []byte{0xef, 0xbb, 0xbf})
	if !utf8.Valid(contents) {
		return nil, fmt.Errorf("game list must be UTF-8 or BOM-marked UTF-16")
	}
	seen := map[string]string{}
	scanner := bufio.NewScanner(bytes.NewReader(contents))
	scanner.Buffer(make([]byte, 4096), 1<<20)
	line := 0
	for scanner.Scan() {
		line++
		text := strings.TrimSpace(scanner.Text())
		if text == "" || strings.HasPrefix(text, "Game ID ") || onlyDashes(text) {
			continue
		}
		fields := strings.Fields(text)
		if len(fields) < 2 || !qualifiedID.MatchString(fields[0]) || len(fields[0]) > 100 {
			return nil, fmt.Errorf("invalid ScummVM game list at line %d: expected engine:gameid and title", line)
		}
		id := fields[0]
		title := strings.TrimSpace(text[len(id):])
		if strings.IndexFunc(title, func(r rune) bool { return r < 32 }) >= 0 {
			return nil, fmt.Errorf("invalid control character in title at line %d", line)
		}
		if previous, ok := seen[id]; ok && previous != title {
			return nil, fmt.Errorf("conflicting titles for ScummVM ID %s", id)
		}
		seen[id] = title
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(seen) == 0 {
		return nil, fmt.Errorf("ScummVM game list has no qualified game IDs")
	}
	games := make([]Game, 0, len(seen))
	for id, title := range seen {
		games = append(games, Game{ID: id, Title: title})
	}
	sort.Slice(games, func(i, j int) bool { return games[i].ID < games[j].ID })
	return games, nil
}

var qualifiedID = regexp.MustCompile(`^[a-zA-Z0-9_-]+:[a-zA-Z0-9_.+-]+$`)
var engineID = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

func onlyDashes(text string) bool {
	return strings.Trim(text, "- ") == ""
}

type limitedOutput struct{ bytes.Buffer }

func (output *limitedOutput) Write(p []byte) (int, error) {
	if output.Len()+len(p) > maxListBytes {
		return 0, fmt.Errorf("ScummVM output exceeds 16 MiB")
	}
	return output.Buffer.Write(p)
}

func runCommand(ctx context.Context, executable string, args ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, executable, args...)
	output := &limitedOutput{}
	command.Stdout = output
	// Do not combine diagnostics with the machine-readable list.
	if err := command.Run(); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}
