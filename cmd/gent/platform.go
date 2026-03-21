package main

import (
	"fmt"
	"runtime"
)

// Platform represents a detected OS/architecture combination.
type Platform struct {
	OS   string
	Arch string
}

func (p Platform) String() string { return p.OS + "/" + p.Arch }

// DetectPlatform returns the current platform.
func DetectPlatform() Platform { return Platform{OS: runtime.GOOS, Arch: runtime.GOARCH} }

// supported maps Go's GOOS/GOARCH to archive naming conventions for each native dependency.
var supported = map[Platform]platformArchives{
	{OS: "linux", Arch: "amd64"}: {
		tokenizerArchive: "libtokenizers.linux-amd64.tar.gz",
		onnxArchiveDir:   "onnxruntime-linux-x64",
		libExt:           ".so",
	},
	{OS: "linux", Arch: "arm64"}: {
		tokenizerArchive: "libtokenizers.linux-arm64.tar.gz",
		onnxArchiveDir:   "onnxruntime-linux-aarch64",
		libExt:           ".so",
	},
	{OS: "darwin", Arch: "arm64"}: {
		tokenizerArchive: "libtokenizers.darwin-arm64.tar.gz",
		onnxArchiveDir:   "onnxruntime-osx-arm64",
		libExt:           ".dylib",
	},
	{OS: "darwin", Arch: "amd64"}: {
		tokenizerArchive: "libtokenizers.darwin-x86_64.tar.gz",
		onnxArchiveDir:   "onnxruntime-osx-x86_64",
		libExt:           ".dylib",
	},
}

type platformArchives struct {
	tokenizerArchive string
	onnxArchiveDir   string
	libExt           string
}

// IsSupported returns true if pre-built native libraries are available.
func (p Platform) IsSupported() bool { _, ok := supported[p]; return ok }

// SystemLibDir returns the standard system library directory.
func (p Platform) SystemLibDir() string { return "/usr/local/lib" }

// SupportedPlatforms returns all platforms with pre-built native libraries.
func SupportedPlatforms() []Platform {
	result := make([]Platform, 0, len(supported))
	for p := range supported {
		result = append(result, p)
	}
	return result
}

// NativeDeps returns the native dependencies for the given platform.
func NativeDeps(p Platform) []NativeDep {
	a := supported[p]
	tokenizerURL := fmt.Sprintf(
		"https://github.com/daulet/tokenizers/releases/download/v%s/%s",
		tokenizerVersion, a.tokenizerArchive,
	)
	onnxDir := fmt.Sprintf("%s-%s", a.onnxArchiveDir, onnxRuntimeVersion)
	onnxURL := fmt.Sprintf(
		"https://github.com/microsoft/onnxruntime/releases/download/v%s/%s.tgz",
		onnxRuntimeVersion, onnxDir,
	)
	onnxFiles := []string{fmt.Sprintf("%s/lib/libonnxruntime%s", onnxDir, a.libExt)}
	if p.OS == "linux" {
		onnxFiles = append(onnxFiles,
			fmt.Sprintf("%s/lib/libonnxruntime.so.%s", onnxDir, onnxRuntimeVersion),
		)
	}
	return []NativeDep{
		{
			Name: "libtokenizers", Version: tokenizerVersion, URL: tokenizerURL,
			Files:       []string{"libtokenizers.a"},
			Description: "HuggingFace Rust tokenizer (for daulet/tokenizers Go bindings)",
		},
		{
			Name: "libonnxruntime", Version: onnxRuntimeVersion, URL: onnxURL,
			Files:       onnxFiles,
			Description: "ONNX Runtime (for yalue/onnxruntime_go Go bindings)",
		},
	}
}

// NativeDep describes a native library to download and install.
type NativeDep struct {
	Name, Version, URL, Description string
	Files                           []string
}

// Pinned native library versions. Update when upgrading go.mod dependencies.
const (
	tokenizerVersion   = "1.26.0" // matches daulet/tokenizers in go.mod
	onnxRuntimeVersion = "1.24.4" // compatible with yalue/onnxruntime_go v1.27.0
)
