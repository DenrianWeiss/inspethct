import { ChildProcess, execFile, spawn } from "node:child_process";
import { promisify } from "node:util";
import * as fs from "node:fs";
import * as path from "node:path";
import * as os from "node:os";
import * as vscode from "vscode";
import { RpcClient } from "./rpcClient";
import { tryDownloadLatestBinary } from "./githubRelease";

const execFileAsync = promisify(execFile);

export interface ManagedProcessConfig {
  key: string;
  binaryPath: string;
  upstream: string;
  listenHost: string;
  listenPort: number;
  block: string;
  fork: string;
  forkMode: string;
}

export interface ProcessStartResult {
  endpoint: string;
  managed: boolean;
}

export class DbgserverProcessManager {
  private readonly managed = new Map<string, ChildProcess>();
  private readonly recentStderr = new Map<string, string[]>();
  private readonly recentStdout = new Map<string, string[]>();

  constructor(
    private readonly context: vscode.ExtensionContext,
    private readonly debugLog?: (line: string) => void
  ) {}

  async resolveBinary(configPath?: string): Promise<string | undefined> {
    if (configPath && configPath.trim() !== "") {
      return configPath;
    }
    try {
      const { stdout } = await execFileAsync("which", ["inspethctd"]);
      const resolved = stdout.trim();
      if (resolved.length > 0) {
        return resolved;
      }
    } catch {
      // not found in PATH
    }
    // Try cached download
    const cached = this.cachedBinaryPath();
    if (cached && fs.existsSync(cached)) {
      return cached;
    }
    return undefined;
  }

  async resolveOrDownloadBinary(configPath?: string): Promise<string | undefined> {
    const resolved = await this.resolveBinary(configPath);
    if (resolved) {
      return resolved;
    }
    return tryDownloadLatestBinary(this.context);
  }

  private cachedBinaryPath(): string | undefined {
    const assetName = this.platformAssetName();
    if (!assetName) {
      return undefined;
    }
    return path.join(this.context.globalStorageUri.fsPath, "bin", assetName);
  }

  private platformAssetName(): string | undefined {
    const platform = os.platform();
    const arch = os.arch();
    if (platform === "darwin" && arch === "arm64") {
      return "inspethctd-darwin-arm64";
    }
    if (platform === "darwin" && arch === "x64") {
      return "inspethctd-darwin-amd64";
    }
    if (platform === "linux" && arch === "arm64") {
      return "inspethctd-linux-arm64";
    }
    if (platform === "linux" && arch === "x64") {
      return "inspethctd-linux-amd64";
    }
    if (platform === "win32" && arch === "x64") {
      return "inspethctd-windows-amd64.exe";
    }
    return undefined;
  }

  async start(config: ManagedProcessConfig): Promise<ProcessStartResult> {
    const existing = this.managed.get(config.key);
    if (existing && !existing.killed) {
      this.log(`[process] reusing managed dbgserver key=${config.key}`);
      return { endpoint: this.endpoint(config.listenHost, config.listenPort), managed: true };
    }

    const args = [
      "dbgserver",
      "-upstream",
      config.upstream,
      "-listen",
      `${config.listenHost}:${config.listenPort}`,
      "-block",
      config.block,
      "-fork",
      config.fork,
      "-mode",
      config.forkMode
    ];

    const child = spawn(config.binaryPath, args, {
      stdio: "pipe",
      env: process.env
    });

    this.log(`[process] spawn ${config.binaryPath} ${args.join(" ")}`);
    this.recentStderr.set(config.key, []);
    this.recentStdout.set(config.key, []);
    child.stdout?.on("data", (chunk) => {
      this.pushLine(this.recentStdout.get(config.key), String(chunk));
      this.log(`[dbgserver stdout] ${String(chunk).trimEnd()}`);
    });
    child.stderr?.on("data", (chunk) => {
      this.pushLine(this.recentStderr.get(config.key), String(chunk));
      this.log(`[dbgserver stderr] ${String(chunk).trimEnd()}`);
    });
    child.on("exit", (code, signal) => {
      this.log(`[process] dbgserver exited key=${config.key} code=${String(code)} signal=${String(signal)}`);
    });
    child.on("error", (error) => {
      this.log(`[process] dbgserver process error key=${config.key}: ${this.describeError(error)}`);
    });

    this.managed.set(config.key, child);
    await this.waitUntilReady(config.key, this.endpoint(config.listenHost, config.listenPort));
    return { endpoint: this.endpoint(config.listenHost, config.listenPort), managed: true };
  }

  stop(key: string): void {
    const child = this.managed.get(key);
    if (!child) {
      return;
    }
    this.managed.delete(key);
    if (!child.killed) {
      child.kill("SIGTERM");
    }
  }

  stopAll(): void {
    for (const key of this.managed.keys()) {
      this.stop(key);
    }
  }

  private endpoint(host: string, port: number): string {
    const clientHost = host === "0.0.0.0" || host === "::" ? "127.0.0.1" : host;
    const formattedHost = clientHost.includes(":") && !clientHost.startsWith("[") ? `[${clientHost}]` : clientHost;
    return `http://${formattedHost}:${port}`;
  }

  private async waitUntilReady(key: string, endpoint: string): Promise<void> {
    const startedAt = Date.now();
    let lastError: unknown;
    let attempts = 0;
    while (Date.now() - startedAt < 15000) {
      attempts += 1;
      const managed = this.managed.get(key);
      if (!managed || managed.exitCode !== null || managed.killed) {
        const stderr = (this.recentStderr.get(key) || []).join("\n");
        throw new Error(
          `dbgserver exited before ready (attempts=${attempts}, exitCode=${String(managed?.exitCode)}, killed=${String(
            managed?.killed
          )})${stderr ? `\nRecent stderr:\n${stderr}` : ""}`
        );
      }
      try {
        const rpc = new RpcClient(endpoint);
        await rpc.call("dbgserver.capabilities", []);
        this.log(`[ready] dbgserver ready after ${attempts} probe(s) at ${endpoint}`);
        return;
      } catch (error) {
        lastError = error;
        this.log(`[ready] probe ${attempts} failed: ${this.describeError(error)}`);
        await new Promise((resolve) => setTimeout(resolve, 300));
      }
    }
    const stderr = (this.recentStderr.get(key) || []).join("\n");
    throw new Error(
      `dbgserver did not become ready after ${attempts} probe(s): ${this.describeError(lastError)}${
        stderr ? `\nRecent stderr:\n${stderr}` : ""
      }`
    );
  }

  private log(line: string): void {
    this.debugLog?.(line);
  }

  private describeError(error: unknown): string {
    if (error instanceof Error) {
      const withCause = error as Error & { cause?: unknown };
      if (withCause.cause) {
        return `${error.message}; cause=${this.describeError(withCause.cause)}`;
      }
      return error.message;
    }
    return String(error);
  }

  private pushLine(buffer: string[] | undefined, rawChunk: string): void {
    if (!buffer) {
      return;
    }
    for (const line of rawChunk.split(/\r?\n/)) {
      if (!line.trim()) {
        continue;
      }
      buffer.push(line);
      if (buffer.length > 30) {
        buffer.shift();
      }
    }
  }
}
