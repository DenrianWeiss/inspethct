import * as vscode from "vscode";
import { DebugAdapterInlineImplementation } from "vscode";
import { InspethctDebugSession } from "./dap/session";
import { DbgserverProcessManager } from "./services/processManager";
import { RpcClient } from "./services/rpcClient";
import { SolidityFunctionCodeLensProvider } from "./solidity/codelensProvider";
import { composeCallFromFunction } from "./solidity/callComposer";
import { InspethctLaunchConfig, SequenceScript, SourceBundleConfig } from "./types";
import { SequenceBuilderPanel } from "./ui/sequenceBuilderPanel";

class InspethctDebugAdapterFactory implements vscode.DebugAdapterDescriptorFactory {
  constructor(private readonly processManager: DbgserverProcessManager) {}

  createDebugAdapterDescriptor(): vscode.ProviderResult<vscode.DebugAdapterDescriptor> {
    return new DebugAdapterInlineImplementation(new InspethctDebugSession(this.processManager));
  }
}

class InspethctConfigurationProvider implements vscode.DebugConfigurationProvider {
  constructor(private readonly processManager: DbgserverProcessManager) {}

  async resolveDebugConfiguration(
    folder: vscode.WorkspaceFolder | undefined,
    config: InspethctLaunchConfig & { workspaceRoot?: string }
  ): Promise<vscode.DebugConfiguration | null | undefined> {
    const settings = vscode.workspace.getConfiguration("inspethct", folder?.uri);
    const upstreamDefault = settings.get<string>("defaultUpstream", "http://127.0.0.1:8545");

    config.type = "inspethct";
    config.request = "launch";
    config.name = config.name || "Inspethct Debug";
    config.upstream = config.upstream || upstreamDefault;
    config.listenHost = config.listenHost || "127.0.0.1";
    config.listenPort = config.listenPort || 18548;
    config.block = config.block || settings.get<string>("defaultBlock", "latest");
    config.fork = config.fork || settings.get<string>("defaultFork", "cancun");
    config.forkMode = config.forkMode || settings.get<string>("defaultForkMode", "diff");
    config.autoStartDbgserver = config.autoStartDbgserver ?? true;
    config.sessionType = config.sessionType || "replay";
    config.workspaceRoot = folder?.uri.fsPath;

    if (config.autoStartDbgserver) {
      const configuredBinary = config.dbgserverBinaryPath || settings.get<string>("binaryPath");
      const resolvedBinary = await this.processManager.resolveBinary(configuredBinary);
      if (!resolvedBinary) {
        const action = await vscode.window.showErrorMessage(
          "Cannot find inspethctd in PATH. Please install it or set inspethct.binaryPath.",
          "Set Binary Path",
          "Download Guide"
        );
        if (action === "Set Binary Path") {
          const pickedPath = await vscode.window.showInputBox({
            title: "Path to inspethctd",
            placeHolder: "/usr/local/bin/inspethctd"
          });
          if (pickedPath) {
            await settings.update("binaryPath", pickedPath, vscode.ConfigurationTarget.Workspace);
            config.dbgserverBinaryPath = pickedPath;
          } else {
            return undefined;
          }
        } else if (action === "Download Guide") {
          void vscode.env.openExternal(vscode.Uri.parse("https://github.com/your-org/inspethct"));
          return undefined;
        } else {
          return undefined;
        }
      } else {
        config.dbgserverBinaryPath = resolvedBinary;
      }
    } else if (!config.dbgserverUrl) {
      vscode.window.showErrorMessage("Attach mode requires dbgserverUrl.");
      return undefined;
    }

    if (config.promptSourceBundle ?? true) {
      const sourceBundle = await promptSourceBundle(folder, config.upstream);
      if (sourceBundle) {
        config.sourceBundle = sourceBundle;
      }
    }

    if (config.sessionType === "replay" && !config.txHash) {
      const txHash = await vscode.window.showInputBox({
        title: "Replay transaction hash",
        placeHolder: "0x...",
        validateInput: (value) => (value.startsWith("0x") ? undefined : "Transaction hash must start with 0x")
      });
      if (!txHash) {
        return undefined;
      }
      config.txHash = txHash;
    }

    return config;
  }
}

