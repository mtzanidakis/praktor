package telegram

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestChunkMessage(t *testing.T) {
	// Short message
	chunks := chunkMessage("hello", 4096)
	if len(chunks) != 1 {
		t.Errorf("expected 1 chunk, got %d", len(chunks))
	}

	// Exact limit
	msg := make([]byte, 4096)
	for i := range msg {
		msg[i] = 'a'
	}
	chunks = chunkMessage(string(msg), 4096)
	if len(chunks) != 1 {
		t.Errorf("expected 1 chunk for exact limit, got %d", len(chunks))
	}

	// Over limit
	msg = make([]byte, 8192)
	for i := range msg {
		msg[i] = 'a'
	}
	chunks = chunkMessage(string(msg), 4096)
	if len(chunks) != 2 {
		t.Errorf("expected 2 chunks, got %d", len(chunks))
	}

	// Split at newline
	msg = make([]byte, 5000)
	for i := range msg {
		msg[i] = 'a'
	}
	msg[3000] = '\n'
	chunks = chunkMessage(string(msg), 4096)
	if len(chunks) != 2 {
		t.Errorf("expected 2 chunks with newline split, got %d", len(chunks))
	}
	if len(chunks[0]) != 3001 { // Up to and including the newline
		t.Errorf("expected first chunk length 3001, got %d", len(chunks[0]))
	}
}

func TestToTelegramMarkdown(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"**bold**", "*bold*"},
		{"hello **world**!", "hello *world*!"},
		{"**a** and **b**", "*a* and *b*"},
		{"no bold here", "no bold here"},
		{"*already single*", "*already single*"},
	}
	for _, tt := range tests {
		got := toTelegramMarkdown(tt.in)
		if got != tt.want {
			t.Errorf("toTelegramMarkdown(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestConvertMarkdownTables(t *testing.T) {
	input := "Here is a table:\n| Name | Value |\n|---|---|\n| BTC | $67,671 |\n| ETH | $3,052 |\n\nEnd."
	got := convertMarkdownTables(input)

	// Should contain pre-formatted block
	if !strings.Contains(got, "```") {
		t.Errorf("expected pre-formatted block, got:\n%s", got)
	}
	// Should preserve surrounding text
	if !strings.Contains(got,"Here is a table:") || !strings.Contains(got,"End.") {
		t.Errorf("surrounding text lost, got:\n%s", got)
	}
	// Should not contain raw pipe table syntax
	if strings.Contains(got,"|---|") {
		t.Errorf("separator row not removed, got:\n%s", got)
	}
	// Should contain box-drawing separator
	if !strings.Contains(got,"─┼─") {
		t.Errorf("expected box-drawing separator, got:\n%s", got)
	}
}

func TestConvertMarkdownTablesUnicodeAndBold(t *testing.T) {
	input := "| Ζεύγος | Τιμή |\n|---|---|\n| **BTC** | $68,500 |\n| **ETH** | $2,055 |"
	got := convertMarkdownTables(input)

	// Bold markers should be stripped inside pre block
	if strings.Contains(got, "**") || strings.Contains(got, "*BTC*") {
		t.Errorf("bold markers not stripped, got:\n%s", got)
	}
	// Check alignment: all data rows should have the same rune width
	lines := strings.Split(got, "\n")
	var dataWidths []int
	for _, line := range lines {
		if strings.Contains(line, "│") && !strings.Contains(line, "┼") {
			dataWidths = append(dataWidths, utf8.RuneCountInString(line))
		}
	}
	for i := 1; i < len(dataWidths); i++ {
		if dataWidths[i] != dataWidths[0] {
			t.Errorf("misaligned rows: line 0 has %d runes, line %d has %d runes\n%s",
				dataWidths[0], i, dataWidths[i], got)
		}
	}
}

func TestConvertMarkdownTablesNoTable(t *testing.T) {
	input := "No tables here.\nJust text."
	got := convertMarkdownTables(input)
	if got != input {
		t.Errorf("expected unchanged text, got:\n%s", got)
	}
}

