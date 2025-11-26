package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// Config represents the configuration file structure
type Config struct {
	Repositories map[string]json.RawMessage `json:"repositories"`
}

// Global state
var (
	repos          map[string]*Repository
	reposMux       sync.RWMutex
	configFilePath string
	sshPool        *SSHPool
)

func main() {
	// Parse command-line flags
	configPath := flag.String("config", "", "Path to config file (default: config.json in executable directory or current directory)")
	flag.Parse()

	// Initialize SSH pool
	sshPool = NewSSHPool()
	defer sshPool.Close()

	// Load configuration
	if err := loadConfig(*configPath); err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	reposMux.RLock()
	if len(repos) == 0 {
		reposMux.RUnlock()
		log.Fatal("No repositories configured. Please add repositories to config.json")
	}
	log.Printf("Loaded %d repositories: %v", len(repos), getRepoNames())
	reposMux.RUnlock()

	// Start config file watcher in background
	go watchConfig()

	// Create MCP server
	s := server.NewMCPServer(
		"multi-repo-server",
		"1.0.0",
		server.WithResourceCapabilities(true, false),
	)

	// Register tools
	registerTools(s)

	// Register resources
	registerResources(s)

	// Start server
	if err := server.ServeStdio(s); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}

// loadConfig loads repository configuration from config.json
func loadConfig(configPath string) error {
	// If no config path specified, look for config.json in standard locations
	if configPath == "" {
		// Try ~/.config/fs-mcp/config.json first (recommended location)
		homeDir, err := os.UserHomeDir()
		if err == nil {
			candidatePath := filepath.Join(homeDir, ".config", "fs-mcp", "config.json")
			if _, err := os.Stat(candidatePath); err == nil {
				configPath = candidatePath
			}
		}

		// Try executable directory
		if configPath == "" {
			exePath, err := os.Executable()
			if err == nil {
				exeDir := filepath.Dir(exePath)
				candidatePath := filepath.Join(exeDir, "config.json")
				if _, err := os.Stat(candidatePath); err == nil {
					configPath = candidatePath
				}
			}
		}

		// Fallback to current directory
		if configPath == "" {
			configPath = "config.json"
		}
	}

	// Make path absolute for file watcher
	absPath, err := filepath.Abs(configPath)
	if err == nil {
		configPath = absPath
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("failed to read config file %s: %w (use -config flag to specify path)", configPath, err)
	}

	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		return fmt.Errorf("failed to parse config file: %w", err)
	}

	// Parse repositories
	newRepos := make(map[string]*Repository)
	for name, raw := range config.Repositories {
		repo, err := ParseRepository(name, raw)
		if err != nil {
			return err
		}
		newRepos[name] = repo
	}

	reposMux.Lock()
	repos = newRepos
	configFilePath = configPath
	reposMux.Unlock()

	log.Printf("Loaded config from: %s", configPath)
	return nil
}

// watchConfig watches the config file for changes and reloads it
func watchConfig() {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Printf("Failed to create file watcher: %v", err)
		return
	}
	defer watcher.Close()

	reposMux.RLock()
	configPath := configFilePath
	reposMux.RUnlock()

	if err := watcher.Add(configPath); err != nil {
		log.Printf("Failed to watch config file: %v", err)
		return
	}

	log.Printf("Watching config file for changes: %s", configPath)

	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}
			// Handle Write and Create events (normal saves)
			if event.Op&(fsnotify.Write|fsnotify.Create) != 0 {
				log.Printf("Config file changed, reloading...")
				if err := reloadConfig(); err != nil {
					log.Printf("Failed to reload config: %v", err)
				} else {
					reposMux.RLock()
					log.Printf("Config reloaded successfully. Repositories: %v", getRepoNames())
					reposMux.RUnlock()
				}
			}
			// Handle Remove and Rename events (atomic saves from editors like vim)
			if event.Op&(fsnotify.Remove|fsnotify.Rename) != 0 {
				log.Printf("Config file removed/renamed, re-adding watch...")
				// Re-add the watch after atomic save
				watcher.Add(configPath)
				// Wait a bit for the file to be fully written
				time.Sleep(50 * time.Millisecond)
				// Reload config
				if err := reloadConfig(); err != nil {
					log.Printf("Failed to reload config after rename: %v", err)
				} else {
					reposMux.RLock()
					log.Printf("Config reloaded successfully after atomic save. Repositories: %v", getRepoNames())
					reposMux.RUnlock()
				}
			}
		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			log.Printf("File watcher error: %v", err)
		}
	}
}

// reloadConfig reloads the configuration from the config file
func reloadConfig() error {
	reposMux.RLock()
	configPath := configFilePath
	reposMux.RUnlock()

	data, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("failed to read config file: %w", err)
	}

	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		return fmt.Errorf("failed to parse config file: %w", err)
	}

	// Parse repositories
	newRepos := make(map[string]*Repository)
	for name, raw := range config.Repositories {
		repo, err := ParseRepository(name, raw)
		if err != nil {
			return err
		}
		newRepos[name] = repo
	}

	reposMux.Lock()
	repos = newRepos
	reposMux.Unlock()

	return nil
}

