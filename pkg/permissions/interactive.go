package permissions

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
)

// PromptResponse is what an Interactive prompter returns.
type PromptResponse int

const (
	PromptAllowOnce PromptResponse = iota
	PromptAllowAlways
	PromptDenyOnce
	PromptDenyAlways
	PromptAbort // signal: stop the whole agent run
)

// Prompter is the function Interactive consults for each tool call.
type Prompter func(ctx context.Context, call ToolCall) (PromptResponse, error)

// Interactive consults a Prompter for every call. Compose with Remembered to
// turn "always" answers into a session-scoped cache.
type Interactive struct {
	Prompt Prompter
}

// ErrUserAborted is returned by Interactive.Check when the user picks abort.
var ErrUserAborted = errors.New("permissions: user aborted")

func (i Interactive) Check(ctx context.Context, call ToolCall) (Decision, error) {
	if i.Prompt == nil {
		return Decision{}, errors.New("interactive: Prompt is nil")
	}
	resp, err := i.Prompt(ctx, call)
	if err != nil {
		return Decision{}, err
	}
	switch resp {
	case PromptAllowOnce:
		return Decision{Action: Allow}, nil
	case PromptAllowAlways:
		return Decision{Action: Allow, Remember: true}, nil
	case PromptDenyOnce:
		return Decision{Action: Deny, Reason: "user declined"}, nil
	case PromptDenyAlways:
		return Decision{Action: Deny, Reason: "user declined (cached)", Remember: true}, nil
	case PromptAbort:
		return Decision{}, ErrUserAborted
	default:
		return Decision{}, fmt.Errorf("interactive: unknown response %d", resp)
	}
}

// TTYPrompter returns a Prompter that writes to out and reads single-line
// responses from in:
//
//	y / <enter>  → allow once
//	a            → allow always (cached)
//	n            → deny once
//	d            → deny always (cached)
//	q            → abort run
func TTYPrompter(out io.Writer, in io.Reader) Prompter {
	reader := bufio.NewReader(in)
	return func(_ context.Context, call ToolCall) (PromptResponse, error) {
		args := string(call.Args)
		if len(args) > 200 {
			args = args[:200] + "…"
		}
		fmt.Fprintf(out, "\n[permissions] %s with args %s\n  allow once [y], always [a], deny once [n], deny always [d], quit [q] > ", call.Name, args)
		line, err := reader.ReadString('\n')
		if err != nil {
			if errors.Is(err, io.EOF) {
				return PromptDenyOnce, nil
			}
			return 0, err
		}
		switch strings.TrimSpace(line) {
		case "", "y", "Y":
			return PromptAllowOnce, nil
		case "a", "A":
			return PromptAllowAlways, nil
		case "n", "N":
			return PromptDenyOnce, nil
		case "d", "D":
			return PromptDenyAlways, nil
		case "q", "Q":
			return PromptAbort, nil
		default:
			return PromptDenyOnce, nil
		}
	}
}
