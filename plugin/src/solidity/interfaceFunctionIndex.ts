import * as vscode from "vscode";

export interface InterfaceFunctionEntry {
  interfaceName: string;
  signature: string;
  line: number;
}

export interface InterfaceRange {
  interfaceName: string;
  startLine: number;
  endLine: number;
}

export interface InterfaceBreakpointIndex {
  functions: InterfaceFunctionEntry[];
  ranges: InterfaceRange[];
}

export function collectInterfaceBreakpointIndex(document: vscode.TextDocument): InterfaceBreakpointIndex {
  const text = document.getText();
  const functions: InterfaceFunctionEntry[] = [];
  const ranges: InterfaceRange[] = [];
  const interfaceRegex = /\binterface\s+([A-Za-z_][A-Za-z0-9_]*)\b/g;

  let match: RegExpExecArray | null;
  while ((match = interfaceRegex.exec(text)) !== null) {
    const interfaceName = match[1];
    const bodyOpen = findNextBrace(text, interfaceRegex.lastIndex);
    if (bodyOpen < 0) {
      continue;
    }
    const bodyClose = findClosingBrace(text, bodyOpen);
    if (bodyClose < 0) {
      continue;
    }

    const startLine = document.positionAt(match.index).line;
    const endLine = document.positionAt(bodyClose).line;
    ranges.push({ interfaceName, startLine, endLine });

    const body = text.slice(bodyOpen + 1, bodyClose);
    const functionRegex = /\bfunction\s+([A-Za-z_][A-Za-z0-9_]*)\s*\(/g;
    let fnMatch: RegExpExecArray | null;
    while ((fnMatch = functionRegex.exec(body)) !== null) {
      const name = fnMatch[1];
      const openParen = bodyOpen + 1 + functionRegex.lastIndex - 1;
      const closeParen = findClosingParen(text, openParen);
      if (closeParen < 0 || closeParen > bodyClose) {
        continue;
      }
      const params = text.slice(openParen + 1, closeParen);
      const signature = `${name}(${normalizeParameterList(params)})`;
      const line = document.positionAt(bodyOpen + 1 + fnMatch.index).line;
      functions.push({ interfaceName, signature, line });
    }

    interfaceRegex.lastIndex = bodyClose + 1;
  }

  return { functions, ranges };
}

function findNextBrace(text: string, from: number): number {
  for (let i = from; i < text.length; i++) {
    if (text[i] === "{") {
      return i;
    }
    if (text[i] === ";") {
      return -1;
    }
  }
  return -1;
}

function findClosingBrace(text: string, openIndex: number): number {
  let depth = 0;
  for (let i = openIndex; i < text.length; i++) {
    const char = text[i];
    if (char === "{") {
      depth++;
      continue;
    }
    if (char === "}") {
      depth--;
      if (depth === 0) {
        return i;
      }
    }
  }
  return -1;
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