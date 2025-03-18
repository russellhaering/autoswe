# Query Codebase Tool

The `query_codebase` tool performs semantic search over the codebase and returns AI-generated explanations with relevant code examples.

## Parameters

- `query`: Natural language query about the codebase (required)

## Response

Returns a JSON object with:
- `answer`: AI-generated answer with code examples and explanations

## Features

- Uses semantic understanding (not just text matching)
- Searches across all files in the repository
- Contextualizes code snippets in the response
- Returns both explanations and relevant code examples
- Understands code patterns and intent

## Examples

- Find implementation patterns: `"How is authentication implemented?"`
- Locate specific functionality: `"Where is the database connection configured?"`
- Understand architecture: `"How are API endpoints structured?"`

## Errors

- Empty query
- Indexing not complete
- Query too vague
- No relevant results found 