package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"

	"github.com/russellhaering/autoswe/pkg/secrets"
)

// runSubcommand dispatches `autoswe <cmd> [args]` for login/logout/whoami.
// Returns an error to be printed by main(); a nil return is success.
func runSubcommand(cmd string, args []string) error {
	switch cmd {
	case "login":
		return runLogin(args)
	case "logout":
		return runLogout(args)
	case "whoami":
		return runWhoami(args)
	default:
		return fmt.Errorf("unknown subcommand: %s", cmd)
	}
}

func runLogin(args []string) error {
	fs := flag.NewFlagSet("login", flag.ContinueOnError)
	var provider string
	fs.StringVar(&provider, "provider", "anthropic", "provider to log in: anthropic or openai")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if !isStorableProvider(provider) {
		return fmt.Errorf("login: provider %q does not use stored tokens (bedrock uses the AWS credential chain)", provider)
	}
	token, err := promptToken(provider, os.Stdin, os.Stderr)
	if err != nil {
		return err
	}
	if token == "" {
		return errors.New("login: empty token")
	}
	if err := secrets.Set(provider, token); err != nil {
		return fmt.Errorf("login: keychain: %w", err)
	}
	fmt.Fprintf(os.Stderr, "[saved %s token to keychain]\n", provider)
	return nil
}

func runLogout(args []string) error {
	fs := flag.NewFlagSet("logout", flag.ContinueOnError)
	var provider string
	fs.StringVar(&provider, "provider", "anthropic", "provider to log out: anthropic or openai")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if !isStorableProvider(provider) {
		return fmt.Errorf("logout: provider %q does not use stored tokens", provider)
	}
	if err := secrets.Delete(provider); err != nil {
		return fmt.Errorf("logout: keychain: %w", err)
	}
	fmt.Fprintf(os.Stderr, "[removed %s token from keychain]\n", provider)
	return nil
}

func runWhoami(args []string) error {
	fs := flag.NewFlagSet("whoami", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return err
	}
	w := os.Stdout
	for _, p := range secrets.KnownProviders {
		fmt.Fprintf(w, "%-10s %s\n", p+":", statusOf(p))
	}
	fmt.Fprintln(w, "bedrock:   uses AWS credential chain (env / shared config / IAM role)")
	return nil
}

// statusOf reports the auth status for a provider: env, keychain, or
// not configured. Never echoes the token.
func statusOf(provider string) string {
	if envValue(provider) != "" {
		return fmt.Sprintf("env (%s set)", envVarFor(provider))
	}
	if _, ok, err := secrets.Get(provider); err == nil && ok {
		return "keychain"
	}
	return "not configured"
}

func envVarFor(provider string) string {
	switch provider {
	case "anthropic":
		return "ANTHROPIC_API_KEY"
	case "openai":
		return "OPENAI_API_KEY"
	default:
		return strings.ToUpper(provider) + "_API_KEY"
	}
}

func envValue(provider string) string {
	return os.Getenv(envVarFor(provider))
}

func isStorableProvider(p string) bool {
	for _, k := range secrets.KnownProviders {
		if k == p {
			return true
		}
	}
	return false
}

// promptToken prints "Enter <provider> API token: " on stderr and reads
// a single line of input. If stdin is a TTY the input is hidden via
// term.ReadPassword; otherwise it's read as-is so piped input still works.
func promptToken(provider string, in *os.File, out io.Writer) (string, error) {
	fmt.Fprintf(out, "Enter %s API token: ", provider)
	if term.IsTerminal(int(in.Fd())) {
		b, err := term.ReadPassword(int(in.Fd()))
		fmt.Fprintln(out)
		if err != nil {
			return "", fmt.Errorf("read token: %w", err)
		}
		return strings.TrimSpace(string(b)), nil
	}
	// Piped: read one line.
	var line []byte
	buf := make([]byte, 1)
	for {
		n, err := in.Read(buf)
		if n > 0 {
			if buf[0] == '\n' {
				break
			}
			line = append(line, buf[0])
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return "", fmt.Errorf("read token: %w", err)
		}
	}
	return strings.TrimSpace(string(line)), nil
}