export function activate(context: vscode.ExtensionContext): void {
  const processManager = new DbgserverProcessManager();
  const configProvider = new InspethctConfigurationProvider(processManager);
  const adapterFactory = new InspethctDebugAdapterFactory(processManager);
  const codeLensProvider = new SolidityFunctionCodeLensProvider();

  context.subscriptions.push(
    vscode.debug.registerDebugConfigurationProvider("inspethct", configProvider),
    vscode.debug.registerDebugAdapterDescriptorFactory("inspethct", adapterFactory),
    vscode.languages.registerCodeLensProvider({ language: "solidity", scheme: "file" }, codeLensProvider),
    vscode.commands.registerCommand("inspethct.debugFunction", async (uri?: vscode.Uri, signature?: string) => {
      await runDebugFunctionCommand(uri, signature);
    }),
    vscode.commands.registerCommand("inspethct.importReplayFromChain", async () => {
      await runImportReplayCommand();
    }),
    vscode.commands.registerCommand("inspethct.runSequenceScript", async (uri?: vscode.Uri) => {
      await runSequenceScriptCommand(uri);
    }),
    vscode.commands.registerCommand("inspethct.openSequenceBuilder", async () => {
      await runOpenSequenceBuilderCommand(context);
    }),
    {
      dispose: () => processManager.stopAll()
    }
  );
}

export function deactivate(): void {
  // no-op, process cleanup is handled by subscription dispose
}

async function runDebugFunctionCommand(uri?: vscode.Uri, signature?: string): Promise<void> {
  const editor = uri ? undefined : vscode.window.activeTextEditor;
  const targetUri = uri || editor?.document.uri;
  if (!targetUri) {
    return;
  }
  const document = await vscode.workspace.openTextDocument(targetUri);
  if (document.languageId !== "solidity") {
    void vscode.window.showErrorMessage("Function entry debug currently supports Solidity files only.");
    return;
  }

  let pickedSignature = signature;
  if (!pickedSignature) {
    const value = await vscode.window.showInputBox({
      title: "Function signature",
      placeHolder: "transfer(address,uint256)"
    });
    if (!value) {
      return;
    }
    pickedSignature = value;
  }

  const call = await composeCallFromFunction(pickedSignature);
  if (!call) {
    return;
  }

  const folder = vscode.workspace.getWorkspaceFolder(targetUri);
  const settings = vscode.workspace.getConfiguration("inspethct", folder?.uri);
  const upstream = settings.get<string>("defaultUpstream", "http://127.0.0.1:8545");

  const config: InspethctLaunchConfig = {
    type: "inspethct",
    request: "launch",
    name: `Inspethct ${pickedSignature}`,
    sessionType: "call",
    upstream,
    call,
    autoStartDbgserver: true,
    promptSourceBundle: true,
    carryUserMutations: true
  };

  await vscode.debug.startDebugging(folder, config);
}

