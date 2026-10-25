package appinfo

const Name = "RomMeta"

// Version is set by release builds with:
// go build -ldflags "-X rommeta/internal/appinfo.Version=<version>"
var Version = "0.0.0-dev"

func DisplayName() string {
	if Version == "" {
		return Name
	}
	return Name + " " + Version
}
