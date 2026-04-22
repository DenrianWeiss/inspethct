package srcmap

import "strings"

// SourceName resolves a source identifier back to the canonical source path.
func (idx *Index) SourceName(sourceID int) string {
	if idx == nil {
		return ""
	}
	if file := idx.Sources[sourceID]; file != nil {
		return file.Name
	}
	return ""
}

// SourceFileByName returns the source file with the given canonical name.
func (idx *Index) SourceFileByName(name string) (*SourceFile, bool) {
	if idx == nil {
		return nil, false
	}
	for _, file := range idx.Sources {
		if file != nil && file.Name == name {
			return file, true
		}
	}
	return nil, false
}

// OffsetForLineColumn converts a 1-based line/column pair into a byte offset.
func (file *SourceFile) OffsetForLineColumn(line int, column int) (int, bool) {
	if file == nil || line <= 0 || column <= 0 {
		return 0, false
	}
	currentLine := 1
	currentColumn := 1
	for index, r := range file.Content {
		if currentLine == line && currentColumn == column {
			return index, true
		}
		if r == '\n' {
			currentLine++
			currentColumn = 1
		} else {
			currentColumn++
		}
	}
	if currentLine == line && currentColumn == column {
		return len(file.Content), true
	}
	return 0, false
}

// LineColumnForOffset converts a byte offset into a 1-based line/column pair.
func (file *SourceFile) LineColumnForOffset(offset int) (int, int) {
	if file == nil || offset <= 0 {
		return 1, 1
	}
	if offset > len(file.Content) {
		offset = len(file.Content)
	}
	line := 1
	column := 1
	for index, r := range file.Content {
		if index >= offset {
			break
		}
		if r == '\n' {
			line++
			column = 1
		} else {
			column++
		}
	}
	return line, column
}

func deriveABIMetadata(entries []ABIEntry) ([]FunctionDefinition, []EventDefinition) {
	functions := make([]FunctionDefinition, 0)
	events := make([]EventDefinition, 0)
	for _, entry := range entries {
		switch strings.ToLower(entry.Type) {
		case "function", "constructor", "fallback", "receive", "error":
			functions = append(functions, FunctionDefinition{
				Type:            entry.Type,
				Name:            entry.Name,
				StateMutability: entry.StateMutability,
				Inputs:          append([]ABIParameter(nil), entry.Inputs...),
				Outputs:         append([]ABIParameter(nil), entry.Outputs...),
			})
		case "event":
			events = append(events, EventDefinition{
				Name:      entry.Name,
				Anonymous: entry.Anonymous,
				Inputs:    append([]ABIParameter(nil), entry.Inputs...),
			})
		}
	}
	return functions, events
}
