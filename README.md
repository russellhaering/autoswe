# auto-swe

`auto-swe` is an experimental, autonomous, CLI-driven AI agent that writes software. Currently it is specifically focused on Go, but in the future it may be extended to support additional languages.

`auto-swe` mostly consists of a collection of tools for exploring and modifying a codebase - think of it as an IDE for AI. Given a task description, `auto-swe` invokes an LLM (currently only Claude is supported) with access to these tools a basic system prompt, and leaves the rest to the LLM.

## Configuration

You'll need to set up *both* of the following API keys:

- `ANTHROPIC_API_KEY` - for Claude AI (or use --anthropic-key flag)
- `GOOGLE_API_KEY` - used for Gemini AI (or use --gemini-key flag)

`auto-swe` uses both Gemini and Claude for various purposes:

* `gemini-2.0-flash-lite` is used for indexing and search due to  its low cost and large context window
* `claude-3-7-sonnet-latest` is used for the bulk of the work, including task orchestration, tool usage, and the generation of commit messages and other artifacts
* `gemini-2.0-flash` is used as a fallback for patch application when applying patches programmatically fails (this may be removed or replaced with a different tool in the future)

## Usage

### Basic Commands

```bash
# Run an AI-assisted task
auto-swe task "add error handling to the database connection"

# Ask a question about the codebase
auto-swe task "How are tools registered?"

# Commit current changes with an AI-generated commit message
auto-swe commit
```

## Tools

The following is a non-exhaustive list of the tools that `auto-swe` has access to.

### Code Quality & Validation

* `lint` - Runs `golangci-lint` on the project for static code analysis
* `format` - Uses `goimports` (a wrapper around `gofmt`) to format all Go code in the project
* `test` - Executes project tests using `go test -v ./...`
* `build` - Compiles the project using `go build ./...`

### Code Discovery & Understanding

* `query_codebase` - Performs semantic code search using natural language queries
* `ast_grep` - Uses AST-based pattern matching to find or modify specific code patterns
* `fs_grep` - Traditional text-based search across the codebase 

### File Manipulation

* `fs_put_file` - Creates or overwrites files with specified content
* `fs_patch` - Applies patches to existing files to modify specific portions
* `fs_rm` - Removes files or directories from the codebase

### Git Integration

* `git_commit` - Commits the current changes
* `commit` - Generates meaningful Git commit messages based on changes
* `branch` - Creates and manages Git branches for specific tasks
* `merge` - Assists with merging branches and resolving conflicts

## Semantic Search

In order to allow the LLM to efficiently understand the codebase, `auto-swe` builds a semantic search index of the codebase.

Every time an `auto-swe` command is run, it will walk the full codebase in the current working directory, and for any file that has changed since the last update to the index it will "re-index" that file.

Indexing a file is a two-step process:

1. The entire file is sent to an LLM (currently `gemini-2.0-flash-lite`) with a prompt that asks it to describe each function, struct, section, etc in the file, along with the exact line range where that element can be found.
2. The LLM's responses are then used to build a vector embedding for the file, which is stored in a local boltdb database.

When a natural language query is made, the following process occurs:

1. A vector search is made against the index to find the most relevant files and snippets. When a high density of relevant snippets are found in a single file or section, the entire file or section is considered a match.
2. Every matching snippet, section or file is sent to `gemini-2.0-flash-lite` with a prompt that asks it to filter out verbatim results relevant to the query.

Running `auto-swe context "some query"` allows you to see the raw results of a semantic search, but in normal operation these searches are invoked automatically by the LLM when it needs to answer a question about the codebase, and the results help to populate the LLM's context window.