package userdatapath

import (
	"fmt"
	"os"
	"path/filepath"
)

const RootDirName = ".stargazer_data"

func Root() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve user home dir: %w", err)
	}

	root := filepath.Join(homeDir, RootDirName)
	if err := os.MkdirAll(root, 0o755); err != nil {
		return "", fmt.Errorf("create root data dir: %w", err)
	}

	return root, nil
}

func EnsureSubdir(name string) (string, error) {
	root, err := Root()
	if err != nil {
		return "", err
	}

	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create subdir %q: %w", name, err)
	}

	return dir, nil
}

func CatalogDBPath(source string) (string, error) {
	dir, err := EnsureSubdir("catalog")
	if err != nil {
		return "", err
	}

	filename := fmt.Sprintf("%s.sqlite", normalizeSource(source))
	return filepath.Join(dir, filename), nil
}

func CachePath(filename string) (string, error) {
	dir, err := EnsureSubdir("cache")
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, filename), nil
}

func ExportPath(filename string) (string, error) {
	dir, err := EnsureSubdir("exports")
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, filename), nil
}

func LogPath(filename string) (string, error) {
	dir, err := EnsureSubdir("logs")
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, filename), nil
}

func normalizeSource(source string) string {
	switch source {
	case "jwst", "JWST":
		return "jwst"
	case "hst", "HST":
		return "hst"
	default:
		return "unknown"
	}
}
