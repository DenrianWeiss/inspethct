import * as vscode from "vscode";
import { Fragment, FunctionFragment, Interface } from "ethers";

export interface AbiFunctionItem {
  signature: string;
  display: string;
}

export interface AbiFunctionInput {
  name: string;
  type: string;
}

export interface AbiArtifactItem {
  id: string;
  label: string;
  sourcePath: string;
  abi: string;
  functions: AbiFunctionItem[];
}

interface ArtifactLike {
  contractName?: string;
  sourceName?: string;
  abi?: unknown;
}

export async function scanWorkspaceAbiCatalog(folder: vscode.WorkspaceFolder): Promise<AbiArtifactItem[]> {
  const uris = await vscode.workspace.findFiles(
    new vscode.RelativePattern(folder, "{out,artifacts}/**/*.json"),
    "**/{node_modules,cache,.git}/**",
    2000
  );

  const artifacts: AbiArtifactItem[] = [];
  for (const uri of uris) {
    const maybeArtifact = await readArtifactFile(uri);
    if (!maybeArtifact) {
      continue;
    }

    const abiJson = JSON.stringify(maybeArtifact.abi);
    let iface: Interface;
    try {
      iface = new Interface(maybeArtifact.abi as ReadonlyArray<Fragment | string>);
    } catch {
      continue;
    }

    const functions: AbiFunctionItem[] = [];
    for (const fragment of iface.fragments) {
      if (fragment.type !== "function") {
        continue;
      }
      const fn = fragment as FunctionFragment;
      functions.push({ signature: fn.format("sighash"), display: fn.format("full") });
    }
    if (functions.length === 0) {
      continue;
    }

    const sourcePath = vscode.workspace.asRelativePath(uri, false);
    const contractName = maybeArtifact.contractName || uri.path.split("/").pop() || "Contract";
    const sourceName = maybeArtifact.sourceName || sourcePath;

    artifacts.push({
      id: sourcePath,
      label: `${contractName} (${sourceName})`,
      sourcePath,
      abi: abiJson,
      functions
    });
  }

  artifacts.sort((a, b) => a.label.localeCompare(b.label));
  return artifacts;
}

export function encodeFunctionInput(abiJson: string, signature: string, argsJson: string): string {
  const abi = JSON.parse(abiJson) as Array<Fragment | string>;
  const iface = new Interface(abi);
  const args = parseArgs(argsJson);
  const fnName = functionNameFromSignature(signature);
  return iface.encodeFunctionData(fnName, args);
}

export function functionInputsForSignature(abiJson: string, signature: string): AbiFunctionInput[] {
  const abi = JSON.parse(abiJson) as Array<Fragment | string>;
  const iface = new Interface(abi);
  const fragment = iface.getFunction(signature);
  if (!fragment) {
    throw new Error(`Function not found in ABI: ${signature}`);
  }
  return fragment.inputs.map((input, index) => ({
    name: input.name || `arg${index}`,
    type: input.format("full")
  }));
}

function parseArgs(argsJson: string): unknown[] {
  const raw = argsJson.trim();
  if (raw.length === 0) {
    return [];
  }
  const parsed = JSON.parse(raw);
  if (!Array.isArray(parsed)) {
    throw new Error("Arguments must be a JSON array");
  }
  return parsed;
}

function functionNameFromSignature(signature: string): string {
  const open = signature.indexOf("(");
  if (open <= 0) {
    throw new Error("Invalid function signature");
  }
  return signature.slice(0, open);
}

async function readArtifactFile(uri: vscode.Uri): Promise<ArtifactLike | undefined> {
  try {
    const buffer = await vscode.workspace.fs.readFile(uri);
    const text = Buffer.from(buffer).toString("utf8");
    const json = JSON.parse(text) as ArtifactLike;
    if (!Array.isArray(json.abi)) {
      return undefined;
    }
    return json;
  } catch {
    return undefined;
  }
}
