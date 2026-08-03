package app

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestWriteToPipeIsUnframed pins the bytes a script or another program sees: the
// blank lines that frame terminal output must not reach a pipe or a redirect.
func TestWriteToPipeIsUnframed(t *testing.T) {
	a := New(Flags{})
	var out bytes.Buffer
	a.Stdout = &out

	if err := a.Write("first"); err != nil {
		t.Fatal(err)
	}
	if err := a.Writef("second %d", 2); err != nil {
		t.Fatal(err)
	}
	if err := a.Flush(); err != nil {
		t.Fatal(err)
	}
	if got, want := out.String(), "first\nsecond 2"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestWriteToFileLeavesStdoutEmpty guards -f|--file: the frame is a terminal
// affordance and must not leak onto a stdout nobody is printing to.
func TestWriteToFileLeavesStdoutEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.md")
	a := New(Flags{File: path})
	var out bytes.Buffer
	a.Stdout = &out

	if err := a.Write("# Title"); err != nil {
		t.Fatal(err)
	}
	if err := a.Flush(); err != nil {
		t.Fatal(err)
	}
	if out.Len() != 0 {
		t.Errorf("stdout should stay empty, got %q", out.String())
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(b), "# Title\n"; got != want {
		t.Errorf("file = %q, want %q", got, want)
	}
}

// TestFlushWithoutOutput covers the interactive browser and any command that
// prints nothing: closing a frame that was never opened writes nothing.
func TestFlushWithoutOutput(t *testing.T) {
	a := New(Flags{})
	var out bytes.Buffer
	a.Stdout = &out
	for range 2 {
		if err := a.Flush(); err != nil {
			t.Fatal(err)
		}
	}
	if out.Len() != 0 {
		t.Errorf("Flush wrote %q with nothing to close", out.String())
	}
}

func TestWriteAddsTrailingNewline(t *testing.T) {
	a := New(Flags{})
	var out bytes.Buffer
	a.Stdout = &out
	if err := a.Write("no newline"); err != nil {
		t.Fatal(err)
	}
	if err := a.Write("has newline\n"); err != nil {
		t.Fatal(err)
	}
	if got, want := out.String(), "no newline\nhas newline\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestWriteEmptyIsNoop keeps an empty result from opening a frame of its own.
func TestWriteEmptyIsNoop(t *testing.T) {
	a := New(Flags{})
	var out bytes.Buffer
	a.Stdout = &out
	if err := a.Write(""); err != nil {
		t.Fatal(err)
	}
	if err := a.Writef(""); err != nil {
		t.Fatal(err)
	}
	if out.Len() != 0 {
		t.Errorf("empty writes produced %q", out.String())
	}
}

func TestTextFallsBackToFlagThenLocale(t *testing.T) {
	t.Setenv("LC_ALL", "de_DE.UTF-8")
	t.Setenv("LC_MESSAGES", "")
	t.Setenv("LANG", "")

	// no language resolved yet: the flag wins over the locale
	a := New(Flags{Lang: "en"})
	if got := a.Text().NoResults; !strings.EqualFold(got, "No results.") {
		t.Errorf("-l en should give English, got %q", got)
	}
	// no flag either: fall back to the system locale
	b := New(Flags{})
	if got := b.Text().NoResults; got != "Keine Ergebnisse." {
		t.Errorf("LC_ALL=de_DE should give German, got %q", got)
	}
}

// TestFrameableByFormat pins which formats may be framed. -o raw is the
// byte-for-byte escape hatch, so it never is; the terminal and -f|--file gates
// are applied separately.
func TestFrameableByFormat(t *testing.T) {
	for _, tc := range []struct {
		output string
		want   bool
	}{
		{"", true}, // default: markdown
		{"markdown", true},
		{"md", true},
		{"text", true},
		{"html", true},
		{"json", true},
		{"raw", false},
		{"bogus", false}, // unparseable: do not guess
	} {
		a := New(Flags{Output: tc.output})
		if got := a.frameable(); got != tc.want {
			t.Errorf("-o %q: frameable = %v, want %v", tc.output, got, tc.want)
		}
	}
}
