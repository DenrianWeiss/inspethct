import * as vscode from "vscode";
import { collectSolidityFunctions } from "./functionIndex";

export class SolidityFunctionCodeLensProvider implements vscode.CodeLensProvider {
  private readonly onDidChangeEmitter = new vscode.EventEmitter<void>();

  readonly onDidChangeCodeLenses = this.onDidChangeEmitter.event;

  refresh(): void {
    this.onDidChangeEmitter.fire();
  }

  provideCodeLenses(document: vscode.TextDocument): vscode.ProviderResult<vscode.CodeLens[]> {
    if (document.languageId !== "solidity") {
      return [];
    }
    const lenses: vscode.CodeLens[] = [];
    const text = document.getText();
    const contractMatch = /\b(?:contract|library)\s+[A-Za-z_][A-Za-z0-9_]*/.exec(text);
    if (contractMatch) {
      const startLine = document.positionAt(contractMatch.index).line;
      const range = new vscode.Range(new vscode.Position(startLine, 0), new vscode.Position(startLine, 0));
      lenses.push(
        new vscode.CodeLens(range, {
          title: "$(debug-start) Debug Contract…",
          command: "inspethct.debugContract",
          arguments: [document.uri]
        })
      );
      lenses.push(
        new vscode.CodeLens(range, {
          title: "$(history) Replay TX",
          command: "inspethct.importReplayFromChain"
        })
      );
    }
    const functions = collectSolidityFunctions(document);
    for (const entry of functions) {
      const range = new vscode.Range(new vscode.Position(entry.line, 0), new vscode.Position(entry.line, 0));
      lenses.push(
        new vscode.CodeLens(range, {
          title: "$(debug-alt) Debug",
          command: "inspethct.debugFunction",
          arguments: [document.uri, entry.signature]
        })
      );
    }
    return lenses;
  }
}
