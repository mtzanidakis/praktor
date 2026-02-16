package telegram

import (
	"regexp"
	"strings"
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

// toTelegramMarkdown converts standard Markdown bold (**text**) to
// Telegram Markdown v1 bold (*text*).
func toTelegramMarkdown(text string) string {
	return reBold.ReplaceAllString(text, "*$1*")
}
