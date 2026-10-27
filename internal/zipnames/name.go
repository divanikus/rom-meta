// Package zipnames decodes the ZIP UTF-8 flag and legacy CP437 member names.
package zipnames

import (
	"archive/zip"
	"golang.org/x/text/encoding/charmap"
)

// Name returns a Unicode member name without changing the archive or its bytes.
func Name(f *zip.File) string {
	if f.Flags&0x800 != 0 {
		return f.Name
	}
	n, _ := charmap.CodePage437.NewDecoder().String(f.Name)
	return n
}
