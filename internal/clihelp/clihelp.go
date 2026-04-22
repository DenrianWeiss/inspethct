package clihelp

import (
	"flag"
	"fmt"
	"io"
	"strings"
)

const defaultWidth = 88

type Doc struct {
	Usage    string
	Summary  []string
	Sections []Section
}

type Section struct {
	Title      string
	Paragraphs []string
	Entries    []Entry
}

type Entry struct {
	Label   string
	Summary string
	Details []string
}

func Render(writer io.Writer, doc Doc) {
	if strings.TrimSpace(doc.Usage) != "" {
		_, _ = fmt.Fprintf(writer, "Usage: %s\n", doc.Usage)
	}
	for _, paragraph := range doc.Summary {
		if strings.TrimSpace(paragraph) == "" {
			continue
		}
		_, _ = fmt.Fprintln(writer)
		for _, line := range wrap(paragraph, defaultWidth) {
			_, _ = fmt.Fprintln(writer, line)
		}
	}
	for _, section := range doc.Sections {
		_, _ = fmt.Fprintln(writer)
		if strings.TrimSpace(section.Title) != "" {
			_, _ = fmt.Fprintln(writer, section.Title)
		}
		for _, paragraph := range section.Paragraphs {
			for _, line := range wrap(paragraph, defaultWidth-2) {
				_, _ = fmt.Fprintln(writer, "  "+line)
			}
		}
		if len(section.Paragraphs) > 0 && len(section.Entries) > 0 {
			_, _ = fmt.Fprintln(writer)
		}
		writeEntries(writer, section.Entries)
	}
}

func RenderFlagSet(writer io.Writer, title string, flags *flag.FlagSet) {
	entries := make([]Entry, 0)
	flags.VisitAll(func(item *flag.Flag) {
		valueName, usage := flag.UnquoteUsage(item)
		label := "-" + item.Name
		if valueName != "" {
			label += " " + valueName
		}
		details := make([]string, 0, 1)
		if item.DefValue != "" && item.DefValue != "false" {
			details = append(details, fmt.Sprintf("Default: %s", item.DefValue))
		}
		entries = append(entries, Entry{Label: label, Summary: usage, Details: details})
	})
	if len(entries) == 0 {
		return
	}
	if strings.TrimSpace(title) != "" {
		_, _ = fmt.Fprintln(writer)
		_, _ = fmt.Fprintln(writer, title)
	}
	writeEntries(writer, entries)
}

func writeEntries(writer io.Writer, entries []Entry) {
	maxLabel := 0
	for _, entry := range entries {
		if length := len(entry.Label); length > maxLabel {
			maxLabel = length
		}
	}
	if maxLabel > 26 {
		maxLabel = 26
	}
	if maxLabel < 14 {
		maxLabel = 14
	}
	for _, entry := range entries {
		writeEntry(writer, entry, maxLabel)
	}
}

func writeEntry(writer io.Writer, entry Entry, labelWidth int) {
	indent := "  "
	if len(entry.Label) > labelWidth {
		_, _ = fmt.Fprintln(writer, indent+entry.Label)
		for _, line := range wrap(entry.Summary, defaultWidth-len(indent)-4) {
			_, _ = fmt.Fprintln(writer, indent+"    "+line)
		}
	} else {
		padding := strings.Repeat(" ", labelWidth-len(entry.Label))
		lines := wrap(entry.Summary, defaultWidth-len(indent)-labelWidth-2)
		if len(lines) == 0 {
			lines = []string{""}
		}
		_, _ = fmt.Fprintln(writer, indent+entry.Label+padding+"  "+lines[0])
		for _, line := range lines[1:] {
			_, _ = fmt.Fprintln(writer, indent+strings.Repeat(" ", labelWidth)+"  "+line)
		}
	}
	for _, detail := range entry.Details {
		for _, line := range wrap(detail, defaultWidth-len(indent)-4) {
			_, _ = fmt.Fprintln(writer, indent+"    "+line)
		}
	}
}

func wrap(text string, width int) []string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return nil
	}
	if width <= 0 {
		return []string{trimmed}
	}
	words := strings.Fields(trimmed)
	if len(words) == 0 {
		return nil
	}
	lines := make([]string, 0, 4)
	current := words[0]
	for _, word := range words[1:] {
		if len(current)+1+len(word) > width {
			lines = append(lines, current)
			current = word
			continue
		}
		current += " " + word
	}
	lines = append(lines, current)
	return lines
}
