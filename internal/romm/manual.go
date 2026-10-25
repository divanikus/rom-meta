package romm

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"rommeta/internal/catalogfs"
)

const maxManual = 64 << 20

func manualWritable(before map[string]json.RawMessage) bool {
	var present bool
	_ = json.Unmarshal(before["has_manual"], &present)
	return !present && !fieldLocked(before, "url_manual") && !fieldLocked(before, "path_manual") && rawString(before, "path_manual") == "" && rawString(before, "url_manual") == ""
}
func manualExtension(name string) (string, error) {
	ext := strings.ToLower(filepath.Ext(name))
	if ext != ".pdf" && ext != ".md" && ext != ".txt" {
		return "", fmt.Errorf("stock manual upload supports PDF, Markdown or text only")
	}
	return ext, nil
}
func (c *Client) uploadManual(ctx context.Context, entry Entry) error {
	if entry.Manual == nil {
		return nil
	}
	ext, e := manualExtension(entry.Manual.Path)
	if e != nil {
		return e
	}
	filename := "rommeta-manual" + ext
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, e := writer.CreateFormFile(filename, filename)
	if e != nil {
		return e
	}
	file, e := catalogfs.Open(entry.Manual.Path)
	if e != nil {
		return e
	}
	defer file.Close()
	digest := sha256.New()
	n, e := io.Copy(io.MultiWriter(part, digest), io.LimitReader(file, maxManual+1))
	if e != nil || n != entry.Manual.Size || hex.EncodeToString(digest.Sum(nil)) != entry.Manual.SHA256 {
		return fmt.Errorf("manual source changed during upload")
	}
	if e = writer.Close(); e != nil {
		return e
	}
	return c.request(ctx, http.MethodPost, fmt.Sprintf("/roms/%d/manuals", entry.ROM.ID), writer.FormDataContentType(), &body, nil, filename)
}
func verifyManualResponse(entry Entry, before, after map[string]json.RawMessage) error {
	rom, e := decodeROM(after)
	if e != nil || rom != entry.ROM {
		return fmt.Errorf("manual upload changed inventory identity")
	}
	ext, e := manualExtension(entry.Manual.Path)
	if e != nil {
		return e
	}
	u, e := url.Parse(rawString(after, "path_manual"))
	if e != nil {
		return e
	}
	actual := strings.TrimPrefix(u.Path, "/assets/romm/resources/")
	want := path.Join("roms", strconv.Itoa(entry.ROM.PlatformID), strconv.Itoa(entry.ROM.ID), "manual", strconv.Itoa(entry.ROM.ID)+ext)
	if actual != want {
		return fmt.Errorf("manual upload path was not retained")
	}
	for _, key := range []string{"path_cover_small", "path_cover_large"} {
		if !sameResourcePath(rawString(before, key), rawString(after, key)) {
			return fmt.Errorf("manual upload changed cover")
		}
	}
	for _, key := range []string{"slug", "merged_screenshots"} {
		if !sameOptionalJSON(before[key], after[key]) {
			return fmt.Errorf("manual upload changed protected %s", key)
		}
	}
	left, right := map[string]json.RawMessage{}, map[string]json.RawMessage{}
	for k, v := range before {
		left[k] = v
	}
	for k, v := range after {
		right[k] = v
	}
	for _, key := range []string{"updated_at", "path_cover_small", "path_cover_large", "path_manual", "has_manual", "locked_fields"} {
		delete(left, key)
		delete(right, key)
	}
	if snapshotHash(left) != snapshotHash(right) {
		return fmt.Errorf("manual upload changed protected metadata")
	}
	return nil
}
