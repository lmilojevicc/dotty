package cli

import "runtime/debug"

// Version holds the application version shown by the CLI.
var Version string

// SetVersion sets the version, falling back to Go's embedded module version for go install builds.
func SetVersion(v string) {
	info, ok := debug.ReadBuildInfo()
	if !ok || info == nil {
		Version = v
		return
	}
	Version = resolveVersion(v, *info)
}

func resolveVersion(v string, info debug.BuildInfo) string {
	if v != "dev" {
		return v
	}
	if info.Main.Version == "" || info.Main.Version == "(devel)" {
		return v
	}
	return info.Main.Version
}
