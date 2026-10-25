package dat

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func BenchmarkParseDatafileSpooling(b *testing.B) {
	benchmarks := []struct {
		name        string
		games       int
		romsPerGame int
	}{
		{name: "cartridge-10000x1", games: 10000, romsPerGame: 1},
		{name: "tracks-2000x30", games: 2000, romsPerGame: 30},
	}
	for _, benchmark := range benchmarks {
		b.Run(benchmark.name, func(b *testing.B) {
			path := writeSyntheticDAT(b, benchmark.games, benchmark.romsPerGame)
			info, err := os.Stat(path)
			if err != nil {
				b.Fatal(err)
			}
			b.SetBytes(info.Size())
			b.ReportAllocs()
			b.ReportMetric(float64(benchmark.games), "games/op")
			b.ReportMetric(float64(benchmark.games*benchmark.romsPerGame), "roms/op")
			var liveHeap uint64
			for iteration := 0; iteration < b.N; iteration++ {
				input, err := os.Open(path)
				if err != nil {
					b.Fatal(err)
				}
				spool, err := os.MkdirTemp(b.TempDir(), "spool-")
				if err != nil {
					input.Close()
					b.Fatal(err)
				}
				parsed, err := parseDatafile(input, spool)
				closeErr := input.Close()
				if err != nil {
					b.Fatal(err)
				}
				if closeErr != nil {
					b.Fatal(closeErr)
				}
				if len(parsed.games) != benchmark.games || parsed.romCount != benchmark.games*benchmark.romsPerGame {
					b.Fatalf("unexpected parsed counts: games=%d roms=%d", len(parsed.games), parsed.romCount)
				}
				b.StopTimer()
				runtime.GC()
				var memory runtime.MemStats
				runtime.ReadMemStats(&memory)
				liveHeap = memory.HeapAlloc
				runtime.KeepAlive(parsed)
				b.StartTimer()
			}
			b.ReportMetric(float64(liveHeap), "live-heap-bytes")
		})
	}
}

func writeSyntheticDAT(b *testing.B, games, romsPerGame int) string {
	b.Helper()
	path := filepath.Join(b.TempDir(), fmt.Sprintf("synthetic-%d-%d.dat", games, romsPerGame))
	file, err := os.Create(path)
	if err != nil {
		b.Fatal(err)
	}
	writer := bufio.NewWriterSize(file, 256*1024)
	_, _ = writer.WriteString("<?xml version=\"1.0\"?><datafile><header><name>Synthetic</name></header>\n")
	for game := 0; game < games; game++ {
		_, _ = fmt.Fprintf(writer, "<game name=\"Game %06d (USA)\" id=\"%d\">\n", game, game)
		for rom := 0; rom < romsPerGame; rom++ {
			_, _ = fmt.Fprintf(writer, "<rom name=\"track-%03d.bin\" size=\"1048576\" crc=\"%08x\" sha1=\"%040x\"/>\n", rom, game+rom, game+rom)
		}
		_, _ = writer.WriteString("</game>\n")
	}
	_, _ = writer.WriteString("</datafile>\n")
	if err := writer.Flush(); err != nil {
		file.Close()
		b.Fatal(err)
	}
	if err := file.Close(); err != nil {
		b.Fatal(err)
	}
	return path
}
