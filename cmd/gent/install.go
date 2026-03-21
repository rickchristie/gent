package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// InstallToUserDir copies files to ~/.gent/lib/.
func InstallToUserDir(files []string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}
	libDir := filepath.Join(home, ".gent", "lib")
	if err := os.MkdirAll(libDir, 0o755); err != nil {
		return "", fmt.Errorf("cannot create %s: %w", libDir, err)
	}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			return "", fmt.Errorf("read %s: %w", f, err)
		}
		if err := os.WriteFile(filepath.Join(libDir, filepath.Base(f)), data, 0o644); err != nil {
			return "", fmt.Errorf("write: %w", err)
		}
	}
	return libDir, nil
}

// ShellProfile returns the path to the user's shell profile file.
func ShellProfile() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	shell := os.Getenv("SHELL")
	switch {
	case strings.HasSuffix(shell, "/zsh"):
		return filepath.Join(home, ".zshrc"), nil
	case strings.HasSuffix(shell, "/fish"):
		return filepath.Join(home, ".config", "fish", "config.fish"), nil
	default:
		return filepath.Join(home, ".bashrc"), nil
	}
}

// EnvLines returns the export lines needed to make the linker find libraries in libDir.
func EnvLines(libDir string) []string {
	return []string{
		fmt.Sprintf(`export CGO_LDFLAGS="-L%s"`, libDir),
		fmt.Sprintf(`export LD_LIBRARY_PATH="%s:$LD_LIBRARY_PATH"`, libDir),
	}
}

// AppendToShellProfile appends lines guarded by a marker to prevent duplicates.
func AppendToShellProfile(profilePath string, lines []string) error {
	const marker = "# Added by gent setup tool"
	existing, err := os.ReadFile(profilePath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read %s: %w", profilePath, err)
	}
	if strings.Contains(string(existing), marker) {
		return nil
	}
	f, err := os.OpenFile(profilePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open %s: %w", profilePath, err)
	}
	defer f.Close()
	_, err = f.WriteString("\n" + marker + "\n" + strings.Join(lines, "\n") + "\n")
	return err
}
