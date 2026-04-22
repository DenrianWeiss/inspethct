import * as vscode from "vscode";

export interface SolidityFunctionEntry {
  name: string;
  signature: string;
  line: number;
}

export function collectSolidityFunctions(document: vscode.TextDocument): SolidityFunctionEntry[] {
  const text = document.getText();
  const entries: SolidityFunctionEntry[] = [];
  const regex = /function\s+([A-Za-z_][A-Za-z0-9_]*)\s*\(/g;

  let match: RegExpExecArray | null;
  while ((match = regex.exec(text)) !== null) {
    const name = match[1];
    const openParen = regex.lastIndex - 1;
    const closeParen = findClosingParen(text, openParen);
    if (closeParen < 0) {
      continue;
    }
    const params = text.slice(openParen + 1, closeParen);
    const signature = `${name}(${normalizeParameterList(params)})`;
    const line = document.positionAt(match.index).line;
    entries.push({ name, signature, line });
  }
  return entries;
}

function findClosingParen(text: string, openIndex: number): number {
  let depth = 0;
  for (let index = openIndex; index < text.length; index++) {
    const char = text[index];
    if (char === "(") {
      depth++;
      continue;
    }
    if (char === ")") {
      depth--;
      if (depth === 0) {
        return index;
      }
    }
  }
  return -1;
}

function normalizeParameterList(raw: string): string {
  const parts = splitTopLevel(raw);
  return parts
    .map((part) => normalizeParam(part.trim()))
    .filter((part) => part.length > 0)
    .join(",");
}

function splitTopLevel(input: string): string[] {
  const parts: string[] = [];
  let start = 0;
  let depth = 0;
  for (let index = 0; index < input.length; index++) {
    const char = input[index];
    if (char === "(") {
      depth++;
    } else if (char === ")") {
      depth--;
    } else if (char === "," && depth === 0) {
      parts.push(input.slice(start, index));
      start = index + 1;
    }
  }
  const tail = input.slice(start).trim();
  if (tail.length > 0) {
    parts.push(tail);
  }
  return parts;
}

function normalizeParam(raw: string): string {
  if (raw.length === 0) {
    return "";
  }
  const trimmed = raw.replace(/\b(memory|calldata|storage|payable)\b/g, "").replace(/\s+/g, " ").trim();
  const tokens = trimmed.split(" ").filter(Boolean);
  if (tokens.length === 0) {
    return "";
  }
  if (tokens.length === 1) {
    return tokens[0];
  }
  return tokens[0];
}
