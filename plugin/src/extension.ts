import * as vscode from "vscode";
import { DebugAdapterInlineImplementation } from "vscode";
import { InspethctDebugSession } from "./dap/session";
import { DbgserverProcessManager } from "./services/processManager";
import { SolidityFunctionCodeLensProvider } from "./solidity/codelensProvider";
import { composeCallFromFunction } from "./solidity/callComposer";
import { collectSolidityFunctions } from "./solidity/functionIndex";
import { InspethctLaunchConfig, SequenceScript } from "./types";
import { SequenceBuilderPanel } from "./ui/sequenceBuilderPanel";
import {
  buildAutoSourceBundle,
  importReplayFromChain,
  launchDefaults,
  quickStart,
  startCallLaunch,
  startSequenceLaunch
} from "./commands/quickStart";

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
    const defaults = launchDefaults(folder);
    config.type = "inspethct";
    config.request = "launch";
    config.name = config.name || "Inspethct Debug";
    config.upstream = config.upstream || defaults.upstream;
    config.listenHost = config.listenHost || "127.0.0.1";
    config.listenPort = config.listenPort || 18548;
    config.block = config.block || defaults.block;
    config.fork = config.fork || defaults.fork;
    config.forkMode = config.forkMode || defaults.forkMode;
    config.autoStartDbgserver = config.autoStartDbgserver ?? true;
    config.sessionType = config.sessionType || "replay";
    config.workspaceRoot = folder?.uri.fsPath;

    if (config.autoStartDbgserver) {
      const settings = vscode.workspace.getConfiguration("inspethct", folder?.uri);
      const configuredBinary = config.dbgserverBinaryPath || settings.get<string>("binaryPath");
      const resolvedBinary = await this.processManager.resolveBinary(configuredBinary);
      if (!resolvedBinary) {
        const action = await vscode.window.showErrorMessage(
          "Cannot find inspethctd. Install it (go install ./cmd/inspethctd) or set inspethct.binaryPath.",
          "Set Path…",
          "Open Settings"
        );
        if (action === "Set Path…") {
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
        } else if (action === "Open Settings") {
          await vscode.commands.executeCommand("workbench.action.openSettings", "inspethct.binaryPath");
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

    if (config.sessionType === "replay" && !config.txHash) {
      const txHash = await vscode.window.showInputBox({
        title: "Replay transaction hash",
        placeHolder: "0x...",
        validateInput: (value) => (value.startsWith("0x") && value.length === 66 ? undefined : "Need 66-char 0x-prefixed hash")
      });
      if (!txHash) {
        return undefined;
      }
      config.txHash = txHash;
    }

    config.autoSourceBundle = config.autoSourceBundle ?? true;
    if (!config.sourceBundle && config.autoSourceBundle) {
      config.sourceBundle = buildAutoSourceBundle(folder);
    }

    return config;
  }
}

export function activate(context: vscode.ExtensionContext): void {
  const output = vscode.window.createOutputChannel("Inspethct");
  const processManager = new DbgserverProcessManager((line) => output.appendLine(line));
  const configProvider = new InspethctConfigurationProvider(processManager);
  const adapterFactory = new InspethctDebugAdapterFactory(processManager);
  const codeLensProvider = new SolidityFunctionCodeLensProvider();

  context.subscriptions.push(
    output,
    vscode.debug.registerDebugConfigurationProvider("inspethct", configProvider),
    vscode.debug.registerDebugAdapterDescriptorFactory("inspethct", adapterFactory),
    vscode.languages.registerCodeLensProvider({ language: "solidity", scheme: "file" }, codeLensProvider),
    vscode.commands.registerCommand("inspethct.start", () => quickStart(context)),
    vscode.commands.registerCommand("inspethct.debugFunction", async (uri?: vscode.Uri, signature?: string) => {
      await runDebugFunctionCommand(uri, signature);
    }),
    vscode.commands.registerCommand("inspethct.debugAtCursor", async () => {
      await runDebugAtCursorCommand();
    }),
    vscode.commands.registerCommand("inspethct.debugContract", async (uri?: vscode.Uri) => {
      await runDebugContractCommand(uri);
    }),
    vscode.commands.registerCommand("inspethct.importReplayFromChain", async () => {
      const folder = vscode.workspace.workspaceFolders?.[0];
      await importReplayFromChain(folder);
    }),
    vscode.commands.registerCommand("inspethct.runSequenceScript", async (uri?: vscode.Uri) => {
      await runSequenceScriptCommand(uri);
    }),
    vscode.commands.registerCommand("inspethct.openSequenceBuilder", async () => {
      await runOpenSequenceBuilderCommand(context);
    }),
    vscode.commands.registerCommand("inspethct.openRepl", async () => {
      await runReplCommand(context);
    }),
    {
      dispose: () => processManager.stopAll()
    }
  );
}

export function deactivate(): void {
  // Process cleanup runs through the registered subscription.
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

  const folder = vscode.workspace.getWorkspaceFolder(targetUri) || vscode.workspace.workspaceFolders?.[0];
  const contractName = guessContractNameFromFile(document);
  await startCallLaunch(folder, call, `Inspethct ${pickedSignature}`, contractName);
}

async function runDebugAtCursorCommand(): Promise<void> {
  const editor = vscode.window.activeTextEditor;
  if (!editor || editor.document.languageId !== "solidity") {
    void vscode.window.showErrorMessage("Open a Solidity file and place the cursor inside a function.");
    return;
  }
  const cursorLine = editor.selection.active.line;
  const functions = collectSolidityFunctions(editor.document);
  let nearest: { line: number; signature: string } | undefined;
  for (const fn of functions) {
    if (fn.line <= cursorLine && (!nearest || fn.line > nearest.line)) {
      nearest = fn;
    }
  }
  if (!nearest) {
    void vscode.window.showErrorMessage("No function found at cursor.");
    return;
  }
  await runDebugFunctionCommand(editor.document.uri, nearest.signature);
}

async function runDebugContractCommand(uri?: vscode.Uri): Promise<void> {
  const editor = uri ? undefined : vscode.window.activeTextEditor;
  const targetUri = uri || editor?.document.uri;
  if (!targetUri) {
    return;
  }
  const document = await vscode.workspace.openTextDocument(targetUri);
  if (document.languageId !== "solidity") {
    void vscode.window.showErrorMessage("Open a Solidity file to debug.");
    return;
  }
  const functions = collectSolidityFunctions(document);
  if (functions.length === 0) {
    void vscode.window.showErrorMessage("No functions found in this file.");
    return;
  }
  const picked = await vscode.window.showQuickPick(
    functions.map((fn) => ({ label: fn.signature, description: `line ${fn.line + 1}`, signature: fn.signature })),
    { title: "Pick a function to debug" }
  );
  if (!picked) {
    return;
  }
  await runDebugFunctionCommand(targetUri, picked.signature);
}

function guessContractNameFromFile(document: vscode.TextDocument): string | undefined {
  const match = /\b(?:contract|library)\s+([A-Za-z_][A-Za-z0-9_]*)/.exec(document.getText());
  return match?.[1];
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
  await startSequenceLaunch(folder, script);
}

async function runOpenSequenceBuilderCommand(context: vscode.ExtensionContext): Promise<void> {
  const folder = vscode.workspace.workspaceFolders?.[0];
  const defaults = launchDefaults(folder);
  SequenceBuilderPanel.open(context, defaults, async (config) => {
    await vscode.debug.startDebugging(folder, config);
  });
}

let replOutput: vscode.OutputChannel | undefined;

async function runReplCommand(_context: vscode.ExtensionContext): Promise<void> {
  const session = vscode.debug.activeDebugSession;
  if (!session || session.type !== "inspethct") {
    void vscode.window.showWarningMessage("Inspethct REPL requires an active Inspethct debug session.");
    return;
  }
  if (!replOutput) {
    replOutput = vscode.window.createOutputChannel("Inspethct REPL");
  }
  replOutput.show(true);
  for (;;) {
    const command = await vscode.window.showInputBox({
      prompt: "inspethct>",
      placeHolder: "help, c, n, b line ...:N, info locals, x 0x80 0x40, ...",
      ignoreFocusOut: true
    });
    if (command === undefined || command.length === 0) {
      return;
    }
    replOutput.appendLine(`> ${command}`);
    try {
      const reply = await session.customRequest("evaluate", { expression: command, context: "repl" });
      const text = reply && typeof reply.result === "string" ? reply.result : JSON.stringify(reply);
      if (text && text.length > 0) {
        replOutput.appendLine(text);
      }
    } catch (error) {
      replOutput.appendLine(`error: ${String(error)}`);
    }
  }
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
