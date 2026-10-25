package scummvm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

const officialRefs = officialRepo + ".git/info/refs?service=git-upload-pack"
const officialRefsMediaType = "application/x-git-upload-pack-advertisement"

// Public Git ref discovery avoids the shared anonymous REST API quota. It only
// reads the advertised master SHA: no Git executable, clone or pack download.
func resolveOfficialRevision(ctx context.Context, client *http.Client) (string, error) {
	contents, refsErr := officialGET(ctx, client, officialRefs)
	if refsErr == nil {
		var revision string
		revision, refsErr = parseOfficialRefs(contents)
		if refsErr == nil {
			return revision, nil
		}
	}
	// Some networks block Git endpoints; the existing REST resolver remains a
	// fallback. Neither path changes the commit-pinned raw-file downloads.
	contents, apiErr := officialGET(ctx, client, officialHead)
	if apiErr == nil {
		var head struct {
			SHA string `json:"sha"`
		}
		if err := json.Unmarshal(contents, &head); err == nil && validOfficialRevision(head.SHA) {
			return head.SHA, nil
		}
		apiErr = fmt.Errorf("official ScummVM website returned an invalid commit")
	}
	return "", fmt.Errorf("resolve official ScummVM revision: Git refs: %v; GitHub API: %v; use --revision <full-commit-sha> to skip discovery, or --offline after a successful import", refsErr, apiErr)
}

func validOfficialRevision(revision string) bool {
	return revisionPattern.MatchString(revision) && revision != strings.Repeat("0", 40)
}

// Validate the complete v0 pkt-line advertisement before accepting an exact
// refs/heads/master record. HEAD, tags and similarly named branches cannot win.
func parseOfficialRefs(contents []byte) (string, error) {
	packet, rest, err := officialRefPacket(contents)
	if err != nil || strings.TrimSuffix(packet, "\n") != "# service=git-upload-pack" {
		return "", fmt.Errorf("invalid official ScummVM Git service advertisement")
	}
	packet, rest, err = officialRefPacket(rest)
	if err != nil || packet != "" {
		return "", fmt.Errorf("invalid official ScummVM Git service separator")
	}
	var revision string
	for {
		packet, rest, err = officialRefPacket(rest)
		if err != nil {
			return "", err
		}
		if packet == "" {
			if len(rest) != 0 || revision == "" {
				return "", fmt.Errorf("official ScummVM Git refs do not contain one complete master revision")
			}
			return revision, nil
		}
		line, _, _ := strings.Cut(strings.TrimSuffix(packet, "\n"), "\x00")
		sha, ref, ok := strings.Cut(line, " ")
		if !ok || !revisionPattern.MatchString(sha) || ref == "" {
			return "", fmt.Errorf("invalid official ScummVM Git ref record")
		}
		if ref == "refs/heads/master" {
			if revision != "" || !validOfficialRevision(sha) {
				return "", fmt.Errorf("invalid or duplicate official ScummVM master revision")
			}
			revision = sha
		}
	}
}

func officialRefPacket(contents []byte) (string, []byte, error) {
	if len(contents) >= 4 {
		length, err := strconv.ParseUint(string(contents[:4]), 16, 16)
		if err == nil {
			if length == 0 {
				return "", contents[4:], nil
			}
			if length > 4 && int(length) <= len(contents) {
				return string(contents[4:length]), contents[length:], nil
			}
		}
	}
	return "", nil, fmt.Errorf("invalid or truncated official ScummVM Git ref packet")
}
