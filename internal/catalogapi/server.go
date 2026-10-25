// Package catalogapi provides an independent read-only portable catalog
// contract. Consumer DTOs, database access and provider acquisition do not belong here.
package catalogapi

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
	"rommeta/internal/catalogfs"
	"rommeta/internal/exporter"
	"rommeta/internal/httpdocs"
)

const Version = 1
const MaxFile = 512 << 20

type Digest = exporter.FileDigest
type Document struct {
	YAML string `json:"yaml"`
	Path string `json:"path"`
	Digest
	Data json.RawMessage `json:"data"`
}
type Asset struct {
	exporter.CatalogAsset
	DigestSHA string `json:"sha256"`
	ID        string `json:"id"`
	URI       string `json:"uri"`
}
type Card struct {
	Game         Document `json:"game"`
	Fingerprints Document `json:"fingerprints"`
	Assets       []Asset  `json:"assets"`
}
type Input struct {
	Disc    *exporter.DiscObservation `json:"disc,omitempty"`
	Record  exporter.Record           `json:"record"`
	Outer   Digest                    `json:"outer"`
	Payload exporter.PayloadProof     `json:"payload"`
}
type SkippedInput struct {
	ROM    string `json:"rom"`
	Status string `json:"status"`
	Reason string `json:"reason"`
}
type Coverage struct {
	Considered int            `json:"considered"`
	Included   int            `json:"included"`
	Skipped    []SkippedInput `json:"skipped"`
}
type Selection struct {
	MediaMode     string             `json:"media_mode,omitempty"`
	GameBinding   bool               `json:"game_binding,omitempty"`
	Coverage      Coverage           `json:"coverage"`
	Policy        exporter.Selection `json:"policy"`
	SchemaVersion int                `json:"schema_version"`
	Platform      string             `json:"platform"`
	System        string             `json:"system"`
	Inputs        []Input            `json:"inputs"`
	Cards         map[string]Card    `json:"cards"`
	Digest        string             `json:"digest"`
}
type Options struct {
	Export           exporter.Options
	Token            string
	SkipUnmatched    bool
	AllowGameBinding bool
	Media            string
	LookupCacheDir   string
}
type boundFile struct {
	root, relative string
	digest         Digest
	info           os.FileInfo
	strong         bool
}
type Server struct {
	token     string
	selection Selection
	checks    []boundFile
	assets    map[string]boundFile
}

