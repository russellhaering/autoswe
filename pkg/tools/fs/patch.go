package fs

import (
	"context"
	"fmt"
	iofs "io/fs"
	"os"
	"strings"

	"github.com/google/generative-ai-go/genai"
	"github.com/google/wire"
	"github.com/invopop/jsonschema"
	"github.com/russellhaering/auto-swe/pkg/log"
	"github.com/russellhaering/auto-swe/pkg/repo"
	"github.com/russellhaering/auto-swe/pkg/tools/fs/simplediff"
	"go.uber.org/zap"
)

// PatchInput represents the input parameters for the Patch tool
type PatchInput struct {
	Path string `json:"path" jsonschema_description:"Path to the file to patch"`
	Diff string `json:"diff" jsonschema_description:"A search-and-replace diff using the markers <<<<<<< SEARCH, =======, and >>>>>>> REPLACE"`
}

// PatchOutput represents the output of the Patch tool
type PatchOutput struct{}

type PatchTool struct {
	Gemini     *genai.Client
	FilteredFS repo.FilteredFS
}

var ProvidePatchTool = wire.Struct(new(PatchTool), "*")

// Name returns the name of the tool
func (t *PatchTool) Name() string {
	return "fs_patch"
}

// Description returns a description of the patch tool
func (t *PatchTool) Description() string {
	return "Patches a file by searching for and replacing specified text"
}

// Schema returns the JSON schema for the patch tool
func (t *PatchTool) Schema() *jsonschema.Schema {
	return jsonschema.Reflect(&PatchInput{})
}

// applyPatchWithGemini uses the Gemini AI model to apply the patch when simplediff fails
func (t *PatchTool) applyPatchWithGemini(ctx context.Context, originalContent, diffContent string) (string, error) {
	model := t.Gemini.GenerativeModel("gemini-2.0-flash")

	// Build the prompt for Gemini
	prompt := fmt.Sprintf(`You are a precise code editing tool. Given a file's content and a diff in the simplediff format, apply the changes exactly as specified in the diff to the file content. Return ONLY the modified file content, with no additional text or explanation.

The diff format uses these markers:
<<<<<<< SEARCH
[content to find]
=======
[content to replace with]
>>>>>>> REPLACE

Original file content:
%s

Diff to apply:
%s

Remember:
1. Apply the changes exactly as specified in the diff
2. Always output the ENTIRE modified file content, including unchanged parts; we will be replacing the file with the output, verbatim
3. Return ONLY the modified file content
4. Do not add any comments or explanations
5. Preserve all whitespace and formatting in unchanged parts
6. If the diff cannot be applied, return an error message starting with "ERROR:"`, originalContent, diffContent)

	// Generate response
	resp, err := model.GenerateContent(ctx, genai.Text(prompt))
	if err != nil {
		return "", fmt.Errorf("failed to generate content: %v", err)
	}

	if resp == nil || len(resp.Candidates) == 0 {
		return "", fmt.Errorf("no response generated")
	}

	// Extract text from the response
	var content string
	for _, part := range resp.Candidates[0].Content.Parts {
		if text, ok := part.(genai.Text); ok {
			content = string(text)
			break
		}
	}

	if len(content) == 0 {
		return "", fmt.Errorf("empty response from Gemini")
	}
	// Strip language-specific code block markers if present
	if strings.HasPrefix(content, "```") {
		// Find the first newline to skip the opening marker line
		if idx := strings.Index(content, "\n"); idx >= 0 {
			content = content[idx+1:]
		}

		// Remove the closing marker if present
		if strings.HasSuffix(content, "```") {
			content = content[:len(content)-3]
		} else if idx := strings.LastIndex(content, "\n```"); idx >= 0 {
			content = content[:idx]
		}
	}

	if strings.HasPrefix(content, "ERROR:") {
		return "", fmt.Errorf("%s", strings.TrimPrefix(content, "ERROR:"))
	}

	return content, nil
}

func (t *PatchTool) Execute(ctx context.Context, input PatchInput) (PatchOutput, error) {
	log.Info("Starting patch operation", zap.String("path", input.Path))
	log.Debug("Diff content", zap.String("diff", input.Diff))

	if input.Diff == "" {
		log.Error("Empty diff provided")
		return PatchOutput{}, fmt.Errorf("diff is required")
	}

	// Read the original file
	content, err := iofs.ReadFile(t.FilteredFS, input.Path)
	if err != nil {
		log.Error("Failed to read file", zap.String("path", input.Path), zap.Error(err))
		return PatchOutput{}, fmt.Errorf("failed to read file: %w", err)
	}
	originalContent := string(content)
	log.Debug("Read file content", zap.String("path", input.Path), zap.Int("bytes", len(content)))

	// First try to apply the patch using simplediff
	log.Debug("Attempting to apply patch programmatically")
	result, err := simplediff.ApplyDiff(originalContent, input.Diff)
	if err != nil {
		log.Warn("Failed to apply patch programmatically, falling back to Gemini", zap.Error(err))
		// Fall back to Gemini
		result, err = t.applyPatchWithGemini(ctx, originalContent, input.Diff)
		if err != nil {
			log.Error("Failed to apply patch with Gemini", zap.Error(err))
			return PatchOutput{}, fmt.Errorf("failed to apply patch: %w", err)
		}
	}

	// Write the modified content back to the file
	err = os.WriteFile(input.Path, []byte(result), 0644)
	if err != nil {
		log.Error("Failed to write file", zap.String("path", input.Path), zap.Error(err))
		return PatchOutput{}, fmt.Errorf("failed to write file: %w", err)
	}
	log.Info("Successfully wrote modified content", zap.String("path", input.Path), zap.Int("bytes", len(result)))

	return PatchOutput{}, nil
}
