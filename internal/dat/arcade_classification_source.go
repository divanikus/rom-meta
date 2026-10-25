package dat

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"rommeta/internal/catalogfs"
)

// ArcadeClassificationSource identifies the actual bytes used by this run.
// Acquisition details do not change the durable content-based snapshot identity.
type ArcadeClassificationSource struct {
	Version string `json:"version"`
	Origin  string `json:"origin"`
	Path    string `json:"path"`
	URL     string `json:"url,omitempty"`
	SHA256  string `json:"sha256"`
}

type arcadeClassificationCache struct {
	Version string `json:"version"`
	URL     string `json:"url"`
	SHA256  string `json:"sha256"`
}

const maxArcadeClassificationDownload = 16 << 20

func arcadeClassificationURL(version string) string {
	query := url.Values{
		"tipo": {"category"},
		"file": {"/support/packs/pS_category_" + strings.TrimPrefix(version, "0.") + ".zip"},
	}
	return "https://www.progettosnaps.net/download/?" + query.Encode()
}

func arcadeClassificationCacheDir(root, version string) string {
	return filepath.Join(root, ".rommeta", "cache", "arcade", "mame", version)
}

func checkedArcadeClassification(path, version, origin, sourceURL, expectedHash string) (ArcadeClassificationSource, error) {
	result := ArcadeClassificationSource{Version: version, Origin: origin, Path: path, URL: sourceURL}
	hash, err := arcadeFileHash(path)
	if err != nil {
		return result, err
	}
	if expectedHash != "" && hash != expectedHash {
		return result, fmt.Errorf("classification SHA256 mismatch: %s", path)
	}
	if _, err = readArcadeClassification(path, version); err != nil {
		return result, err
	}
	after, err := arcadeFileHash(path)
	if err != nil {
		return result, err
	}
	if after != hash {
		return result, fmt.Errorf("classification changed while being validated: %s", path)
	}
	result.SHA256 = hash
	return result, nil
}

func resolveArcadeClassification(version, sourceHash string, options ArcadeOptions) (ArcadeClassificationSource, error) {
	var empty ArcadeClassificationSource
	// Reject an unvalidated version before it can reach a URL or filesystem path.
	if arcadeVersionRE.FindString(version) != version {
		return empty, fmt.Errorf("invalid MAME classification version %q", version)
	}
	if strings.TrimSpace(options.ClassificationPath) != "" {
		path, err := filepath.Abs(options.ClassificationPath)
		if err != nil {
			return empty, err
		}
		return checkedArcadeClassification(path, version, "explicit", "", "")
	}
	// A previously accepted import pins its classification, even if the site's
	// archive for the same version is revised or the disposable cache is removed.
	if source, found, err := savedArcadeClassification(options.DataRoot, version, sourceHash); found || err != nil {
		return source, err
	}
	cacheDir := arcadeClassificationCacheDir(options.DataRoot, version)
	sourceURL := arcadeClassificationURL(version)
	if _, err := catalogfs.Stat(cacheDir); err == nil {
		return readArcadeClassificationCache(cacheDir, version, sourceURL)
	} else if !os.IsNotExist(err) {
		return empty, err
	}
	if options.Offline {
		return empty, fmt.Errorf("no local classifications for MAME %s (--offline); run once online or provide --classifications <zip>", version)
	}
	reportProgress(options.Progress, ImportProgress{Stage: "downloading classifications", Item: "Progetto-SNAPS " + version})
	source, err := downloadArcadeClassification(cacheDir, version, sourceURL, options.Client)
	if err != nil {
		return empty, fmt.Errorf("classifications for MAME %s: %w; retry online or supply --classifications <zip> (matching version required)", version, err)
	}
	return source, nil
}

func readArcadeClassificationCache(dir, version, sourceURL string) (ArcadeClassificationSource, error) {
	var meta arcadeClassificationCache
	data, err := catalogfs.ReadFile(filepath.Join(dir, "source.json"))
	if err == nil {
		err = json.Unmarshal(data, &meta)
	}
	if err == nil && (meta.Version != version || meta.URL != sourceURL || len(meta.SHA256) != 64) {
		err = fmt.Errorf("invalid cache manifest")
	}
	var result ArcadeClassificationSource
	if err == nil {
		result, err = checkedArcadeClassification(filepath.Join(dir, "classifications.zip"), version, "cache", sourceURL, meta.SHA256)
	}
	if err != nil {
		return result, fmt.Errorf("invalid classification cache %s: %w; use --classifications <zip> or remove this cache directory to download it again", dir, err)
	}
	return result, nil
}

