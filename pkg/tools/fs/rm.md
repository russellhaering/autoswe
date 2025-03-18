# Filesystem Remove Tool

The `fs_rm` tool deletes files or directories.

## Parameters

- `path`: Path to the file or directory to delete (required)
- `recursive`: Boolean flag for recursive deletion (defaults to false)

## Features

- Deletes individual files
- Removes directories when `recursive=true`
- Automatically removes empty parent directories
- Respects repository access restrictions

## Warning

**Use with caution!** Deletion operations are permanent and cannot be undone.

## Errors

- Path doesn't exist
- Path is inaccessible
- Non-empty directory without `recursive=true` 