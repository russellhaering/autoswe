// Package secrets is a thin wrapper over go-keyring for storing per-provider
// API tokens. The service name is "autoswe" and the user/account field is
// the provider name (e.g. "anthropic", "openai"). Tokens are encrypted at
// rest by the OS keychain; see README for the security caveats.
package secrets

import (
	"errors"

	"github.com/zalando/go-keyring"
)

// Service is the keychain "service" attribute we register under.
const Service = "autoswe"

// KnownProviders is the canonical list of providers we ask the user about
// from `autoswe whoami`. Storage isn't restricted to these — Set will
// accept any name.
var KnownProviders = []string{"anthropic", "openai"}

// Get returns (token, true, nil) if a token is stored, ("", false, nil)
// on miss. Other errors are real backend failures.
func Get(provider string) (string, bool, error) {
	tok, err := keyring.Get(Service, provider)
	if err != nil {
		if errors.Is(err, keyring.ErrNotFound) {
			return "", false, nil
		}
		return "", false, err
	}
	return tok, true, nil
}

// Set stores or replaces the token for the given provider.
func Set(provider, token string) error {
	return keyring.Set(Service, provider, token)
}

// Delete removes the stored token. Missing entries are not an error.
func Delete(provider string) error {
	err := keyring.Delete(Service, provider)
	if err == nil || errors.Is(err, keyring.ErrNotFound) {
		return nil
	}
	return err
}

// UseMock replaces the backend with an in-memory implementation. For tests.
func UseMock() {
	keyring.MockInit()
}