func downloadArcadeClassification(dir, version, sourceURL string, client *http.Client) (ArcadeClassificationSource, error) {
	var empty ArcadeClassificationSource
	// Keep a bounded request even when a caller supplies its own transport.
	configured := http.Client{Timeout: 60 * time.Second}
	if client != nil {
		configured = *client
		if configured.Timeout == 0 {
			configured.Timeout = 60 * time.Second
		}
	}
	previousRedirect := configured.CheckRedirect
	configured.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if req.URL.Scheme != "https" {
			return fmt.Errorf("classification download redirected outside HTTPS")
		}
		if len(via) >= 10 {
			return fmt.Errorf("too many classification download redirects")
		}
		if previousRedirect != nil {
			return previousRedirect(req, via)
		}
		return nil
	}
	request, err := http.NewRequest(http.MethodGet, sourceURL, nil)
	if err != nil {
		return empty, err
	}
	request.Header.Set("User-Agent", "RomMeta (MAME classification import)")
	response, err := configured.Do(request)
	if err != nil {
		return empty, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return empty, fmt.Errorf("Progetto-SNAPS returned HTTP %d for %s", response.StatusCode, sourceURL)
	}
	if response.ContentLength > maxArcadeClassificationDownload {
		return empty, fmt.Errorf("classification archive exceeds the 16 MiB limit")
	}
	parent := filepath.Dir(dir)
	if err := catalogfs.MkdirAll(parent, 0o755); err != nil {
		return empty, err
	}
	stage, err := os.MkdirTemp(parent, ".classification-")
	if err != nil {
		return empty, err
	}
	defer catalogfs.RemoveAll(stage)
	archivePath := filepath.Join(stage, "classifications.zip")
	file, err := os.Create(archivePath)
	if err != nil {
		return empty, err
	}
	size, copyErr := io.Copy(file, io.LimitReader(response.Body, maxArcadeClassificationDownload+1))
	closeErr := file.Close()
	if copyErr != nil {
		return empty, copyErr
	}
	if closeErr != nil {
		return empty, closeErr
	}
	if size > maxArcadeClassificationDownload {
		return empty, fmt.Errorf("classification archive exceeds the 16 MiB limit")
	}
	// An HTML error page, wrong-version ZIP or incomplete file is never cached.
	checked, err := checkedArcadeClassification(archivePath, version, "download", sourceURL, "")
	if err != nil {
		return empty, err
	}
	meta, err := json.MarshalIndent(arcadeClassificationCache{Version: version, URL: sourceURL, SHA256: checked.SHA256}, "", "  ")
	if err != nil {
		return empty, err
	}
	if err := catalogfs.WriteFile(filepath.Join(stage, "source.json"), append(meta, '\n'), 0o644); err != nil {
		return empty, err
	}
	if err := catalogfs.Rename(stage, dir); err != nil {
		// A concurrent invocation may have published a complete cache first.
		if existing, readErr := readArcadeClassificationCache(dir, version, sourceURL); readErr == nil {
			return existing, nil
		}
		return empty, err
	}
	checked.Path = filepath.Join(dir, "classifications.zip")
	return checked, nil
}

func savedArcadeClassification(root, version, sourceHash string) (ArcadeClassificationSource, bool, error) {
	var result ArcadeClassificationSource
	base := filepath.Join(root, "bindings", "romsets", "mame")
	entries, err := catalogfs.ReadDir(base)
	if os.IsNotExist(err) {
		return result, false, nil
	}
	if err != nil {
		return result, false, err
	}
	found := false
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), version+"-") {
			continue
		}
		dir := filepath.Join(base, entry.Name())
		data, err := catalogfs.ReadFile(filepath.Join(dir, "_source.yaml"))
		if err != nil {
			return result, false, err
		}
		var meta struct {
			Source          string `yaml:"source"`
			SHA256          string `yaml:"sha256"`
			Classifications struct {
				File   string `yaml:"file"`
				SHA256 string `yaml:"sha256"`
			} `yaml:"classifications"`
		}
		if err := yaml.Unmarshal(data, &meta); err != nil {
			return result, false, err
		}
		if meta.Source != "mame" || meta.SHA256 != sourceHash {
			continue
		}
		if meta.Classifications.File != "classifications.zip" || len(meta.Classifications.SHA256) != 64 {
			return result, false, fmt.Errorf("invalid classification reference in snapshot %s", dir)
		}
		checked, err := checkedArcadeClassification(filepath.Join(dir, meta.Classifications.File), version, "snapshot", "", meta.Classifications.SHA256)
		if err != nil {
			return result, false, err
		}
		if found && result.SHA256 != checked.SHA256 {
			return result, false, fmt.Errorf("multiple saved classifications for this MAME input; select one explicitly with --classifications <zip>")
		}
		result, found = checked, true
	}
	return result, found, nil
}
