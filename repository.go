package main

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Repository represents a configured repository (local or remote)
type Repository struct {
	Type    string `json:"type"`    // "local" or "ssh"
	Path    string `json:"path"`    // Local path or remote path
	Host    string `json:"host"`    // SSH host (remote only)
	Port    int    `json:"port"`    // SSH port (remote only, default 22)
	User    string `json:"user"`    // SSH user (remote only)
	KeyFile string `json:"key"`     // SSH key path (remote only)
}

// FileSystem interface abstracts local and remote file operations
type FileSystem interface {
	ReadFile(path string) ([]byte, error)
	ReadDir(path string) ([]fs.DirEntry, error)
	Stat(path string) (fs.FileInfo, error)
	Walk(root string, fn filepath.WalkFunc) error
	BasePath() string
	Type() string
	Info() map[string]string
}

// LocalFS implements FileSystem for local repositories
type LocalFS struct {
	basePath string
}

func NewLocalFS(basePath string) *LocalFS {
	return &LocalFS{basePath: basePath}
}

func (l *LocalFS) ReadFile(path string) ([]byte, error) {
	fullPath := filepath.Join(l.basePath, path)
	return os.ReadFile(fullPath)
}

func (l *LocalFS) ReadDir(path string) ([]fs.DirEntry, error) {
	fullPath := filepath.Join(l.basePath, path)
	return os.ReadDir(fullPath)
}

func (l *LocalFS) Stat(path string) (fs.FileInfo, error) {
	fullPath := filepath.Join(l.basePath, path)
	return os.Stat(fullPath)
}

func (l *LocalFS) Walk(root string, fn filepath.WalkFunc) error {
	fullPath := filepath.Join(l.basePath, root)
	return filepath.Walk(fullPath, fn)
}

func (l *LocalFS) BasePath() string {
	return l.basePath
}

func (l *LocalFS) Type() string {
	return "local"
}

func (l *LocalFS) Info() map[string]string {
	return map[string]string{
		"type": "local",
		"path": l.basePath,
	}
}

// ParseRepository parses a repository config value which can be either:
// - a string (legacy local path)
// - an object with type, path, host, etc.
func ParseRepository(name string, raw json.RawMessage) (*Repository, error) {
	// Try to parse as string first (legacy format)
	var pathStr string
	if err := json.Unmarshal(raw, &pathStr); err == nil {
		return &Repository{
			Type: "local",
			Path: pathStr,
		}, nil
	}

	// Parse as object
	var repo Repository
	if err := json.Unmarshal(raw, &repo); err != nil {
		return nil, fmt.Errorf("failed to parse repository %s: %w", name, err)
	}

	// Set defaults
	if repo.Type == "" {
		repo.Type = "local"
	}
	if repo.Type == "ssh" && repo.Port == 0 {
		repo.Port = 22
	}
	if repo.Type == "ssh" && repo.KeyFile == "" {
		repo.KeyFile = "~/.ssh/id_rsa"
	}

	// Expand ~ in key file path
	if repo.KeyFile != "" && strings.HasPrefix(repo.KeyFile, "~") {
		homeDir, err := os.UserHomeDir()
		if err == nil {
			repo.KeyFile = filepath.Join(homeDir, repo.KeyFile[1:])
		}
	}

	// Validate SSH repos
	if repo.Type == "ssh" {
		if repo.Host == "" {
			return nil, fmt.Errorf("repository %s: SSH repo requires 'host'", name)
		}
		if repo.User == "" {
			return nil, fmt.Errorf("repository %s: SSH repo requires 'user'", name)
		}
		if repo.Path == "" {
			return nil, fmt.Errorf("repository %s: SSH repo requires 'path'", name)
		}
	}

	return &repo, nil
}

// GetFileSystem returns a FileSystem for this repository
func (r *Repository) GetFileSystem(sshPool *SSHPool) (FileSystem, error) {
	switch r.Type {
	case "local", "":
		return NewLocalFS(r.Path), nil
	case "ssh":
		return sshPool.GetRemoteFS(r)
	default:
		return nil, fmt.Errorf("unknown repository type: %s", r.Type)
	}
}

// ValidatePath ensures the requested path is within the repository bounds
func ValidatePath(basePath, requestedPath string) (string, error) {
	absBasePath, err := filepath.Abs(basePath)
	if err != nil {
		return "", err
	}

	targetPath := filepath.Join(absBasePath, requestedPath)
	absTargetPath, err := filepath.Abs(targetPath)
	if err != nil {
		return "", err
	}

	// Check if the target path is within the repository
	relPath, err := filepath.Rel(absBasePath, absTargetPath)
	if err != nil || strings.HasPrefix(relPath, "..") {
		return "", fmt.Errorf("path traversal detected: %s", requestedPath)
	}

	return relPath, nil
}
