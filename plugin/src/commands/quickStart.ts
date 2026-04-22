import * as vscode from "vscode";
import { LaunchCallConfig, SourceBundleConfig, SequenceScript, InspethctLaunchConfig } from "../types";
import { composeCallFromFunction } from "../solidity/callComposer";
import { RpcClient } from "../services/rpcClient";

/** Build the auto-detect source bundle config that the dbgserver will resolve. */
export function buildAutoSourceBundle(
  folder: vscode.WorkspaceFolder | undefined,
  contractName?: string
): SourceBundleConfig | undefined {
  const settings = vscode.workspace.getConfiguration("inspethct", folder?.uri);
  const apiBase = settings.get<string>("explorerApiBase", "https://api.etherscan.io/v2/api");
  const apiKey = settings.get<string>("explorerApiKey", "");
  const chainId = settings.get<string>("explorerChainId", "1");
  return {
    kind: "auto",
    contractName: contractName || "",
    runtime: true,
    projectRoot: folder?.uri.fsPath,
    apiBase,
    apiKey,
    chainId
  };
}

export interface LaunchInputs {
  upstream: string;
  block: string;
  fork: string;
  forkMode: string;
}

export function launchDefaults(folder: vscode.WorkspaceFolder | undefined): LaunchInputs {
  const settings = vscode.workspace.getConfiguration("inspethct", folder?.uri);
  return {
    upstream: settings.get<string>("defaultUpstream", "http://127.0.0.1:8545"),
    block: settings.get<string>("defaultBlock", "latest"),
    fork: settings.get<string>("defaultFork", "cancun"),
    forkMode: settings.get<string>("defaultForkMode", "diff")
  };
}

export async function startReplayLaunch(folder: vscode.WorkspaceFolder | undefined, txHash: string): Promise<void> {
  const defaults = launchDefaults(folder);
  const launch: InspethctLaunchConfig = {
    type: "inspethct",
    request: "launch",
    name: `Inspethct Replay ${txHash.slice(0, 10)}…`,
    sessionType: "replay",
    upstream: defaults.upstream,
    block: defaults.block,
    fork: defaults.fork,
    forkMode: defaults.forkMode,
    txHash,
    autoStartDbgserver: true,
    promptSourceBundle: false,
    autoSourceBundle: true,
    sourceBundle: buildAutoSourceBundle(folder),
    carryUserMutations: true
  };
  await vscode.debug.startDebugging(folder, launch);
}

export async function startCallLaunch(
  folder: vscode.WorkspaceFolder | undefined,
  call: LaunchCallConfig,
  name: string,
  preferredContractName?: string
): Promise<void> {
  const defaults = launchDefaults(folder);
  const launch: InspethctLaunchConfig = {
    type: "inspethct",
    request: "launch",
    name,
    sessionType: "call",
    upstream: defaults.upstream,
    block: defaults.block,
    fork: defaults.fork,
    forkMode: defaults.forkMode,
    call,
    autoStartDbgserver: true,
    promptSourceBundle: false,
    autoSourceBundle: true,
    sourceBundle: buildAutoSourceBundle(folder, preferredContractName),
    carryUserMutations: true
  };
  await vscode.debug.startDebugging(folder, launch);
}

export async function startSequenceLaunch(
  folder: vscode.WorkspaceFolder | undefined,
  script: SequenceScript
): Promise<void> {
  const defaults = launchDefaults(folder);
  const launch: InspethctLaunchConfig = {
    type: "inspethct",
    request: "launch",
    name: script.name || "Inspethct Sequence",
    sessionType: "sequence",
    upstream: script.upstream || defaults.upstream,
    block: script.block || defaults.block,
    fork: script.fork || defaults.fork,
    forkMode: script.forkMode || defaults.forkMode,
    sequence: script.sequence,
    sourceBundle: script.sourceBundle || buildAutoSourceBundle(folder),
    autoStartDbgserver: true,
    promptSourceBundle: false,
    autoSourceBundle: script.sourceBundle ? false : true,
    carryUserMutations: script.carryUserMutations ?? true
  };
  await vscode.debug.startDebugging(folder, launch);
}

export async function quickStart(context: vscode.ExtensionContext): Promise<void> {
  const folder = vscode.workspace.workspaceFolders?.[0];
  const choice = await vscode.window.showQuickPick(
    [
      { label: "$(history) Replay a transaction", value: "replay", description: "Step through a chain transaction by hash" },
      { label: "$(play) Call a function", value: "call", description: "Synthesize a single call against a contract" },
      { label: "$(file-code) Open Sequence Builder", value: "builder", description: "Visually build a multi-step debug script" },
      { label: "$(file-text) Run Sequence Script", value: "script", description: "Run a saved sequence script (JSON)" }
    ],
    { title: "Inspethct: How would you like to start?", placeHolder: "Pick an entry point" }
  );
  if (!choice) {
    return;
  }
  switch (choice.value) {
    case "replay": {
      const txHash = await vscode.window.showInputBox({
        title: "Transaction hash",
        placeHolder: "0x...",
        validateInput: (value) => (value.startsWith("0x") && value.length === 66 ? undefined : "Need 66-char 0x-prefixed hash")
      });
      if (txHash) {
        await startReplayLaunch(folder, txHash);
      }
      return;
    }
    case "call": {
      const signature = await vscode.window.showInputBox({
        title: "Function signature",
        placeHolder: "transfer(address,uint256)"
      });
      if (!signature) {
        return;
      }
      const call = await composeCallFromFunction(signature);
      if (!call) {
        return;
      }
      await startCallLaunch(folder, call, `Inspethct ${signature}`);
      return;
    }
    case "builder": {
      await vscode.commands.executeCommand("inspethct.openSequenceBuilder");
      return;
    }
    case "script": {
      await vscode.commands.executeCommand("inspethct.runSequenceScript");
      return;
    }
  }
}

export async function importReplayFromChain(folder: vscode.WorkspaceFolder | undefined): Promise<void> {
  const defaults = launchDefaults(folder);
  const upstream =
    (await vscode.window.showInputBox({
      title: "Upstream RPC URL",
      value: defaults.upstream
    })) || "";
  if (!upstream) {
    return;
  }
  const txHash = await vscode.window.showInputBox({
    title: "Transaction hash",
    placeHolder: "0x...",
    validateInput: (value) => (value.startsWith("0x") && value.length === 66 ? undefined : "Need 66-char 0x-prefixed hash")
  });
  if (!txHash) {
    return;
  }
  try {
    const client = new RpcClient(upstream);
    const result = await client.call<unknown>("eth_getTransactionByHash", [txHash]);
    if (!result) {
      void vscode.window.showErrorMessage("Transaction not found on upstream RPC.");
      return;
    }
    await startReplayLaunch(folder, txHash);
  } catch (error) {
    void vscode.window.showErrorMessage(`Import failed: ${String(error)}`);
  }
}
