# Git Commit Tool

The `git_commit` tool stages all current changes and creates a new commit.

## Parameters

- `message`: Commit message (required)

## Response

Returns a JSON object with:
- `output`: Output from the git commit command

## Features

- Automatically stages all changes (`git add .`)
- Creates a commit with the specified message
- Executes in the workspace repository
- Returns the git command output

## Examples

- Create a simple commit: `"Add new user authentication feature"`
- Include issue reference: `"Fix pagination bug #123"`

## Errors

- Empty commit message
- No changes to commit
- Git configuration issues 