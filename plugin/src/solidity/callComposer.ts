import * as vscode from "vscode";
import { FunctionFragment, Interface, ParamType } from "ethers";
import { LaunchCallConfig } from "../types";

const DEFAULT_FROM = "0x0000000000000000000000000000000000000001";

export async function composeCallFromFunction(signature: string): Promise<LaunchCallConfig | undefined> {
  const canonical = ensureFunctionPrefix(signature);
  let fragment: FunctionFragment;
  try {
    fragment = FunctionFragment.from(canonical);
  } catch (error) {
    void vscode.window.showErrorMessage(`Invalid function signature: ${String(error)}`);
    return undefined;
  }

  const to = await vscode.window.showInputBox({
    title: "Target contract address",
    placeHolder: "0x...",
    validateInput: (value) => (value.trim().startsWith("0x") ? undefined : "Address must start with 0x")
  });
  if (!to) {
    return undefined;
  }

  const from = await vscode.window.showInputBox({
    title: "Sender address (from)",
    value: DEFAULT_FROM,
    validateInput: (value) => (value.trim().startsWith("0x") ? undefined : "Address must start with 0x")
  });
  if (!from) {
    return undefined;
  }

  const value = await vscode.window.showInputBox({ title: "Call value (hex or decimal)", value: "0x0" });
  if (value === undefined) {
    return undefined;
  }

  const gas = await vscode.window.showInputBox({ title: "Gas limit (hex or decimal, optional)", value: "0x0" });
  if (gas === undefined) {
    return undefined;
  }

  const block = await vscode.window.showInputBox({ title: "Block tag/number", value: "latest" });
  if (!block) {
    return undefined;
  }

  const args: unknown[] = [];
  for (const input of fragment.inputs) {
    const valueForInput = await promptParamValue(input);
    if (valueForInput === undefined) {
      return undefined;
    }
    args.push(valueForInput);
  }

  const iface = new Interface([fragment]);
  const inputData = iface.encodeFunctionData(fragment.name, args);

  return {
    from,
    to,
    input: inputData,
    value,
    gas,
    block
  };
}

function ensureFunctionPrefix(signature: string): string {
  const trimmed = signature.trim();
  if (trimmed.startsWith("function ")) {
    return trimmed;
  }
  return `function ${trimmed}`;
}

async function promptParamValue(param: ParamType): Promise<unknown | undefined> {
  const label = `${param.name || "arg"}: ${param.format("full")}`;

  if (param.baseType === "array") {
    const raw = await vscode.window.showInputBox({
      title: `Input array for ${label}`,
      placeHolder: "JSON array"
    });
    if (raw === undefined) {
      return undefined;
    }
    let parsed: unknown;
    try {
      parsed = JSON.parse(raw);
    } catch {
      void vscode.window.showErrorMessage(`Invalid JSON array for ${label}`);
      return undefined;
    }
    if (!Array.isArray(parsed)) {
      void vscode.window.showErrorMessage(`Expected JSON array for ${label}`);
      return undefined;
    }
    const out: unknown[] = [];
    for (const element of parsed) {
      const normalized = normalizeValue(param.arrayChildren!, element);
      out.push(normalized);
    }
    return out;
  }

  if (param.baseType === "tuple") {
    const raw = await vscode.window.showInputBox({
      title: `Input tuple for ${label}`,
      placeHolder: "JSON object or JSON array"
    });
    if (raw === undefined) {
      return undefined;
    }
    let parsed: unknown;
    try {
      parsed = JSON.parse(raw);
    } catch {
      void vscode.window.showErrorMessage(`Invalid JSON tuple for ${label}`);
      return undefined;
    }
    return normalizeTuple(param, parsed);
  }

  const raw = await vscode.window.showInputBox({
    title: `Input ${label}`,
    placeHolder: "value"
  });
  if (raw === undefined) {
    return undefined;
  }
  return normalizePrimitive(param, raw);
}

function normalizeValue(param: ParamType, value: unknown): unknown {
  if (param.baseType === "array") {
    if (!Array.isArray(value)) {
      throw new Error(`Expected array for ${param.format("full")}`);
    }
    return value.map((item) => normalizeValue(param.arrayChildren!, item));
  }
  if (param.baseType === "tuple") {
    return normalizeTuple(param, value);
  }
  return normalizePrimitive(param, value);
}

function normalizeTuple(param: ParamType, value: unknown): unknown {
  const components = param.components ?? [];
  if (Array.isArray(value)) {
    return components.map((component, index) => normalizeValue(component, value[index]));
  }
  if (value && typeof value === "object") {
    const record = value as Record<string, unknown>;
    const hasNamed = components.every((component) => component.name && record[component.name] !== undefined);
    if (hasNamed) {
      const objectResult: Record<string, unknown> = {};
      for (const component of components) {
        objectResult[component.name] = normalizeValue(component, record[component.name]);
      }
      return objectResult;
    }
    return components.map((component, index) => normalizeValue(component, record[String(index)]));
  }
  throw new Error(`Invalid tuple value for ${param.format("full")}`);
}

function normalizePrimitive(param: ParamType, value: unknown): unknown {
  const text = typeof value === "string" ? value.trim() : value;
  const type = param.type;
  if (type === "bool") {
    if (typeof text === "boolean") {
      return text;
    }
    if (text === "true" || text === "1") {
      return true;
    }
    if (text === "false" || text === "0") {
      return false;
    }
    throw new Error(`Invalid bool value for ${param.format("full")}`);
  }
  if (type.startsWith("uint") || type.startsWith("int")) {
    if (typeof text === "bigint") {
      return text;
    }
    if (typeof text === "number") {
      return BigInt(text);
    }
    if (typeof text === "string") {
      if (text.startsWith("0x") || text.startsWith("-0x")) {
        return BigInt(text);
      }
      return BigInt(text);
    }
  }
  if (type === "string") {
    return String(text ?? "");
  }
  if (type === "address" || type.startsWith("bytes")) {
    return String(text ?? "");
  }
  return text;
}
