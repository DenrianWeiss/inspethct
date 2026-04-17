package srcmap

import (
	"fmt"
	"strconv"
	"strings"
)

func parseAST(raw map[string]any) *ASTNode {
	if len(raw) == 0 {
		return nil
	}
	root, ok := buildASTNode(raw, 0)
	if !ok {
		return nil
	}
	return root
}

func buildASTNode(raw map[string]any, parentID int) (*ASTNode, bool) {
	id, ok := asInt(raw["id"])
	if !ok {
		return nil, false
	}
	nodeType, _ := raw["nodeType"].(string)
	src, err := parseSourceRange(asString(raw["src"]))
	if err != nil {
		src = SourceRange{SourceID: -1}
	}
	name := firstNonEmptyString(raw, "name", "canonicalName", "memberName")
	node := &ASTNode{
		ID:       id,
		NodeType: nodeType,
		Name:     name,
		Src:      src,
		ParentID: parentID,
		Raw:      raw,
	}
	for _, child := range collectChildObjects(raw) {
		childNode, ok := buildASTNode(child, id)
		if !ok {
			continue
		}
		node.Children = append(node.Children, childNode)
	}
	return node, true
}

func collectChildObjects(raw map[string]any) []map[string]any {
	children := make([]map[string]any, 0)
	for key, value := range raw {
		switch typed := value.(type) {
		case map[string]any:
			if _, hasID := typed["id"]; hasID {
				children = append(children, typed)
			}
		case []any:
			for _, item := range typed {
				child, ok := item.(map[string]any)
				if !ok {
					continue
				}
				if _, hasID := child["id"]; hasID {
					children = append(children, child)
				}
			}
		}
		_ = key
	}
	return children
}

func parseSourceRange(value string) (SourceRange, error) {
	parts := strings.Split(value, ":")
	if len(parts) < 3 {
		return SourceRange{}, fmt.Errorf("invalid source range %q", value)
	}
	start, err := strconv.Atoi(parts[0])
	if err != nil {
		return SourceRange{}, err
	}
	length, err := strconv.Atoi(parts[1])
	if err != nil {
		return SourceRange{}, err
	}
	fileID, err := strconv.Atoi(parts[2])
	if err != nil {
		return SourceRange{}, err
	}
	return SourceRange{SourceID: fileID, Start: start, Length: length}, nil
}

func firstNonEmptyString(raw map[string]any, keys ...string) string {
	for _, key := range keys {
		if value := asString(raw[key]); value != "" {
			return value
		}
	}
	return ""
}

func asString(value any) string {
	s, _ := value.(string)
	return s
}

func asInt(value any) (int, bool) {
	switch typed := value.(type) {
	case float64:
		return int(typed), true
	case int:
		return typed, true
	case int64:
		return int(typed), true
	case string:
		parsed, err := strconv.Atoi(typed)
		if err != nil {
			return 0, false
		}
		return parsed, true
	default:
		return 0, false
	}
}