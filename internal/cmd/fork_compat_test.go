package cmd

import (
	"reflect"
	"strings"
	"testing"
)

func TestForkParagraphSelection(t *testing.T) {
	got, err := parseParagraphSelection("2-4,3,6", 6)
	if err != nil || !reflect.DeepEqual(got, []int{2, 3, 4, 6}) {
		t.Fatalf("selection: %v, %v", got, err)
	}
	for _, input := range []string{"0", "7", "4-2", "bad"} {
		if _, err := parseParagraphSelection(input, 6); err == nil {
			t.Fatalf("accepted invalid selection %q", input)
		}
	}
}

func TestForkFindParagraphRange(t *testing.T) {
	pm := &paragraphMap{Paragraphs: []docParagraph{
		{Num: 1, Text: "Intro"}, {Num: 2, Text: "Start here"},
		{Num: 3, Text: "Middle"}, {Num: 4, Text: "End here"},
	}}
	got, err := findParagraphRange(pm, "START", "end")
	if err != nil || !reflect.DeepEqual(got, []int{2, 3, 4}) {
		t.Fatalf("range: %v, %v", got, err)
	}
	if _, err := findParagraphRange(pm, "missing", ""); err == nil {
		t.Fatal("accepted missing match")
	}
}

func TestForkCommandsDryRun(t *testing.T) {
	for _, args := range [][]string{
		{"docs", "style", "doc1", "--paragraphs", "1-2", "--style", "h1"},
		{"drive", "create", "Example", "--type", "slides"},
		{"drive", "comments", "unresolve", "file1", "comment1"},
		{"docs", "comments", "unresolve", "doc1", "comment1"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			result := executeWithTestRuntime(t, append([]string{"--json", "--dry-run", "--account", "test@example.com"}, args...), nil)
			if result.err != nil {
				t.Fatalf("dry run failed: %v; %s", result.err, result.stderr)
			}
		})
	}
}
