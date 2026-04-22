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
    const functions = collectSolidityFunctions(document);
    return functions.map((entry) => {
      const range = new vscode.Range(new vscode.Position(entry.line, 0), new vscode.Position(entry.line, 0));
      return new vscode.CodeLens(range, {
        title: "Debug with Inspethct",
        command: "inspethct.debugFunction",
        arguments: [document.uri, entry.signature]
      });
    });
  }
}
