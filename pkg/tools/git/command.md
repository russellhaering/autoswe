# Git Command Tool

The `git_command` tool executes arbitrary git commands in the workspace repository.

## Parameters

- `args`: Array of git command arguments (required)

## Response

Returns a JSON object with:
- `output`: Output from the git command

## Features

- Runs any git subcommand with arguments
- Executes in the workspace repository
- Returns command output as string
- Respects repository access restrictions

## Examples

- Check status: `["status", "-s"]`
- View branches: `["branch", "-a"]`
- View commit history: `["log", "--oneline", "-n", "5"]`

## Errors

- Empty arguments array
- Invalid git command
- Permission issues
- Command execution failures 