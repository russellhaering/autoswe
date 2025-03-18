# Filesystem Put Tool

The `fs_put` tool writes content to a file.

## Parameters

- `path`: Path to the file to write (required, relative to workspace root)
- `content`: Content to write to the file (required, cannot be empty)

## Features

- Creates new files or overwrites existing ones
- Sets file permissions to 0644
- Respects repository access restrictions

## Examples

- Create configuration: `config.json`
- Save source code: `src/main.go`

## Errors

- Empty content provided
- Path is inaccessible
- Parent directory cannot be created 