async function runImportReplayCommand(): Promise<void> {
  const folder = vscode.workspace.workspaceFolders?.[0];
  const settings = vscode.workspace.getConfiguration("inspethct", folder?.uri);
  const upstream =
    (await vscode.window.showInputBox({
      title: "Upstream RPC URL",
      value: settings.get<string>("defaultUpstream", "http://127.0.0.1:8545")
    })) || "";
  if (!upstream) {
    return;
  }

  const txHash = await vscode.window.showInputBox({
    title: "Transaction hash",
    placeHolder: "0x...",
    validateInput: (value) => (value.startsWith("0x") ? undefined : "Transaction hash must start with 0x")
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

    const config: InspethctLaunchConfig = {
      type: "inspethct",
      request: "launch",
      name: `Inspethct Replay ${txHash.slice(0, 10)}...`,
      sessionType: "replay",
      upstream,
      txHash,
      autoStartDbgserver: true,
      promptSourceBundle: true,
      carryUserMutations: true
    };

    await vscode.debug.startDebugging(folder, config);
  } catch (error) {
    void vscode.window.showErrorMessage(`Import failed: ${String(error)}`);
  }
}

async function runSequenceScriptCommand(uri?: vscode.Uri): Promise<void> {
  const selectedUri = uri || (await pickSequenceScriptUri());
  if (!selectedUri) {
    return;
  }

  let script: SequenceScript;
  try {
    const buffer = await vscode.workspace.fs.readFile(selectedUri);
    script = JSON.parse(Buffer.from(buffer).toString("utf8")) as SequenceScript;
  } catch (error) {
    void vscode.window.showErrorMessage(`Invalid sequence script: ${String(error)}`);
    return;
  }

  if (!Array.isArray(script.sequence) || script.sequence.length === 0) {
    void vscode.window.showErrorMessage("Sequence script must contain non-empty sequence array.");
    return;
  }

  const folder = vscode.workspace.getWorkspaceFolder(selectedUri) || vscode.workspace.workspaceFolders?.[0];
  const settings = vscode.workspace.getConfiguration("inspethct", folder?.uri);
  const upstream = script.upstream || settings.get<string>("defaultUpstream", "http://127.0.0.1:8545");

  const config: InspethctLaunchConfig = {
    type: "inspethct",
    request: "launch",
    name: script.name || `Inspethct Sequence ${selectedUri.path.split("/").pop() || "script"}`,
    sessionType: "sequence",
    upstream,
    block: script.block,
    fork: script.fork,
    forkMode: script.forkMode,
    sequence: script.sequence,
    sourceBundle: script.sourceBundle,
    autoStartDbgserver: true,
    promptSourceBundle: script.promptSourceBundle ?? false,
    carryUserMutations: script.carryUserMutations ?? true
  };

  await vscode.debug.startDebugging(folder, config);
}

async function runOpenSequenceBuilderCommand(context: vscode.ExtensionContext): Promise<void> {
  const folder = vscode.workspace.workspaceFolders?.[0];
  const settings = vscode.workspace.getConfiguration("inspethct", folder?.uri);
  SequenceBuilderPanel.open(
    context,
    {
      upstream: settings.get<string>("defaultUpstream", "http://127.0.0.1:8545"),
      block: settings.get<string>("defaultBlock", "latest"),
      fork: settings.get<string>("defaultFork", "cancun"),
      forkMode: settings.get<string>("defaultForkMode", "diff")
    },
    async (config) => {
      await vscode.debug.startDebugging(folder, config);
    }
  );
}

async function pickSequenceScriptUri(): Promise<vscode.Uri | undefined> {
  const picked = await vscode.window.showOpenDialog({
    canSelectFiles: true,
    canSelectFolders: false,
    filters: { JSON: ["json"] },
    title: "Select Inspethct sequence script"
  });
  if (!picked || picked.length === 0) {
    return undefined;
  }
  return picked[0];
}

async function promptSourceBundle(
  folder: vscode.WorkspaceFolder | undefined,
  upstream: string
): Promise<SourceBundleConfig | undefined> {
  const picked = await vscode.window.showQuickPick(
    [
      { label: "Skip source bundle", value: "skip" },
      { label: "Local project", value: "local-project" },
      { label: "Standard JSON", value: "standard-json" },
      { label: "Manual ABI", value: "manual" },
      { label: "Explorer API", value: "explorer" }
    ],
    { title: "Select contract source" }
  );

  if (!picked || picked.value === "skip") {
    return undefined;
  }

  const contractName = await vscode.window.showInputBox({
    title: "Contract name",
    placeHolder: "MyContract"
  });
  if (!contractName) {
    return undefined;
  }

  const sourceName = await vscode.window.showInputBox({
    title: "Source name (optional)",
    placeHolder: "src/MyContract.sol"
  });

  const runtimePick = await vscode.window.showQuickPick(
    [
      { label: "Runtime bytecode", value: true },
      { label: "Creation bytecode", value: false }
    ],
    { title: "Bundle bytecode type" }
  );

  if (!runtimePick) {
    return undefined;
  }

  const base: SourceBundleConfig = {
    kind: picked.value as SourceBundleConfig["kind"],
    contractName,
    sourceName: sourceName || undefined,
    runtime: runtimePick.value
  };

  if (picked.value === "local-project") {
    base.projectRoot =
      (await vscode.window.showInputBox({
        title: "Project root",
        value: folder?.uri.fsPath
      })) || folder?.uri.fsPath;
    return base;
  }

  if (picked.value === "standard-json") {
    const pickedFile = await vscode.window.showOpenDialog({
      canSelectFiles: true,
      canSelectFolders: false,
      title: "Select standard json file"
    });
    if (!pickedFile || pickedFile.length === 0) {
      return undefined;
    }
    base.standardJsonPath = pickedFile[0].fsPath;
    return base;
  }

  if (picked.value === "manual") {
    const jsonFile = await vscode.window.showOpenDialog({
      canSelectFiles: true,
      canSelectFolders: false,
      title: "Select source map or bytecode metadata file"
    });
    const abiFile = await vscode.window.showOpenDialog({
      canSelectFiles: true,
      canSelectFolders: false,
      title: "Select ABI json file"
    });
    if (!abiFile || abiFile.length === 0) {
      return undefined;
    }
    base.standardJsonPath = jsonFile?.[0]?.fsPath;
    base.abiPath = abiFile[0].fsPath;
    return base;
  }

  if (picked.value === "explorer") {
    const address = await vscode.window.showInputBox({
      title: "Contract address (optional)",
      placeHolder: "0x..."
    });
    const apiBase = await vscode.window.showInputBox({
      title: "Explorer API base",
      value: "https://api.etherscan.io/v2/api"
    });
    const apiKey = await vscode.window.showInputBox({ title: "Explorer API key", placeHolder: "Optional" });

    base.address = address || undefined;
    base.apiBase = apiBase || undefined;
    base.apiKey = apiKey || undefined;
    base.rpcUrl = upstream;
    return base;
  }

  return undefined;
}
