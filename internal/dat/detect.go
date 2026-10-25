package dat

import (
	"encoding/xml"
	"fmt"
	"io"
)

// DetectFormat reads only the XML root, using the same archive/member selection
// as the importer. Names and extensions do not determine the import strategy.
func DetectFormat(path, member string) (format string, err error) {
	opened, err := openDAT(path, member)
	if err != nil {
		return "", err
	}
	defer func() {
		if closeErr := opened.Close(); err == nil {
			err = closeErr
		}
	}()
	decoder := xml.NewDecoder(opened.Reader)
	for {
		token, readErr := decoder.Token()
		if readErr == io.EOF {
			return "", fmt.Errorf("DAT has no XML root")
		}
		if readErr != nil {
			return "", readErr
		}
		if start, ok := token.(xml.StartElement); ok {
			return formatFromRoot(start.Name.Local), nil
		}
	}
}
