package secrets

import "testing"

func TestRoundTrip(t *testing.T) {
	UseMock()

	if _, ok, err := Get("anthropic"); err != nil || ok {
		t.Fatalf("expected miss; got ok=%v err=%v", ok, err)
	}
	if err := Set("anthropic", "tok-abc"); err != nil {
		t.Fatalf("set: %v", err)
	}
	tok, ok, err := Get("anthropic")
	if err != nil || !ok {
		t.Fatalf("expected hit; got ok=%v err=%v", ok, err)
	}
	if tok != "tok-abc" {
		t.Errorf("expected tok-abc; got %q", tok)
	}

	// Replace.
	if err := Set("anthropic", "tok-xyz"); err != nil {
		t.Fatalf("replace: %v", err)
	}
	tok, _, _ = Get("anthropic")
	if tok != "tok-xyz" {
		t.Errorf("replace failed; got %q", tok)
	}

	// Delete present → miss.
	if err := Delete("anthropic"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, ok, _ := Get("anthropic"); ok {
		t.Errorf("expected miss after delete")
	}

	// Delete missing → no error.
	if err := Delete("never-stored"); err != nil {
		t.Errorf("delete missing should not error; got %v", err)
	}
}

func TestIsolation(t *testing.T) {
	UseMock()
	if err := Set("anthropic", "a"); err != nil {
		t.Fatal(err)
	}
	if err := Set("openai", "o"); err != nil {
		t.Fatal(err)
	}
	a, _, _ := Get("anthropic")
	o, _, _ := Get("openai")
	if a != "a" || o != "o" {
		t.Errorf("cross-talk: anthropic=%q openai=%q", a, o)
	}
}
