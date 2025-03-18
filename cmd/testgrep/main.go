package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/russellhaering/autoswe/pkg/repo"
	"github.com/russellhaering/autoswe/pkg/tools/fs"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: testgrep [pattern] [path]")
		os.Exit(1)
	}

	pattern := os.Args[1]
	path := "."
	if len(os.Args) > 2 {
		path = os.Args[2]
	}

	// Create the grep tool
	filesystem, err := repo.NewFilesystem(".")
	if err != nil {
		fmt.Printf("Error creating filesystem: %v\n", err)
		os.Exit(1)
	}

	filteredFS := repo.NewFilteredFS(filesystem, func(path string) bool {
		return true // Allow all files
	})

	tool := fs.GrepTool{
		FilteredFS: filteredFS,
	}

	// Execute the grep
	result, err := tool.Execute(context.Background(), fs.GrepInput{
		Pattern: pattern,
		Path:    path,
	})

	if err != nil {
		fmt.Printf("Error executing grep: %v\n", err)
		os.Exit(1)
	}

	// Print raw result
	fmt.Println("Raw Result:")
	fmt.Println(result.Result)

	// Print JSON marshaled result
	jsonResult, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		fmt.Printf("Error marshaling result: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("\nJSON Result:")
	fmt.Println(string(jsonResult))
}