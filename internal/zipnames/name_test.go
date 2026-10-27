package zipnames

import (
	"archive/zip"
	"testing"
)

func TestNameEncodingFlag(t *testing.T) {
	for _, tt := range []struct {
		raw   string
		flags uint16
		want  string
	}{
		{"ASCII.nes", 0, "ASCII.nes"},
		{"Mérida.nes", 0x800, "Mérida.nes"},
		{"M" + string([]byte{0x82}) + "rida.nes", 0, "Mérida.nes"},
	} {
		got := Name(&zip.File{FileHeader: zip.FileHeader{Name: tt.raw, Flags: tt.flags}})
		if got != tt.want {
			t.Fatalf("%q != %q", got, tt.want)
		}
	}
}
