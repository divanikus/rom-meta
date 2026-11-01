// Package frontenddata carries generated native fields without XML serialization.
package frontenddata

type Entry struct {
	Name   string
	Fields map[string]string
}
type Media struct {
	OutputPath string `json:"output_path"`
}
type Input struct {
	ROM        string           `json:"rom"`
	LaunchFile string           `json:"launch_file"`
	Game       string           `json:"game"`
	Status     string           `json:"status"`
	Hidden     bool             `json:"hidden"`
	Media      map[string]Media `json:"media"`
}
type Version struct {
	Records []int `json:"records"`
}
type Group struct {
	ID, Game string
	Versions []Version
}
type Groups struct{ Groups []Group }
type Selection struct {
	Schema    int         `json:"schema_version"`
	Target    string      `json:"target"`
	System    string      `json:"system"`
	DataRoot  string      `json:"data_root"`
	ROMRoot   string      `json:"rom_root"`
	Records   []Input     `json:"records"`
	LaunchBox *Groups     `json:"launchbox"`
	Systems   []Selection `json:"systems"`
}
type Library struct {
	Selection Selection
	Native    map[string][]Entry
}