func contained(root, relative string) (string, error) {
	if relative == "" || strings.Contains(relative, "\\") || strings.HasPrefix(relative, "/") {
		return "", fmt.Errorf("invalid relative path")
	}
	for _, p := range strings.Split(relative, "/") {
		if p == "" || p == "." || p == ".." || strings.Contains(p, ":") {
			return "", fmt.Errorf("invalid path component")
		}
	}
	base, err := catalogfs.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	name, err := catalogfs.EvalSymlinks(filepath.Join(base, filepath.FromSlash(relative)))
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(base, name)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes configured root")
	}
	return name, nil
}
func fileDigest(name string, limit int64) (Digest, error) {
	return fileDigestContext(context.Background(), name, limit)
}
func fileDigestContext(ctx context.Context, name string, limit int64) (Digest, error) {
	f, err := catalogfs.Open(name)
	if err != nil {
		return Digest{}, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > limit {
		return Digest{}, fmt.Errorf("not a bounded regular file")
	}
	hash := sha256.New()
	n, err := io.Copy(hash, io.LimitReader(requestReader{ctx, f}, limit+1))
	if err != nil || n != info.Size() {
		return Digest{}, fmt.Errorf("file changed while reading")
	}
	return Digest{Size: n, SHA256: hex.EncodeToString(hash.Sum(nil))}, nil
}
func bind(root, relative string) (boundFile, error) {
	name, err := contained(root, relative)
	if err != nil {
		return boundFile{}, err
	}
	d, err := fileDigest(name, MaxFile)
	info, statErr := catalogfs.Stat(name)
	if err == nil {
		err = statErr
	}
	return boundFile{root: root, relative: relative, digest: d, info: info}, err
}
func (f boundFile) verify() (string, error) {
	name, err := contained(f.root, f.relative)
	if err != nil {
		return "", err
	}
	info, err := catalogfs.Stat(name)
	if err != nil || !info.Mode().IsRegular() || info.Size() != f.digest.Size || !info.ModTime().Equal(f.info.ModTime()) || !catalogfs.SameFile(f.info, info) {
		return "", fmt.Errorf("snapshot file changed")
	}
	if !f.strong {
		return name, nil
	}
	got, err := fileDigest(name, MaxFile)
	if err != nil || got != f.digest {
		return "", fmt.Errorf("configured snapshot changed; restart API after reviewing source")
	}
	return name, nil
}
func yamlValue(n *yaml.Node) (any, error) {
	if n.Kind == yaml.DocumentNode {
		return yamlValue(n.Content[0])
	}
	if n.Kind == yaml.MappingNode {
		m := map[string]any{}
		for i := 0; i < len(n.Content); i += 2 {
			if n.Content[i].Tag != "!!str" {
				return nil, fmt.Errorf("non-string document key")
			}
			v, e := yamlValue(n.Content[i+1])
			if e != nil {
				return nil, e
			}
			m[n.Content[i].Value] = v
		}
		return m, nil
	}
	if n.Kind == yaml.SequenceNode {
		a := []any{}
		for _, c := range n.Content {
			v, e := yamlValue(c)
			if e != nil {
				return nil, e
			}
			a = append(a, v)
		}
		return a, nil
	}
	if n.Kind == yaml.AliasNode {
		return nil, fmt.Errorf("document aliases unsupported")
	}
	if n.Tag == "!!timestamp" {
		return n.Value, nil
	}
	var v any
	err := n.Decode(&v)
	return v, err
}
func document(root, relative string) (Document, boundFile, error) {
	f, err := bind(root, relative)
	f.strong = true
	if err != nil {
		return Document{}, f, err
	}
	name, _ := contained(root, relative)
	content, err := catalogfs.ReadFile(name)
	if err != nil {
		return Document{}, f, err
	}
	check := sha256.Sum256(content)
	if hex.EncodeToString(check[:]) != f.digest.SHA256 {
		return Document{}, f, fmt.Errorf("document changed")
	}
	var node yaml.Node
	if err = yaml.Unmarshal(content, &node); err != nil {
		return Document{}, f, err
	}
	value, err := yamlValue(&node)
	if err != nil {
		return Document{}, f, err
	}
	data, err := json.Marshal(value)
	return Document{YAML: string(content), Path: relative, Digest: f.digest, Data: data}, f, err
}
func New(options Options) (*Server, error) {
	if options.Token != "" && len(options.Token) < 32 {
		return nil, fmt.Errorf("ROMMETA_API_TOKEN must contain at least 32 characters")
	}
	if options.Export.Limit < 0 {
		return nil, fmt.Errorf("API selection limit must be non-negative (0: all)")
	}
	if options.Export.Out == "" {
		options.Export.Out = filepath.Join(os.TempDir(), "rommeta-api-no-output")
	}
	source, err := exporter.Build(options.Export)
	if err != nil {
		return nil, err
	}
	return selectionFromPlan(options, source, nil, nil, nil)
}

func selectionFromPlan(options Options, source *exporter.Plan, observations map[string]exporter.ObservedInput, assetCache map[string]boundFile, documentDigests map[string]string) (*Server, error) {
	server := &Server{token: options.Token, assets: map[string]boundFile{}, selection: Selection{SchemaVersion: Version, Platform: source.Platform, System: source.System, Policy: source.Profile.Selection, Inputs: []Input{}, Cards: map[string]Card{}}}
	server.selection.Coverage = Coverage{Considered: len(source.Records), Skipped: []SkippedInput{}}
	server.selection.MediaMode, server.selection.GameBinding = options.Media, options.AllowGameBinding
	records := []exporter.Record{}
	for _, record := range source.Records {
		if record.Status != "matched" || record.MatchedBy != "hash" && !(options.AllowGameBinding && record.MatchedBy == "game-name") || record.Hidden || len(record.Playlist) > 0 {
			reason := record.Reason
			if reason == "" {
				reason = "requires an exact single-file payload match"
			}
			if !options.SkipUnmatched {
				return nil, fmt.Errorf("API selection %s (%s): %s; narrow selection or explicitly use --skip-unmatched", record.ROM, record.Status, reason)
			}
			server.selection.Coverage.Skipped = append(server.selection.Coverage.Skipped, SkippedInput{record.ROM, record.Status, reason})
			continue
		}
		proof := record.Payload
		optical := record.Disc != nil
		if optical {
			proof = &exporter.PayloadProof{}
		}
		if proof == nil {
			return nil, fmt.Errorf("reader supplied no complete payload proof")
		}
		var outer Digest
		if observations != nil {
			observed, ok := observations[record.ROM]
			if !ok || !exporter.ValidObservation(observed) || !observed.Payload.Equal(*proof) || optical && !reflect.DeepEqual(observed.Disc, record.Disc) {
				return nil, fmt.Errorf("input observation differs")
			}
			outer = observed.Outer
		} else {
			name, err := contained(source.ROMRoot, record.ROM)
			if err != nil {
				return nil, err
			}
			info, err := catalogfs.Stat(name)
			if err != nil || !info.Mode().IsRegular() || info.Size() > 32<<30 {
				return nil, fmt.Errorf("ROM must be a regular file up to 32 GiB")
			}
			outer = Digest{Size: info.Size(), SHA256: proof.SHA256}
			if record.Member != "" {
				outer, err = fileDigest(name, 32<<30)
				if err != nil {
					return nil, err
				}
			}
			server.checks = append(server.checks, boundFile{root: source.ROMRoot, relative: record.ROM, digest: outer, info: info})
		}
		if !optical && (proof.SHA1 != record.SHA1 || proof.Size != record.Size || proof.Member != record.Member) {
			return nil, fmt.Errorf("input proof differs")
		}
		for role, media := range record.Media {
			media.OutputPath = ""
			record.Media[role] = media
		}
		server.selection.Inputs = append(server.selection.Inputs, Input{Record: record, Outer: outer, Payload: *proof, Disc: record.Disc})
		records = append(records, record)
		if _, ok := server.selection.Cards[record.Game]; ok {
			continue
		}
		game, gcheck, err := document(source.DataRoot, record.Game+"/game.yaml")
		if err != nil {
			return nil, err
		}
		fps, fcheck, err := document(source.DataRoot, record.Game+"/fingerprints.yaml")
		if err != nil {
			return nil, err
		}
		if documentDigests != nil && (documentDigests[game.Path] != game.SHA256 || documentDigests[fps.Path] != fps.SHA256) {
			return nil, fmt.Errorf("catalog changed behind cached lookup index; review and restart API")
		}
		server.checks = append(server.checks, gcheck, fcheck)
		server.selection.Cards[record.Game] = Card{Game: game, Fingerprints: fps, Assets: []Asset{}}
	}
	server.selection.Coverage.Included = len(server.selection.Inputs)
	var inventory []exporter.CatalogAsset
	var err error
	if options.Media == "covers" {
		seen := map[string]bool{}
		for _, record := range records {
			media, ok := record.Media["cover"]
			if !ok || seen[media.SourcePath] {
				continue
			}
			seen[media.SourcePath] = true
			inventory = append(inventory, exporter.CatalogAsset{Path: media.SourcePath, Game: record.Game, Kind: "box-front", Source: media.Source, Region: media.Region, Language: media.Language, Variant: media.Variant, Size: media.Size, Style: media.Style, StyleEvidence: media.StyleEvidence, SourceType: media.SourceType})
		}
	} else if options.Media != "none" {
		inventory, err = exporter.InventoryMedia(source.DataRoot, records)
	}
	if err != nil {
		return nil, err
	}
	for _, asset := range inventory {
		bound, cached := assetCache[asset.Path]
		var err error
		if cached {
			_, err = bound.verify()
		} else {
			bound, err = bind(source.DataRoot, asset.Path)
		}
		if err != nil {
			return nil, err
		}
		if assetCache != nil {
			assetCache[asset.Path] = bound
		}
		if bound.digest.Size != asset.Size {
			return nil, fmt.Errorf("asset changed during inventory")
		}
		hash := sha256.Sum256([]byte(asset.Path + "\n" + bound.digest.SHA256))
		id := hex.EncodeToString(hash[:])
		server.assets[id] = bound
		card := server.selection.Cards[asset.Game]
		card.Assets = append(card.Assets, Asset{asset, bound.digest.SHA256, id, "/v1/assets/" + id})
		server.selection.Cards[asset.Game] = card
	}
	bytes, err := json.Marshal(server.selection)
	if err != nil {
		return nil, err
	}
	hash := sha256.Sum256(bytes)
	server.selection.Digest = hex.EncodeToString(hash[:])
	return server, nil
}
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if httpdocs.Handle(w, r, "/openapi.json", "/docs", "RomMeta consumer API v1", func() any { return OpenAPI(false, s.token != "") }) {
		return
	}
	want := "Bearer " + s.token
	if s.token != "" && subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte(want)) != 1 {
		w.Header().Set("WWW-Authenticate", "Bearer")
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "read only", http.StatusMethodNotAllowed)
		return
	}
	if r.URL.RawQuery != "" {
		http.Error(w, "queries unsupported for configured snapshot", http.StatusBadRequest)
		return
	}
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, no-cache")
	switch {
	case r.URL.Path == "/v1/health":
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet {
			_ = json.NewEncoder(w).Encode(map[string]any{"schema_version": Version, "inputs": len(s.selection.Inputs), "cards": len(s.selection.Cards), "digest": s.selection.Digest, "coverage": s.selection.Coverage})
		}
	case r.URL.Path == "/v1/selection":
		for _, f := range s.checks {
			if _, err := f.verify(); err != nil {
				http.Error(w, "source snapshot changed; review and restart API", http.StatusConflict)
				return
			}
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("ETag", `"`+s.selection.Digest+`"`)
		if r.Method == http.MethodGet {
			_ = json.NewEncoder(w).Encode(s.selection)
		}
	case strings.HasPrefix(r.URL.Path, "/v1/assets/"):
		id := strings.TrimPrefix(r.URL.Path, "/v1/assets/")
		bound, ok := s.assets[id]
		if !ok {
			http.NotFound(w, r)
			return
		}
		name, err := bound.verify()
		if err != nil {
			http.Error(w, "asset snapshot changed", http.StatusConflict)
			return
		}
		file, err := catalogfs.Open(name)
		if err != nil {
			http.Error(w, "asset unavailable", http.StatusConflict)
			return
		}
		defer file.Close()
		opened, err := file.Stat()
		if err != nil || !catalogfs.SameFile(bound.info, opened) || opened.Size() != bound.digest.Size || !opened.ModTime().Equal(bound.info.ModTime()) {
			http.Error(w, "asset changed", http.StatusConflict)
			return
		}
		if kind := mime.TypeByExtension(filepath.Ext(name)); kind != "" {
			w.Header().Set("Content-Type", kind)
		}
		w.Header().Set("ETag", `"`+bound.digest.SHA256+`"`)
		http.ServeContent(w, r, filepath.Base(name), time.Time{}, file)
	default:
		http.NotFound(w, r)
	}
}
