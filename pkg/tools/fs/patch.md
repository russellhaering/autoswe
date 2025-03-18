# Filesystem Patch Tool

The `fs_patch` tool applies changes to a file using a simple search-and-replace diff format.

## Features

- Apply changes to files using search-and-replace pattern matching
- Support for adding, removing, and modifying content
- Falls back to AI-assisted patching for complex cases
- Preserves file permissions
- Validates changes before applying
- Handles multiple line replacements
- Respects repository access restrictions

## Usage

- Specify the target file to modify
- Provide a diff in the simplediff format (see below)
- For complex changes that can't be applied automatically, the system will use AI assistance

## Parameters

- `path`: Path to the file to modify (required)
- `diff`: Diff in simplediff format to apply (required)

## Simplediff Format

The tool uses a simple search-and-replace format with markers:
```
<<<<<<< SEARCH
[content to find]
=======
[content to replace with]
>>>>>>> REPLACE
```

Where:
- The content between `<<<<<<< SEARCH` and `=======` is the text to find
- The content between `=======` and `>>>>>>> REPLACE` is the text to replace it with
- Exact matching is used (whitespace and line breaks matter)
- Empty search or replace sections are allowed (for insertion or deletion)

## Examples

- Replace an import statement:
```
<<<<<<< SEARCH
import os
=======
import os
import sys
>>>>>>> REPLACE
```

- Add a comment:
```
<<<<<<< SEARCH
func main() {
=======
// Main entry point
func main() {
>>>>>>> REPLACE
```

- Remove code:
```
<<<<<<< SEARCH
// Debug code
console.log("Debug info");
=======
>>>>>>> REPLACE
```

## Error Handling

The tool will return appropriate error messages when:
- Target file doesn't exist
- Search content is not found in the file
- Diff format is invalid
- Path is inaccessible due to permissions 