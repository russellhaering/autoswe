# AST-Grep Tool

The `ast_grep` tool performs powerful, precise code search using Abstract Syntax Tree (AST) patterns rather than simple text or regex matching. This allows for more accurate code identification based on the actual structure of the code.

## Overview

AST-grep is a structural code search and replace tool that understands the semantics of your code. Unlike traditional pattern matching tools that operate on text, AST-grep works on the parsed representation of code (the Abstract Syntax Tree), ensuring that your searches are language-aware and respect code structure.

## Key Features

- **Structural Search**: Find code based on its structure, not just text patterns
- **Language Awareness**: Understands the syntax and semantics of multiple programming languages
- **Precise Matches**: Avoids false positives common with regex-based searches
- **Pattern Variables**: Use special syntax like `$VAR` to capture and match code elements

## Usage

When using the AST-grep tool, you provide a pattern that represents the structure of code you want to find. The pattern can include:

- Regular code syntax for exact matches
- Pattern variables (starting with `$`) to match specific elements
- Special syntax like `$$$` to match multiple elements

### Examples

To find all function declarations:
```
func $FUNC($$$ARGS) { $$$ }
```

To find all if statements with a specific condition:
```
if $CONDITION == nil { $$$ }
```

## Implementation Details

The tool runs in a containerized environment using the `ast-grep-container` image to provide consistent behavior regardless of the local environment. All searches are performed against the mounted project directory.

## Parameters

- **pattern** (required): The AST-grep pattern to search for
- **lang** (optional): Language to restrict the search to (e.g., 'go', 'rust', 'typescript')
- **paths** (optional): Specific directories or files to search within
- **rewrite** (optional): Pattern to rewrite matched code (for transformation operations)