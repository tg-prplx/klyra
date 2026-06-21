package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	termansi "github.com/charmbracelet/x/ansi"
)

func modalPaddingY(termHeight int) int {
	if termHeight > 0 && termHeight <= 14 {
		return 0
	}
	return 1
}

func modalBoxWidth(termWidth, preferredWidth, widthPercent, minWidth, maxWidth int) int {
	boxWidth := preferredWidth
	if boxWidth <= 0 {
		boxWidth = minWidth
	}
	if termWidth > 0 {
		adaptive := termWidth * widthPercent / 100
		if maxWidth > 0 && adaptive > maxWidth {
			adaptive = maxWidth
		}
		if adaptive < minWidth {
			adaptive = max(32, termWidth-4)
		}
		if adaptive > boxWidth {
			boxWidth = adaptive
		}
		if boxWidth > termWidth-4 {
			boxWidth = termWidth - 4
		}
	}
	if boxWidth < minWidth {
		boxWidth = minWidth
	}
	return boxWidth
}

func modalMaxInnerHeight(termHeight, paddingY int) int {
	maxInnerHeight := termHeight - 4 - paddingY*2
	if maxInnerHeight < 2 {
		maxInnerHeight = 2
	}
	return maxInnerHeight
}

func wrapModalContent(content string, contentWidth int) []string {
	if contentWidth < 1 {
		contentWidth = 1
	}
	rawLines := strings.Split(content, "\n")
	wrappedLines := make([]string, 0, len(rawLines))
	for _, line := range rawLines {
		if line == "" {
			wrappedLines = append(wrappedLines, "")
			continue
		}
		wrapped := termansi.Wrap(line, contentWidth, "")
		parts := strings.Split(strings.TrimRight(wrapped, "\n"), "\n")
		if len(parts) == 0 {
			wrappedLines = append(wrappedLines, "")
			continue
		}
		wrappedLines = append(wrappedLines, parts...)
	}
	return wrappedLines
}

func clampRenderedLines(lines []string, maxLines int) []string {
	if maxLines < 1 {
		maxLines = 1
	}
	if len(lines) <= maxLines {
		return lines
	}
	return lines[:maxLines]
}

func renderModalFrame(termWidth, termHeight, preferredWidth, widthPercent, minWidth, maxWidth int, borderColor lipgloss.TerminalColor, content string) string {
	paddingY := modalPaddingY(termHeight)
	boxWidth := modalBoxWidth(termWidth, preferredWidth, widthPercent, minWidth, maxWidth)
	maxInnerHeight := modalMaxInnerHeight(termHeight, paddingY)
	contentWidth := max(1, boxWidth-4)
	wrappedLines := clampRenderedLines(wrapModalContent(content, contentWidth), maxInnerHeight)

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Foreground(colorText).
		Padding(paddingY, 2).
		Width(boxWidth).
		Render(strings.Join(wrappedLines, "\n"))
}
