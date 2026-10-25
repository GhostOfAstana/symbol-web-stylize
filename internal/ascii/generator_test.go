package ascii

import (
	"errors"
	"io/fs"
	"os"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
)

func fixture(t testing.TB) *Generator {
	t.Helper()
	g, err := New(os.DirFS("../.."))
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func TestCanonicalHello(t *testing.T) {
	g := fixture(t)
	got, err := g.Generate("hello", "standard")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(got, "\n"), "\n")
	if len(got) != 256 || len(lines) != 8 {
		t.Fatalf("hello: bytes=%d lines=%d", len(got), len(lines))
	}
	for _, line := range lines {
		if len(line) != 31 {
			t.Fatalf("hello width=%d", len(line))
		}
	}
}

func TestFontsAndNewlines(t *testing.T) {
	g := fixture(t)
	for _, name := range []string{"standard", "shadow", "thinkertoy"} {
		t.Run(name, func(t *testing.T) {
			for _, text := range []string{"", "\n", "\n\n", "hello", "A\nB", "A\r\nB", `A\nB`, " !\"#$%&'()*+,-./0123456789:;<=>?@ABCDEFGHIJKLMNOPQRSTUVWXYZ[\\]^_`abcdefghijklmnopqrstuvwxyz{|}~"} {
				got, err := g.Generate(text, name)
				if err != nil {
					t.Fatalf("%q: %v", text, err)
				}
				if text == "" && got != "" {
					t.Fatalf("empty input=%q", got)
				}
				if (text == "\n" || text == "\n\n") && got != text {
					t.Fatalf("blank lines=%q", got)
				}
			}
			lf, _ := g.Generate("A\nB", name)
			crlf, _ := g.Generate("A\r\nB", name)
			literal, _ := g.Generate(`A\nB`, name)
			if lf != crlf || lf != literal {
				t.Fatal("newline normalization differs")
			}
			plain, _ := g.Generate("A", name)
			trailing, _ := g.Generate("A\n", name)
			if plain != trailing {
				t.Fatal("trailing separator created an extra line")
			}
		})
	}
}

func TestInputErrors(t *testing.T) {
	g := fixture(t)
	for _, tc := range []struct {
		name, text, banner string
		want               error
	}{
		{"font", "hello", "../../etc/passwd", ErrUnknownBanner},
		{"long", strings.Repeat("A", MaxInputBytes+1), "standard", ErrInputTooLong},
		{"unicode", "Привет", "standard", ErrUnsupportedCharacter},
		{"tab", "a\tb", "standard", ErrUnsupportedCharacter},
		{"control", "\x00", "standard", ErrUnsupportedCharacter},
		{"bare carriage return", "a\rb", "standard", ErrUnsupportedCharacter},
		{"invalid UTF8", string([]byte{0xff}), "standard", ErrUnsupportedCharacter},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := g.Generate(tc.text, tc.banner)
			if !errors.Is(err, tc.want) || got != "" {
				t.Fatalf("result=%q error=%v", got, err)
			}
		})
	}
	if _, err := (*Generator)(nil).Generate("A", "standard"); err == nil {
		t.Fatal("nil generator accepted")
	}
}

func TestOutputLimit(t *testing.T) {
	g := fixture(t)
	font := g.fonts["standard"]
	for i := range font {
		for j := range font[i] {
			font[i][j] = strings.Repeat("x", maxGlyphWidth)
		}
	}
	g.fonts["standard"] = font
	if got, err := g.Generate(strings.Repeat("A", MaxInputBytes), "standard"); got != "" || !errors.Is(err, ErrOutputTooLarge) {
		t.Fatalf("size limit: len=%d err=%v", len(got), err)
	}
}

func TestMalformedFonts(t *testing.T) {
	data, err := os.ReadFile("../../standard.txt")
	if err != nil {
		t.Fatal(err)
	}
	makeFS := func(content []byte) fs.FS {
		return fstest.MapFS{"standard.txt": {Data: content}, "shadow.txt": {Data: data}, "thinkertoy.txt": {Data: data}}
	}
	for _, data := range [][]byte{nil, []byte("bad"), []byte(strings.Replace(string(data), "\n", "x\n", 1)), []byte(strings.Repeat("x", maxFontBytes+1))} {
		if _, err := New(makeFS(data)); err == nil {
			t.Fatal("damaged font accepted")
		}
	}
	if _, err := New(fstest.MapFS{}); err == nil {
		t.Fatal("missing fonts accepted")
	}
	if _, err := New(nil); err == nil {
		t.Fatal("nil FS accepted")
	}
	// CRLF и отсутствие последнего LF не меняют содержимое шрифта.
	for _, variant := range []string{strings.ReplaceAll(string(data), "\n", "\r\n"), strings.TrimSuffix(string(data), "\n")} {
		if _, err := New(makeFS([]byte(variant))); err != nil {
			t.Fatal(err)
		}
	}
}

func TestConcurrentGeneration(t *testing.T) {
	g := fixture(t)
	want, _ := g.Generate("hello", "standard")
	var group sync.WaitGroup
	for range 64 {
		group.Add(1)
		go func() {
			defer group.Done()
			for range 20 {
				got, err := g.Generate("hello", "standard")
				if err != nil || got != want {
					t.Errorf("concurrent generation: %v", err)
					return
				}
			}
		}()
	}
	group.Wait()
}

func FuzzGenerate(f *testing.F) {
	g := fixture(f)
	for _, input := range []string{"hello", "\n\n", "<script>", "Привет", string([]byte{0xff}), "A\r\nB"} {
		f.Add(input, "standard")
	}
	f.Fuzz(func(t *testing.T, input, banner string) {
		got, err := g.Generate(input, banner)
		if err == nil && len(got) > MaxOutputBytes {
			t.Fatal("output exceeded limit")
		}
		if err != nil && got != "" {
			t.Fatal("partial result returned with error")
		}
	})
}
