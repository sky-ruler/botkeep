package layouts

import (
	"os"
	"runtime"
)

// GetHostInfo returns the system hostname or a fallback
func GetHostInfo() string {
	h, err := os.Hostname()
	if err != nil || h == "" {
		return "localhost"
	}
	return h
}

// GetPlatformInfo returns the operating system and architecture (e.g. windows/amd64, linux/arm64)
func GetPlatformInfo() string {
	return runtime.GOOS + "/" + runtime.GOARCH
}

// GetGoVersion returns the active Go runtime version (e.g. go1.24.0)
func GetGoVersion() string {
	return runtime.Version()
}

// AppVersion is the current semantic release version
const AppVersion = "v2.0.0"
