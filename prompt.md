Create a Python MCP (Model Context Protocol) server that allows me to access files from multiple repositories without changing my current working directory.

## Problem
When working in one repository with Claude Code, I often need to reference files from other repositories in different directories. I don't want to change directories or lose my current context.

## Requirements

1. **Configuration**: Support multiple repositories with custom names
   - Use a dictionary like: `REPOS = {"frontend": "/path/to/frontend", "backend": "/path/to/backend"}`
   - Make it easy to add/remove repositories

2. **Tools**: Implement three MCP tools:
   - `list_files`: List files in a repository directory
     - Parameters: repo (required), path (optional, default "."), recursive (optional, default False)
   - `read_file`: Read a file from a repository
     - Parameters: repo (required), file (required)
   - `search_files`: Search for files by name pattern (with wildcards)
     - Parameters: repo (required), pattern (required, supports * and ? wildcards)

3. **Resources**: Expose repositories as MCP resources
   - URI format: `repo://repo-name/path/to/file`
   - Allow reading files via the resource protocol

4. **Security**: 
   - Implement path traversal protection (ensure paths stay within configured repos)
   - Skip hidden files (starting with .)
   - Skip node_modules directories

5. **Technical Stack**:
   - Use the official `mcp` Python package
   - Use async/await with asyncio
   - Use stdio transport for communication
   - Include proper error handling and logging

6. **File Structure**:
   - Main server file: `multi_repo_server.py` with shebang for direct execution
   - `requirements.txt` with dependencies
   - `README.md` with setup and usage instructions

## Expected Behavior

When I ask Claude: "Show me the file backend/src/api.py"
- Claude calls `read_file(repo="backend", file="src/api.py")`
- Returns the file content without me changing directories

When I ask: "List all TypeScript files in frontend"
- Claude calls `search_files(repo="frontend", pattern="*.ts")`
- Returns all matching files recursively

## Output Format

Tools should return JSON results formatted as:
- list_files: `{"repository": "name", "path": ".", "files": ["file1.py", ...]}`
- read_file: Plain text with header `File: repo/path\n\n{content}`
- search_files: `{"repository": "name", "pattern": "*.ts", "matches": [...]}`

## Additional Details

- Use pathlib.Path for path operations
- Implement proper MCP decorators: @app.list_tools(), @app.call_tool(), @app.list_resources(), @app.read_resource()
- Include docstrings for all functions
- Add setup instructions in README for configuring Claude Desktop/Code
- Make the script executable with proper shebang

Please create a production-ready implementation with all files needed to run this MCP server.
