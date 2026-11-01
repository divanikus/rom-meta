package exporter

import (
	"io"
	"os"
	"path/filepath"
	"rommeta/internal/catalogfstest"
	"testing"
	"time"
)

func TestDirectMediaReaderSupportsLooseAndPackedCatalog(t *testing.T) {
	for _, packed := range []bool{false, true} {
		t.Run(map[bool]string{false: "loose", true: "packed"}[packed], func(t *testing.T) {
			options, game, payload := fixture(t)
			put(t, filepath.Join(options.ROMRoot, "one.z64"), payload)
			if packed {
				catalogfstest.Pack(t, filepath.Dir(game))
			}
			plan, err := Build(options)
			if err != nil {
				t.Fatal(err)
			}
			files, err := PreviewFiles([]*Plan{plan}, true)
			if err != nil {
				t.Fatal(err)
			}
			count := 0
			for _, file := range files {
				if file.Open != nil {
					reader, err := file.Open()
					if err != nil {
						t.Fatal(err)
					}
					data, err := io.ReadAll(reader)
					if err != nil {
						reader.Close()
						t.Fatal(err)
					}
					if err = reader.Close(); err != nil {
						t.Fatal(err)
					}
					if int64(len(data)) != file.Size {
						t.Fatal("media size differs")
					}
					count++
				}
			}
			if count == 0 {
				t.Fatal("no asset readers")
			}
		})
	}
}
func TestDirectMediaReaderChecksSourceBeforeAndAfterReading(t *testing.T) {
	for _, when := range []string{"before", "during"} {
		t.Run(when, func(t *testing.T) {
			options, game, payload := fixture(t)
			put(t, filepath.Join(options.ROMRoot, "one.z64"), payload)
			plan, err := Build(options)
			if err != nil {
				t.Fatal(err)
			}
			media := plan.Records[0].Media["video"]
			path := filepath.Join(game, "media", "gameplay-video.usa.emumovies.mp4")
			if when == "before" {
				put(t, path, []byte("changed size"))
				if reader, err := openMedia(plan, media); err == nil {
					reader.Close()
					t.Fatal("accepted changed source size")
				}
				return
			}
			reader, err := openMedia(plan, media)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = io.ReadAll(reader); err != nil {
				t.Fatal(err)
			}
			stat, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			if err = os.Chtimes(path, stat.ModTime().Add(time.Second*2), stat.ModTime().Add(time.Second*2)); err != nil {
				t.Fatal(err)
			}
			if err = reader.Close(); err == nil {
				t.Fatal("accepted source drift while streaming")
			}
		})
	}
}
