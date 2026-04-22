import { ChildProcess, execFile, spawn } from "node:child_process";
import { promisify } from "node:util";
import { RpcClient } from "./rpcClient";

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

  async resolveBinary(configPath?: string): Promise<string | undefined> {
    if (configPath && configPath.trim() !== "") {
      return configPath;
    }
    try {
      const { stdout } = await execFileAsync("which", ["inspethctd"]);
      const resolved = stdout.trim();
      return resolved.length > 0 ? resolved : undefined;
    } catch {
      return undefined;
    }
  }

  async start(config: ManagedProcessConfig): Promise<ProcessStartResult> {
    const existing = this.managed.get(config.key);
    if (existing && !existing.killed) {
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

    this.managed.set(config.key, child);
    await this.waitUntilReady(this.endpoint(config.listenHost, config.listenPort));
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
    return `http://${host}:${port}`;
  }

  private async waitUntilReady(endpoint: string): Promise<void> {
    const startedAt = Date.now();
    let lastError: unknown;
    while (Date.now() - startedAt < 15000) {
      try {
        const rpc = new RpcClient(endpoint);
        await rpc.call("dbgserver.capabilities", []);
        return;
      } catch (error) {
        lastError = error;
        await new Promise((resolve) => setTimeout(resolve, 300));
      }
    }
    throw new Error(`dbgserver did not become ready: ${String(lastError)}`);
  }
}
