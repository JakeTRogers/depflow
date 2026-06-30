package terminal

import "testing"

func TestSanitizeStripsTerminalControlBytes(t *testing.T) {
	t.Parallel()

	input := "safe\x00\x1b]8;;https://example.test\x07link\x1b]8;;\x07\tline\nnext\rend\x7f"
	want := "safe]8;;https://example.testlink]8;;\tline\nnext\rend"

	if got := Sanitize(input); got != want {
		t.Fatalf("Sanitize() = %q, want %q", got, want)
	}
}

func TestSanitizeReturnsUnchangedStringWhenNothingToStrip(t *testing.T) {
	t.Parallel()

	input := "clean string with\ttab\nand newline"
	if got := Sanitize(input); got != input {
		t.Fatalf("Sanitize() = %q, want unchanged %q", got, input)
	}
}

func TestSanitizeEmptyString(t *testing.T) {
	t.Parallel()

	if got := Sanitize(""); got != "" {
		t.Fatalf("Sanitize(\"\") = %q, want empty string", got)
	}
}
