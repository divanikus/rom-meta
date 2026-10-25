package scummvm

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func refPacket(value string) string { return fmt.Sprintf("%04x%s", len(value)+4, value) }

func officialRefFixture() string {
	return refPacket("# service=git-upload-pack\n") + "0000" +
		refPacket(strings.Repeat("b", 40)+" HEAD\x00multi_ack symref=HEAD:refs/heads/master\n") +
		refPacket(fixtureRevision+" refs/heads/master\n") +
		refPacket(strings.Repeat("c", 40)+" refs/tags/example\n") + "0000"
}

func TestOfficialGitRefsAvoidRateLimitedAPI(t *testing.T) {
	requests, apiCalls := 0, 0
	base := fixtureClient(t, &requests).Transport
	client := &http.Client{Transport: officialTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() == officialHead {
			apiCalls++
			return &http.Response{StatusCode: 403, Body: io.NopCloser(strings.NewReader("API rate limit exceeded"))}, nil
		}
		if r.URL.String() == officialRefs && r.Header.Get("Accept") != officialRefsMediaType {
			t.Errorf("wrong Git discovery Accept: %s", r.Header.Get("Accept"))
		}
		return base.RoundTrip(r)
	})}
	root := filepath.Join(t.TempDir(), "preview")
	result, err := Import(ImportOptions{DataRoot: root, ListOptions: ListOptions{Client: client}})
	if err != nil || result.Official == nil || result.Official.Revision != fixtureRevision || requests != 5 || apiCalls != 0 {
		t.Fatalf("API-independent import: %+v; err=%v requests=%d API calls=%d", result, err, requests, apiCalls)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatal("online preview wrote files")
	}
}

func TestOfficialRevisionAPIFallback(t *testing.T) {
	for _, failure := range []string{"403", "503", "network", "content type", "malformed"} {
		t.Run(failure, func(t *testing.T) {
			requests, refsCalls := 0, 0
			base := fixtureClient(t, &requests).Transport
			client := &http.Client{Transport: officialTransport(func(r *http.Request) (*http.Response, error) {
				if r.URL.String() != officialRefs {
					return base.RoundTrip(r)
				}
				refsCalls++
				switch failure {
				case "network":
					return nil, errors.New("Git endpoint unavailable")
				case "content type":
					return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(officialRefFixture())), Header: http.Header{"Content-Type": {"text/html"}}}, nil
				case "malformed":
					return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("truncated")), Header: http.Header{"Content-Type": {officialRefsMediaType}}}, nil
				case "403":
					return &http.Response{StatusCode: 403, Body: io.NopCloser(strings.NewReader("forbidden"))}, nil
				default:
					return &http.Response{StatusCode: 503, Body: io.NopCloser(strings.NewReader("unavailable"))}, nil
				}
			})}
			list, err := Load(ListOptions{Client: client})
			if err != nil || list.Official.Revision != fixtureRevision || requests != 5 || refsCalls != 1 {
				t.Fatalf("fallback failed: %+v; err=%v requests=%d refs=%d", list, err, requests, refsCalls)
			}
		})
	}
}

func TestOfficialRevisionBothPathsFail(t *testing.T) {
	requests := 0
	client := &http.Client{Transport: officialTransport(func(*http.Request) (*http.Response, error) {
		requests++
		return &http.Response{StatusCode: 403, Body: io.NopCloser(strings.NewReader("denied"))}, nil
	})}
	_, err := resolveOfficialRevision(context.Background(), client)
	if err == nil || requests != 2 {
		t.Fatalf("bad failure: %v; requests=%d", err, requests)
	}
	for _, fact := range []string{"Git refs:", "GitHub API:", "HTTP 403", "--revision", "--offline"} {
		if !strings.Contains(err.Error(), fact) {
			t.Errorf("missing %s in %v", fact, err)
		}
	}
}

func TestOfficialRevisionRejectsInvalidAPIFallback(t *testing.T) {
	for _, body := range []string{"invalid JSON", `{"sha":"master"}`, `{"sha":"` + strings.Repeat("0", 40) + `"}`} {
		client := &http.Client{Transport: officialTransport(func(r *http.Request) (*http.Response, error) {
			if r.URL.String() == officialRefs {
				return &http.Response{StatusCode: 503, Body: io.NopCloser(strings.NewReader("unavailable"))}, nil
			}
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
		})}
		if _, err := resolveOfficialRevision(context.Background(), client); err == nil || !strings.Contains(err.Error(), "invalid commit") {
			t.Fatalf("bad API response accepted: %s; %v", body, err)
		}
	}
}

func TestOfficialRawFailureDoesNotRediscoverOrMixRevisions(t *testing.T) {
	requests, rawCalls := 0, 0
	base := fixtureClient(t, &requests).Transport
	client := &http.Client{Transport: officialTransport(func(r *http.Request) (*http.Response, error) {
		if strings.HasPrefix(r.URL.String(), officialRaw) {
			rawCalls++
			return &http.Response{StatusCode: 403, Body: io.NopCloser(strings.NewReader("denied"))}, nil
		}
		return base.RoundTrip(r)
	})}
	if _, err := Load(ListOptions{Client: client}); err == nil || !strings.Contains(err.Error(), "HTTP 403") || requests != 1 || rawCalls != 1 {
		t.Fatalf("raw failure should stop the pinned acquisition: %v, requests=%d raw=%d", err, requests, rawCalls)
	}
}

func TestParseOfficialRefs(t *testing.T) {
	if revision, err := parseOfficialRefs([]byte(officialRefFixture())); err != nil || revision != fixtureRevision {
		t.Fatalf("master not resolved: %s, %v", revision, err)
	}
	// Capabilities on master itself, and a missing LF on the service header,
	// are both valid. The exact branch is selected independently of HEAD.
	valid := refPacket("# service=git-upload-pack") + "0000" + refPacket(fixtureRevision+" refs/heads/master\x00multi_ack\n") + "0000"
	if revision, err := parseOfficialRefs([]byte(valid)); err != nil || revision != fixtureRevision {
		t.Fatalf("master capabilities rejected: %s, %v", revision, err)
	}
	prefix := refPacket("# service=git-upload-pack\n") + "0000"
	master := refPacket(fixtureRevision + " refs/heads/master\n")
	for _, contents := range []string{
		"", "<html>blocked</html>", "zzzz", "001e# service=git-upload-pack\n",
		refPacket("# service=git-receive-pack\n") + "0000" + master + "0000",
		refPacket("# service=git-upload-pack\n") + master + "0000",
		prefix + master, prefix + master + "0000extra", prefix + master + "ffffshort",
		prefix + master + master + "0000",
		prefix + refPacket(fixtureRevision+" refs/heads/master-backup\n") + "0000",
		prefix + refPacket(fixtureRevision+" HEAD\x00symref=HEAD:refs/heads/master\n") + "0000",
		prefix + refPacket("invalid refs/heads/master\n") + "0000",
		prefix + refPacket(strings.Repeat("0", 40)+" refs/heads/master\n") + "0000",
		prefix + master + refPacket("bad later ref") + "0000",
	} {
		if revision, err := parseOfficialRefs([]byte(contents)); err == nil {
			t.Errorf("malformed advertisement accepted: %q => %s", contents, revision)
		}
	}
}
