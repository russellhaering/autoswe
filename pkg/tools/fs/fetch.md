# Filesystem Fetch Tool

The `fs_fetch` tool reads and returns the content of a file as a string.

## Parameters

- `path`: Path to the file to read (required, relative to workspace root)

## Response

Returns a JSON object with:
- `content`: The file's content as a string

## Features

- Reads complete file content
- Returns text and binary files as strings
- Respects repository access restrictions

## Examples

- Read configuration: `config.json`
- View source file: `src/main.go`

## Errors

- File doesn't exist
- Path is inaccessible
- Path points to a directory 