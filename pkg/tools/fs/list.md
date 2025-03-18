# Filesystem List Tool

The `fs_list` tool lists files and directories at a specified path.

## Parameters

- `path`: Path to the directory to list (required)

## Response

Returns a JSON object with an array of file information objects:
```json
{
  "files": [
    {
      "name": "filename.txt",
      "size": 1024,
      "is_dir": false,
      "mode": 644,
      "mod_time": "2023-01-01T12:00:00Z"
    }
  ]
}
```

## Features

- Lists all files and directories in a path
- Shows name, size, type, permissions, and modification time
- Respects repository access restrictions

## Examples

- List root directory: `.`
- List specific directory: `src/`

## Errors

- Path doesn't exist
- Path is inaccessible 