package main

import (
	"context"
	"fmt"
	"os"

	"github.com/russellhaering/autoswe/pkg/autoswe"
	"github.com/russellhaering/autoswe/pkg/log"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

var (
	manager *autoswe.Manager
)

var (
	rootCmd = &cobra.Command{
		Use:   "autoswe",
		Short: "A tool for AI-assisted Go software engineering",
		Long:  `autoswe is a command-line tool that uses AI to assist with Go software engineering tasks. It provides various commands for code analysis, indexing, and task automation.`,
		PersistentPreRunE: func(_ *cobra.Command, _ []string) error {
			_manager, _, err := initializeManager(context.Background(), autoswe.Config{
				GeminiAPIKey:    autoswe.GeminiAPIKey(geminiKey),
				AnthropicAPIKey: autoswe.AnthropicAPIKey(anthropicKey),
				RootDir:         autoswe.RootDir(rootDir),
			})
			if err != nil {
				return fmt.Errorf("failed to initialize manager: %w", err)
			}

			manager = &_manager
			return nil
		},
	}

	// Configuration flags
	geminiKey    string
	rootDir      string
	anthropicKey string
)

func init() {
	// Global flags
	rootCmd.PersistentFlags().StringVar(&geminiKey, "gemini-key", os.Getenv("GOOGLE_API_KEY"), "Gemini API key")
	rootCmd.PersistentFlags().StringVar(&rootDir, "root", ".", "root directory to operate on")
	rootCmd.PersistentFlags().StringVar(&anthropicKey, "anthropic-key", os.Getenv("ANTHROPIC_API_KEY"), "Anthropic API key")

	// Add commands
	rootCmd.AddCommand(newIndexCmd())
	rootCmd.AddCommand(newContextCmd())
	rootCmd.AddCommand(newTaskCmd())
	rootCmd.AddCommand(newCommitCmd())

	// Initialize logger
	if err := log.Init(true); err != nil {
		// Can't use log.Error here since logger isn't initialized
		fmt.Fprintf(os.Stderr, "Error initializing logger: %v\n", err)
		os.Exit(1)
	}
}

func main() {
	// Initialize the command
	if err := rootCmd.Execute(); err != nil {
		log.Error("Failed to execute command", zap.Error(err))
		os.Exit(1)
	}

	// Clean up clients
	if err := manager.Close(); err != nil {
		log.Warn("error closing manager", zap.Error(err))
	}
}

// newIndexCmd creates the index command
func newIndexCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "index",
		Short: "Build or update the code index",
		Long: `Build or update the semantic code index.
This command will scan the codebase, split files into chunks, and create embeddings
for semantic search capabilities.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// Build or update the index
			log.Info("Building/updating code index")
			if err := manager.Indexer.UpdateIndex(cmd.Context()); err != nil {
				return fmt.Errorf("failed to update index: %w", err)
			}

			log.Info("Index updated successfully")
			return nil
		},
	}

	return cmd
}

// newContextCmd creates the query command
func newContextCmd() *cobra.Command {
	var limit int

	cmd := &cobra.Command{
		Use:   `context "search query"`,
		Short: "Semantic context fetching",
		Long:  `Search the semantic code index using natural language queries, and display the raw results in the form that would be exposed to the LLM`,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := manager.Indexer.Query(cmd.Context(), args[0])
			if err != nil {
				return fmt.Errorf("failed to query index: %w", err)
			}

			fmt.Println("Answer:")
			fmt.Println()
			fmt.Println(result.Answer)

			return nil
		},
	}

	// Add flags
	cmd.Flags().IntVarP(&limit, "limit", "n", 10, "maximum number of results to return")

	return cmd
}

// newTaskCmd creates the task command
func newTaskCmd() *cobra.Command {
	var extraContextPaths []string

	cmd := &cobra.Command{
		Use:   "task \"<task description>\"",
		Short: "Run an AI-assisted task",
		Long: `Run an AI-assisted task using Claude to help solve software engineering problems.
The task description should be a clear, natural language description of what you want to accomplish.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// TODO: Add extra context paths

			response, err := manager.ExecuteTask(cmd.Context(), args[0])
			if err != nil {
				return fmt.Errorf("failed to execute task: %w", err)
			}

			fmt.Println()
			fmt.Println()
			fmt.Println("Task Complete")
			fmt.Println()
			fmt.Println(response)

			return nil
		},
	}

	cmd.Flags().StringArrayVar(&extraContextPaths, "extra-context", nil,
		"Path to additional files to include in the semantic search context. Can be specified multiple times.")

	return cmd
}

// newCommitCmd creates the commit command
func newCommitCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "commit",
		Short: "Create a commit with an AI-generated message",
		Long: `Create a git commit with an automatically generated message that summarizes the changes.
This command will analyze the current git diff and create a descriptive commit message.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			commitPrompt := `Given the current git changes, your task is to:
1. Get the current git status to check for changes
2. If there are changes:
   - Analyze the changes
   - Write a clear, concise commit message that:
     * Uses present tense (e.g. "Add feature" not "Added feature")
     * Begins with a concise summary line
     * If needed, adds bullet points with more details after a blank line
   - Create the commit with the generated message
3. If there are no changes, inform the user
4. Complete the task with a status message`

			response, err := manager.ExecuteTask(cmd.Context(), commitPrompt)
			if err != nil {
				return fmt.Errorf("failed to process commit: %w", err)
			}

			fmt.Println(response)
			return nil
		},
	}

	return cmd
}
