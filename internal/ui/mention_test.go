package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMentionTokens(t *testing.T) {
	got := mentionTokens("explain @go.mod and @selection, plus @src/ and again @go.mod")
	want := []string{"@go.mod", "@selection", "@src/"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("tok[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestMentionTokensIgnoresEmailsAndPunctuation(t *testing.T) {
	got := mentionTokens("email foo@bar.com wrote @README.md, see?")
	if len(got) != 1 || got[0] != "@README.md" {
		t.Fatalf("got %v, want [@README.md]", got)
	}
}

func TestResolvePathMention(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "hello.txt")
	if err := os.WriteFile(file, []byte("line1\nline2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sub", "x.go"), []byte("package main"), 0o644); err != nil {
		t.Fatal(err)
	}

	// file
	m := resolvePathMention("@hello.txt", dir)
	if m.Err != "" || m.Text != "line1\nline2" {
		t.Errorf("file mention = %+v", m)
	}
	// missing
	m = resolvePathMention("@nope.txt", dir)
	if m.Err == "" {
		t.Errorf("missing file should error, got %+v", m)
	}

	// directory listing
	m = resolvePathMention("@sub/", dir)
	if m.Err != "" || m.Kind != "dir" {
		t.Fatalf("dir mention = %+v", m)
	}
	if !strings.Contains(m.Text, "x.go") {
		t.Errorf("listing should contain x.go: %q", m.Text)
	}

	// binary refusal
	bin := filepath.Join(dir, "blob.bin")
	if err := os.WriteFile(bin, []byte{'a', 0, 'b'}, 0o644); err != nil {
		t.Fatal(err)
	}
	m = resolvePathMention("@blob.bin", dir)
	if m.Err == "" || !strings.Contains(m.Err, "binary") {
		t.Errorf("binary mention should refuse, got %+v", m)
	}
}

func TestReadFileCapped(t *testing.T) {
	dir := t.TempDir()
	// generate a file over the byte cap
	var big strings.Builder
	for i := 0; i < refMaxBytes/10+50; i++ {
		big.WriteString("0123456789")
	}
	p := filepath.Join(dir, "big.txt")
	if err := os.WriteFile(p, []byte(big.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	data, note, err := readFileCapped(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) > refMaxBytes {
		t.Errorf("read %d bytes, cap is %d", len(data), refMaxBytes)
	}
	if note == "" {
		t.Error("expected truncation note")
	}
}

func TestBuildCompletions(t *testing.T) {
	dir := t.TempDir()
	touch := func(name string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	touch("app.go")
	touch("app_test.go")
	touch("readme.md")
	touch(".hidden")
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sub", "x.go"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	// bare @: selection item + entries
	items := buildCompletions("@", dir, true)
	if items[0].label != "selection" || items[0].insert != "@selection" {
		t.Errorf("first item should be selection, got %+v", items[0])
	}
	var labels []string
	for _, it := range items {
		labels = append(labels, it.label)
	}
	joined := strings.Join(labels, ",")
	for _, want := range []string{"app.go", "readme.md", "sub/"} {
		if !strings.Contains(joined, want) {
			t.Errorf("completions missing %q: %v", want, labels)
		}
	}
	if strings.Contains(joined, ".hidden") {
		t.Errorf("hidden files should be skipped: %v", labels)
	}

	// prefix filter
	items = buildCompletions("@app", dir, true)
	if len(items) != 2 { // selection is filtered out too ("app" doesn't prefix "selection")
		t.Fatalf("expected 2 app* items, got %+v", items)
	}

	// selection only match
	items = buildCompletions("@sel", dir, false)
	if len(items) != 1 || items[0].label != "selection" {
		t.Fatalf("expected selection item, got %+v", items)
	}

	// subdirectory completion
	items = buildCompletions("@sub/", dir, true)
	if len(items) == 0 || items[0].insert != "@sub/x.go" {
		t.Fatalf("expected @sub/x.go completion, got %+v", items)
	}
	// no match keeps nothing but stays valid
	items = buildCompletions("@zzz", dir, true)
	if len(items) != 0 {
		t.Fatalf("expected no completions, got %+v", items)
	}
}

func TestMentionTokenFromInput(t *testing.T) {
	var in InputBar
	in.SetText("look at @go.mod please")
	// cursor at end → last token is "please", not a mention
	if _, ok := in.mentionToken(); ok {
		t.Error("cursor on plain word should not be a mention")
	}
	// move cursor into the token: pos 15 = just after "@go.mod"
	in.pos = 15
	tok, ok := in.mentionToken()
	if !ok || tok != "@go.mod" {
		t.Fatalf("got %q, %v", tok, ok)
	}
	// email mid-text: token scan hits "foo" first
	in.SetText("mail foo@bar.com now")
	in.pos = 12
	if _, ok := in.mentionToken(); ok {
		t.Error("email should not be a mention")
	}
}

func TestFormatRefBlock(t *testing.T) {
	ms := []mention{
		{Token: "@selection", Kind: "selection", Text: "hello output"},
		{Token: "@missing.txt", Err: "not found"},
	}
	block := formatRefBlock(ms)
	if !strings.Contains(block, "--- @selection (selection, included this turn only) ---") {
		t.Errorf("selection header missing: %q", block)
	}
	if !strings.Contains(block, "hello output") {
		t.Errorf("selection text missing")
	}
	if !strings.Contains(block, "@missing.txt: not found") {
		t.Errorf("error header missing: %q", block)
	}
}

func TestResolveSelectionMentionNoShell(t *testing.T) {
	var a App // no shell attached
	ms := a.resolveMentions("explain @selection")
	if len(ms) != 1 || ms[0].Err == "" {
		t.Fatalf("expected selection error without shell, got %+v", ms)
	}
}
