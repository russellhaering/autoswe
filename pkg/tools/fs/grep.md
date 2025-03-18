# Filesystem Grep Tool

The `fs_grep` tool searches for regex patterns in files.

## Parameters

- `pattern`: Regex pattern to search for (required)
- `path`: Directory to search in (optional, defaults to ".")

## Response

Returns a formatted string with:
- Count of matches found
- File paths and line numbers
- 3 context lines before and after each match
- Highlighted matched lines

## Features

- Uses Go regular expression syntax
- Searches recursively through directories
- Shows match context with line numbers
- Respects repository access restrictions

## Examples

- Find TODOs: `TODO|FIXME`
- Find functions: `func\s+\w+\(`
- In specific dir: `path: "src"`

## Errors

- Invalid regex pattern
- Path doesn't exist
- Path is inaccessible 