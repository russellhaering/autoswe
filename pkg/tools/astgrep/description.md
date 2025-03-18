# AST-Grep Tool

AST-grep searches code using Abstract Syntax Tree patterns instead of text/regex. It understands code structure and semantics across languages.

## Features

- Structural search based on code structure
- Language-aware matching
- Pattern variables (`$VAR`) to capture elements

## Usage

Use patterns that represent code structure:
```
func $FUNC($$$ARGS) { $$$ }  # Find function declarations
if $CONDITION == nil { $$$ } # Find specific if statements
```