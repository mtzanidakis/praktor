package telegram

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

// chunkMessage splits a message into chunks that fit within Telegram's message size limit.
func chunkMessage(text string, maxLen int) []string {
	if len(text) <= maxLen {
		return []string{text}
	}

	var chunks []string
	for len(text) > 0 {
		if len(text) <= maxLen {
			chunks = append(chunks, text)
			break
		}

		// Try to split at a newline
		cutAt := maxLen
		if idx := strings.LastIndex(text[:maxLen], "\n"); idx > maxLen/2 {
			cutAt = idx + 1
		}

		chunks = append(chunks, text[:cutAt])
		text = text[cutAt:]
	}

	return chunks
}

var reBold = regexp.MustCompile(`\*\*(.+?)\*\*`)

// toTelegramMarkdown converts standard Markdown to Telegram Markdown v1.
// It converts bold (**text** → *text*) and renders tables as monospace blocks.
func toTelegramMarkdown(text string) string {
	text = convertMarkdownTables(text)
	return reBold.ReplaceAllString(text, "*$1*")
}

// convertMarkdownTables finds Markdown tables and converts them to
// pre-formatted monospace blocks for Telegram.
func convertMarkdownTables(text string) string {
	lines := strings.Split(text, "\n")
	var result []string
	i := 0
	for i < len(lines) {
		// Detect start of a table: line with pipes
		if isTableRow(lines[i]) && i+1 < len(lines) && isSeparatorRow(lines[i+1]) {
			// Collect all table lines
			var rows [][]string
			header := parseTableRow(lines[i])
			rows = append(rows, header)
			i++ // skip header
			i++ // skip separator
			for i < len(lines) && isTableRow(lines[i]) {
				rows = append(rows, parseTableRow(lines[i]))
				i++
			}
			result = append(result, formatTable(rows))
			continue
		}
		result = append(result, lines[i])
		i++
	}
	return strings.Join(result, "\n")
}

func isTableRow(line string) bool {
	trimmed := strings.TrimSpace(line)
	return strings.HasPrefix(trimmed, "|") && strings.HasSuffix(trimmed, "|")
}

func isSeparatorRow(line string) bool {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "|") {
		return false
	}
	for _, ch := range trimmed {
		if ch != '|' && ch != '-' && ch != ':' && ch != ' ' {
			return false
		}
	}
	return true
}

func parseTableRow(line string) []string {
	trimmed := strings.TrimSpace(line)
	// Remove leading/trailing pipes
	trimmed = strings.TrimPrefix(trimmed, "|")
	trimmed = strings.TrimSuffix(trimmed, "|")
	parts := strings.Split(trimmed, "|")
	for i, p := range parts {
		cell := strings.TrimSpace(p)
		// Strip bold markers (**text**) since pre blocks render them literally
		cell = reBold.ReplaceAllString(cell, "$1")
		parts[i] = cell
	}
	return parts
}

func formatTable(rows [][]string) string {
	if len(rows) == 0 {
		return ""
	}
	// Calculate column widths
	cols := len(rows[0])
	widths := make([]int, cols)
	for _, row := range rows {
		for j := 0; j < cols && j < len(row); j++ {
			w := utf8.RuneCountInString(row[j])
			if w > widths[j] {
				widths[j] = w
			}
		}
	}

	var sb strings.Builder
	sb.WriteString("```\n")
	for ri, row := range rows {
		for j := 0; j < cols; j++ {
			cell := ""
			if j < len(row) {
				cell = row[j]
			}
			if j > 0 {
				sb.WriteString(" │ ")
			}
			sb.WriteString(cell)
			// Pad with spaces (using rune count for correct alignment)
			for k := utf8.RuneCountInString(cell); k < widths[j]; k++ {
				sb.WriteByte(' ')
			}
		}
		sb.WriteByte('\n')
		// Draw separator after header
		if ri == 0 {
			for j := 0; j < cols; j++ {
				if j > 0 {
					sb.WriteString("─┼─")
				}
				for k := 0; k < widths[j]; k++ {
					sb.WriteString("─")
				}
			}
			sb.WriteByte('\n')
		}
	}
	sb.WriteString("```")
	return sb.String()
}
