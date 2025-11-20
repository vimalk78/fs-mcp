# Multi-Repository MCP Server

A Model Context Protocol (MCP) server that allows you to access files from multiple repositories without changing your current working directory.

**Built with Go for simplicity and performance.**

## Features

- **Access multiple repositories** from a single location
- **Four powerful tools**:
  - `list_repos`: List all configured repositories and their paths
  - `list_files`: List files in any configured repository
  - `read_file`: Read files from any repository
  - `search_files`: Search for files using wildcards (* and ?)
- **Resource protocol**: Access files via `repo://repo-name/path/to/file` URIs
- **Auto-reload**: Configuration automatically reloads when `config.json` changes - no restart needed!
- **Security built-in**:
  - Path traversal protection
  - Automatic skipping of hidden files (starting with .)
  - Automatic skipping of node_modules directories

## Installation

1. Install the binary:
   ```bash
   go install github.com/vimalk78/fs-mcp@latest
   ```

   Or build locally:
   ```bash
   go build -o fs-mcp
   sudo mv fs-mcp /usr/local/bin/  # Optional: install to system path
   ```

2. Create your config directory and file:
   ```bash
   # Create config directory
   mkdir -p ~/.config/fs-mcp

   # Copy the example config
   cp config.example.json ~/.config/fs-mcp/config.json

   # Edit with your actual repository paths
   vim ~/.config/fs-mcp/config.json
   ```

   Example `~/.config/fs-mcp/config.json`:
   ```json
   {
     "repositories": {
       "frontend": "/home/yourusername/projects/frontend",
       "backend": "/home/yourusername/projects/backend",
       "infrastructure": "/home/yourusername/projects/infrastructure"
     }
   }
   ```

   **Important**:
   - Use absolute paths for your repositories
   - The default config location is `~/.config/fs-mcp/config.json` (automatically detected)
   - You can change repository paths anytime without rebuilding - changes auto-reload!

That's it! You now have a single executable with configurable repositories.

## Configuration

### For Claude Desktop

Add this to your Claude Desktop configuration file:

**macOS**: `~/Library/Application Support/Claude/claude_desktop_config.json`
**Windows**: `%APPDATA%\Claude\claude_desktop_config.json`

```json
{
  "mcpServers": {
    "multi-repo": {
      "command": "fs-mcp"
    }
  }
}
```

Or if you need to specify a custom config location:
```json
{
  "mcpServers": {
    "multi-repo": {
      "command": "fs-mcp",
      "args": ["-config", "/path/to/your/config.json"]
    }
  }
}
```

**Note**:
- If using `go install`, ensure `$GOPATH/bin` or `$GOBIN` is in your PATH
- Config auto-detected at `~/.config/fs-mcp/config.json` if `-config` not specified

### For Claude Code

Add this to your MCP settings file:

**macOS/Linux**: `~/.config/claude-code/mcp_settings.json`
**Windows**: `%APPDATA%\claude-code\mcp_settings.json`

```json
{
  "mcpServers": {
    "multi-repo": {
      "command": "fs-mcp"
    }
  }
}
```

Or if you need to specify a custom config location:
```json
{
  "mcpServers": {
    "multi-repo": {
      "command": "fs-mcp",
      "args": ["-config", "/path/to/your/config.json"]
    }
  }
}
```

**Note**:
- If using `go install`, ensure `$GOPATH/bin` or `$GOBIN` is in your PATH
- Config auto-detected at `~/.config/fs-mcp/config.json` if `-config` not specified
- **For Claude CLI**: Use `claude --mcp-config ~/.config/claude-code/mcp_settings.json` to load MCP servers

**Important**:
- Config file default location: `~/.config/fs-mcp/config.json` (automatically detected)
- Use absolute paths for repository locations in your config file
- If `fs-mcp` command not found, ensure Go's bin directory is in your PATH

## Usage

Once configured, you can use the MCP server through Claude:

### List Repositories

```
What repositories do you have access to?
```

Claude will call:
```python
list_repos()
```

### List Files

```
Show me the files in the backend repository
```

Claude will call:
```python
list_files(repo="backend", path=".", recursive=False)
```

### Read a File

```
Show me the file backend/src/api.py
```

Claude will call:
```python
read_file(repo="backend", file="src/api.py")
```

### Search Files

```
List all TypeScript files in the frontend repository
```

Claude will call:
```python
search_files(repo="frontend", pattern="*.ts")
```

### Using Resources

You can also access files using the resource URI format:

```
Read the file at repo://backend/src/api.py
```

## Tool Reference

### list_repos

Lists all configured repositories and their paths.

**Parameters**: None

**Returns**: JSON object with list of repositories and count

**Example**:
```json
{
  "repositories": [
    {
      "name": "frontend",
      "path": "/home/user/projects/frontend"
    },
    {
      "name": "backend",
      "path": "/home/user/projects/backend"
    }
  ],
  "count": 2
}
```

### list_files

Lists files in a repository directory.

**Parameters**:
- `repo` (required): Repository name from your REPOS configuration
- `path` (optional): Path within the repository (default: ".")
- `recursive` (optional): Whether to list files recursively (default: false)

**Returns**: JSON object with repository, path, and list of files

**Example**:
```json
{
  "repository": "frontend",
  "path": "src",
  "files": [
    "App.tsx",
    "index.ts",
    "components/"
  ]
}
```

### read_file

Reads a file from a repository.

**Parameters**:
- `repo` (required): Repository name
- `file` (required): Path to the file within the repository

**Returns**: Plain text with header showing file location and contents

**Example**:
```
File: backend/src/api.py

from flask import Flask
app = Flask(__name__)
...
```

### search_files

Searches for files matching a pattern.

**Parameters**:
- `repo` (required): Repository name
- `pattern` (required): File name pattern with wildcards (* and ?)

**Returns**: JSON object with repository, pattern, and matching files

**Example**:
```json
{
  "repository": "frontend",
  "pattern": "*.ts",
  "matches": [
    "src/index.ts",
    "src/types/user.ts",
    "src/utils/helper.ts"
  ]
}
```

## Security

The server implements several security measures:

1. **Path Traversal Protection**: All paths are validated to ensure they stay within configured repository bounds
2. **Hidden File Filtering**: Files and directories starting with `.` are automatically skipped
3. **node_modules Filtering**: The `node_modules` directory is automatically skipped
4. **Read-only Access**: The server only provides read access to files

## Troubleshooting

### Server not appearing in Claude

1. Check that the configuration file is in the correct location
2. Verify the `fs-mcp` binary is accessible (check PATH or use absolute path)
3. For Claude CLI: Ensure you're using the `--mcp-config` flag
4. Restart Claude Desktop/Code after configuration changes
5. Check the logs for error messages

### Repository path not found

1. Ensure the paths in your config file are absolute paths
2. Verify the directories exist and are accessible
3. Check file permissions
4. Ensure the `-config` flag points to the correct config file path (if specified)

### File not readable

1. Ensure the file is a text file (binary files are not supported)
2. Check that the file is not in a hidden directory or node_modules
3. Verify the file path is correct relative to the repository root

## Development

To modify or extend the server:

1. Edit `multi_repo_server.py`
2. Add or modify tools in the `@app.call_tool()` handler
3. Update the tool schemas in `@app.list_tools()`
4. Test your changes by restarting Claude

## License

This project is provided as-is for use with Claude.