// getRepoNames returns a list of configured repository names (caller must hold read lock)
func getRepoNames() []string {
	names := make([]string, 0, len(repos))
	for name := range repos {
		names = append(names, name)
	}
	return names
}

func registerTools(s *server.MCPServer) {
	// Note: We don't lock here because tool schemas don't change
	// Tool handlers will check repos dynamically with locking
	reposMux.RLock()
	repoNames := make([]string, 0, len(repos))
	for name := range repos {
		repoNames = append(repoNames, name)
	}
	reposMux.RUnlock()

	// Tool: list_files
	s.AddTool(mcp.Tool{
		Name:        "list_files",
		Description: "List files in a repository directory",
		InputSchema: mcp.ToolInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"repo": map[string]interface{}{
					"type":        "string",
					"description": fmt.Sprintf("Repository name. Available: %s", strings.Join(repoNames, ", ")),
					"enum":        repoNames,
				},
				"path": map[string]interface{}{
					"type":        "string",
					"description": "Path within the repository (default: '.')",
					"default":     ".",
				},
				"recursive": map[string]interface{}{
					"type":        "boolean",
					"description": "List files recursively (default: false)",
					"default":     false,
				},
			},
			Required: []string{"repo"},
		},
	}, handleListFiles)

	// Tool: read_file
	s.AddTool(mcp.Tool{
		Name:        "read_file",
		Description: "Read a file from a repository",
		InputSchema: mcp.ToolInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"repo": map[string]interface{}{
					"type":        "string",
					"description": fmt.Sprintf("Repository name. Available: %s", strings.Join(repoNames, ", ")),
					"enum":        repoNames,
				},
				"file": map[string]interface{}{
					"type":        "string",
					"description": "Path to the file within the repository",
				},
			},
			Required: []string{"repo", "file"},
		},
	}, handleReadFile)

	// Tool: search_files
	s.AddTool(mcp.Tool{
		Name:        "search_files",
		Description: "Search for files by name pattern (supports * and ? wildcards)",
		InputSchema: mcp.ToolInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"repo": map[string]interface{}{
					"type":        "string",
					"description": fmt.Sprintf("Repository name. Available: %s", strings.Join(repoNames, ", ")),
					"enum":        repoNames,
				},
				"pattern": map[string]interface{}{
					"type":        "string",
					"description": "File name pattern with wildcards (* and ?)",
				},
			},
			Required: []string{"repo", "pattern"},
		},
	}, handleSearchFiles)

	// Tool: list_repos
	s.AddTool(mcp.Tool{
		Name:        "list_repos",
		Description: "List all configured repositories and their paths",
		InputSchema: mcp.ToolInputSchema{
			Type:       "object",
			Properties: map[string]interface{}{},
			Required:   []string{},
		},
	}, handleListRepos)
}

func registerResources(s *server.MCPServer) {
	// Add resource template for repository access
	template := mcp.ResourceTemplate{
		URITemplate: "repo://{repo}/{path}",
		Name:        "Repository File",
		Description: "Access files from configured repositories using repo://repo-name/path/to/file",
		MIMEType:    "text/plain",
	}
	s.AddResourceTemplate(template, handleReadResourceTemplate)
}

// getFileSystem returns a FileSystem for the given repository name
func getFileSystem(repoName string) (FileSystem, error) {
	reposMux.RLock()
	repo, ok := repos[repoName]
	reposMux.RUnlock()

	if !ok {
		return nil, fmt.Errorf("unknown repository: %s", repoName)
	}

	return repo.GetFileSystem(sshPool)
}

func handleListFiles(arguments map[string]interface{}) (*mcp.CallToolResult, error) {
	repo, ok := arguments["repo"].(string)
	if !ok {
		return mcp.NewToolResultError("repo parameter is required"), nil
	}

	path := "."
	if p, ok := arguments["path"].(string); ok {
		path = p
	}

	recursive := false
	if r, ok := arguments["recursive"].(bool); ok {
		recursive = r
	}

	fs, err := getFileSystem(repo)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	// Validate path
	relPath, err := ValidatePath(fs.BasePath(), path)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	info, err := fs.Stat(relPath)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Path does not exist: %s", path)), nil
	}

	if !info.IsDir() {
		return mcp.NewToolResultError(fmt.Sprintf("Path is not a directory: %s", path)), nil
	}

	var files []string

	if recursive {
		basePath := fs.BasePath()
		targetPath := filepath.Join(basePath, relPath)
		err = fs.Walk(relPath, func(p string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if p == targetPath || p == basePath {
				return nil
			}
			if shouldSkip(p) {
				if info.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if info.Mode().IsRegular() {
				// Get path relative to target
				rel, _ := filepath.Rel(targetPath, p)
				if rel != "" {
					files = append(files, rel)
				}
			}
			return nil
		})
	} else {
		entries, err := fs.ReadDir(relPath)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		for _, entry := range entries {
			if shouldSkip(entry.Name()) {
				continue
			}
			if entry.IsDir() {
				files = append(files, entry.Name()+"/")
			} else {
				files = append(files, entry.Name())
			}
		}
	}

	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	result := map[string]interface{}{
		"repository": repo,
		"path":       path,
		"files":      files,
	}

	jsonResult, _ := json.MarshalIndent(result, "", "  ")
	return mcp.NewToolResultText(string(jsonResult)), nil
}

