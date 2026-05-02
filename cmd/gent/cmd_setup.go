package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func runSetup() {
	fmt.Println("🔧 Gent Native Library Setup")
	fmt.Println()

	platform := DetectPlatform()
	fmt.Printf("Detected platform: %s\n", platform)

	if !platform.IsSupported() {
		fmt.Println()
		fmt.Println("❌ This platform is not supported. Pre-built libraries are available for:")
		for _, p := range SupportedPlatforms() {
			fmt.Printf("  • %s\n", p)
		}
		fmt.Println()
		fmt.Println("Build from source:")
		fmt.Println("  • libtokenizers: https://github.com/daulet/tokenizers#building-from-source")
		fmt.Println("  • libonnxruntime: https://onnxruntime.ai/docs/build/")
		os.Exit(1)
	}

	if !confirm("Is this correct?", true) {
		fmt.Println("Exiting. Set GOOS/GOARCH environment variables to override detection.")
		os.Exit(0)
	}
	fmt.Println()

	deps := NativeDeps(platform)
	fmt.Println("This tool will install:")
	for _, dep := range deps {
		fmt.Printf("  • %s v%s — %s\n", dep.Name, dep.Version, dep.Description)
	}
	fmt.Println()
	fmt.Printf("Libraries will be installed to ~/.gent/lib/\n")
	fmt.Println()
	if !confirm("Proceed with download?", true) {
		os.Exit(0)
	}
	fmt.Println()

	// Download to temp directory.
	tmpDir, err := os.MkdirTemp("", "gent-setup-*")
	if err != nil {
		fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	var allFiles []string
	for _, dep := range deps {
		fmt.Printf("Downloading %s v%s... ", dep.Name, dep.Version)
		files, err := DownloadAndExtract(dep.URL, tmpDir, dep.Files)
		if err != nil {
			fmt.Println("❌")
			fatalf("Download failed: %v", err)
		}
		fmt.Printf("✓ (%d file(s))\n", len(files))
		allFiles = append(allFiles, files...)
	}
	fmt.Println()

	// Install to ~/.gent/lib/.
	libDir, err := InstallToUserDir(allFiles)
	if err != nil {
		fatalf("Install failed: %v", err)
	}
	fmt.Printf("✓ Installed to %s/\n", libDir)
	fmt.Println()

	// Update shell profile.
	envLines := EnvLines(libDir)
	profilePath, err := ShellProfile()
	if err != nil {
		fmt.Printf("Could not detect shell profile: %v\n", err)
		printManualEnvInstructions(envLines)
		os.Exit(0)
	}

	fmt.Printf("The following lines need to be added to %s:\n\n", profilePath)
	for _, line := range envLines {
		fmt.Printf("  %s\n", line)
	}
	fmt.Println()

	if confirm(fmt.Sprintf("Add these to %s?", filepath.Base(profilePath)), true) {
		if err := AppendToShellProfile(profilePath, envLines); err != nil {
			fatalf("Failed to update %s: %v", profilePath, err)
		}
		fmt.Printf("✓ Updated %s\n", profilePath)
		fmt.Println()
		fmt.Printf("⚠  Run 'source %s' or restart your terminal for changes to take effect.\n",
			profilePath)
		fmt.Println()
		fmt.Println("After that, you can:")
	} else {
		fmt.Println()
		printManualEnvInstructions(envLines)
		fmt.Println("After setting the environment variables:")
	}

	printSetupNextSteps(os.Stdout)
}

func printSetupNextSteps(w io.Writer) {
	fmt.Fprintln(w, "  go build ./...")
	fmt.Fprintln(w, "  go test ./...")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Next step: download an embedding model:")
	fmt.Fprintln(w, "  gent model list")
	fmt.Fprintln(w, "  gent model download multilingual-e5-small")
}

func printManualEnvInstructions(envLines []string) {
	fmt.Println("Add these lines to your shell profile manually:")
	for _, line := range envLines {
		fmt.Printf("  %s\n", line)
	}
	fmt.Println()
}

func confirm(prompt string, defaultYes bool) bool {
	if defaultYes {
		fmt.Printf("%s [Y/n] ", prompt)
	} else {
		fmt.Printf("%s [y/N] ", prompt)
	}
	scanner := bufio.NewScanner(os.Stdin)
	if !scanner.Scan() {
		return defaultYes
	}
	input := strings.TrimSpace(strings.ToLower(scanner.Text()))
	switch input {
	case "":
		return defaultYes
	case "y", "yes":
		return true
	case "n", "no":
		return false
	default:
		return defaultYes
	}
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "Error: "+format+"\n", args...)
	os.Exit(1)
}