func handleReadFile(arguments map[string]interface{}) (*mcp.CallToolResult, error) {
	repo, ok := arguments["repo"].(string)
	if !ok {
		return mcp.NewToolResultError("repo parameter is required"), nil
	}

	file, ok := arguments["file"].(string)
	if !ok {
		return mcp.NewToolResultError("file parameter is required"), nil
	}

	fs, err := getFileSystem(repo)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	// Validate path
	relPath, err := ValidatePath(fs.BasePath(), file)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	info, err := fs.Stat(relPath)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("File does not exist: %s", file)), nil
	}

	if !info.Mode().IsRegular() {
		return mcp.NewToolResultError(fmt.Sprintf("Path is not a file: %s", file)), nil
	}

	if shouldSkip(relPath) {
		return mcp.NewToolResultError(fmt.Sprintf("Access denied: %s", file)), nil
	}

	content, err := fs.ReadFile(relPath)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	result := fmt.Sprintf("File: %s/%s\n\n%s", repo, file, string(content))
	return mcp.NewToolResultText(result), nil
}

func handleSearchFiles(arguments map[string]interface{}) (*mcp.CallToolResult, error) {
	repo, ok := arguments["repo"].(string)
	if !ok {
		return mcp.NewToolResultError("repo parameter is required"), nil
	}

	pattern, ok := arguments["pattern"].(string)
	if !ok {
		return mcp.NewToolResultError("pattern parameter is required"), nil
	}

	fs, err := getFileSystem(repo)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	var matches []string
	basePath := fs.BasePath()

	err = fs.Walk(".", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if path == basePath {
			return nil
		}
		if shouldSkip(path) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if info.Mode().IsRegular() {
			matched, _ := filepath.Match(pattern, filepath.Base(path))
			if matched {
				relPath, _ := filepath.Rel(basePath, path)
				if relPath != "" {
					matches = append(matches, relPath)
				}
			}
		}
		return nil
	})

	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	result := map[string]interface{}{
		"repository": repo,
		"pattern":    pattern,
		"matches":    matches,
	}

	jsonResult, _ := json.MarshalIndent(result, "", "  ")
	return mcp.NewToolResultText(string(jsonResult)), nil
}

func handleListRepos(arguments map[string]interface{}) (*mcp.CallToolResult, error) {
	reposMux.RLock()
	defer reposMux.RUnlock()

	// Build the result with repository details
	repoList := make([]map[string]interface{}, 0, len(repos))
	for name, repo := range repos {
		info := map[string]interface{}{
			"name": name,
			"type": repo.Type,
			"path": repo.Path,
		}
		if repo.Type == "ssh" {
			info["host"] = repo.Host
			info["user"] = repo.User
		}
		repoList = append(repoList, info)
	}

	result := map[string]interface{}{
		"repositories": repoList,
		"count":        len(repos),
	}

	jsonResult, _ := json.MarshalIndent(result, "", "  ")
	return mcp.NewToolResultText(string(jsonResult)), nil
}

func handleReadResourceTemplate(request mcp.ReadResourceRequest) ([]interface{}, error) {
	uri := request.Params.URI

	if !strings.HasPrefix(uri, "repo://") {
		return nil, fmt.Errorf("invalid URI scheme. Expected repo://, got: %s", uri)
	}

	// Parse URI: repo://repo-name/path/to/file
	uriParts := strings.TrimPrefix(uri, "repo://")
	parts := strings.SplitN(uriParts, "/", 2)

	if len(parts) < 1 {
		return nil, fmt.Errorf("invalid URI format: %s", uri)
	}

	repoName := parts[0]
	file := ""
	if len(parts) > 1 {
		file = parts[1]
	}

	fs, err := getFileSystem(repoName)
	if err != nil {
		return nil, err
	}

	if file == "" {
		return nil, fmt.Errorf("no file path specified in URI")
	}

	relPath, err := ValidatePath(fs.BasePath(), file)
	if err != nil {
		return nil, err
	}

	info, err := fs.Stat(relPath)
	if err != nil {
		return nil, fmt.Errorf("file does not exist: %s", file)
	}

	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("path is not a file: %s", file)
	}

	if shouldSkip(relPath) {
		return nil, fmt.Errorf("access denied: %s", file)
	}

	content, err := fs.ReadFile(relPath)
	if err != nil {
		return nil, err
	}

	// Return as slice of text content
	return []interface{}{
		mcp.TextContent{
			Type: "text",
			Text: string(content),
		},
	}, nil
}

// shouldSkip determines if a file or directory should be skipped
func shouldSkip(path string) bool {
	base := filepath.Base(path)

	// Skip hidden files/directories
	if strings.HasPrefix(base, ".") {
		return true
	}

	// Skip node_modules
	if base == "node_modules" {
		return true
	}

	return false
}
