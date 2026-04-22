import * as path from "node:path";
import { RpcClient } from "../services/rpcClient";
import { DbgserverProcessManager } from "../services/processManager";
import { InspethctLaunchConfig, SequenceStep } from "../types";

export interface GdbSessionState {
  kind: "replay" | "call";
  id: string;
  done: boolean;
  position: number;
  current?: {
    reason: string;
    breakpoint?: string;
    stepIndex: number;
    source?: {
      sourceName?: string;
      line?: number;
      column?: number;
    };
    step?: {
      pc: number;
      op: string;
      depth: number;
      gasRemaining: number;
      gasCost: number;
    };
    memory?: string;
    memorySize?: number;
    storage?: unknown[];
    transient?: unknown[];
    callAccess?: unknown;
    storageAccess?: unknown;
    memoryAccess?: unknown;
  };
  call?: {
    from: string;
    to: string;
    input: string;
    value: string;
    gas: number;
    block: string;
  };
  breakpoints?: Array<{ id: string }>;
  mutations?: Array<{ kind: string; [key: string]: unknown }>;
}

interface RuntimeSequenceState {
  index: number;
  steps: SequenceStep[];
  carryMutations: Array<Record<string, unknown>>;
}

export class InspethctRuntime {
  private rpc?: RpcClient;
  private endpoint = "";
  private sessionId = "";
  private lastState?: GdbSessionState;
  private sequence?: RuntimeSequenceState;
  private readonly managedProcessKey: string;
  private readonly sourcePathToName = new Map<string, string>();
  private readonly sourcePathToBreakpointIds = new Map<string, string[]>();

  constructor(
    private readonly processManager: DbgserverProcessManager,
    private readonly launch: InspethctLaunchConfig,
    processKey: string
  ) {
    this.managedProcessKey = processKey;
  }

  async start(): Promise<GdbSessionState> {
    this.endpoint = await this.resolveEndpoint();
    this.rpc = new RpcClient(this.endpoint);
    await this.rpc.call("dbgserver.capabilities", []);

    const sessionType = this.launch.sessionType ?? "replay";
    if (sessionType === "sequence") {
      const steps = this.launch.sequence ?? [];
      if (steps.length === 0) {
        throw new Error("sequence mode requires at least one sequence step");
      }
      this.sequence = { index: 0, steps, carryMutations: [] };
      this.lastState = await this.startCallSession(steps[0]);
    } else if (sessionType === "call") {
      if (!this.launch.call) {
        throw new Error("call mode requires call object");
      }
      this.lastState = await this.startCallSession(this.launch.call);
    } else {
      if (!this.launch.txHash) {
        throw new Error("replay mode requires txHash");
      }
      this.lastState = await this.rpc.call<GdbSessionState>("gdb.startReplaySession", [this.launch.txHash]);
      this.sessionId = this.lastState.id;
    }

    if (this.launch.sourceBundle) {
      this.lastState = await this.rpc.call<GdbSessionState>("gdb.loadSourceBundle", [this.sessionId, this.launch.sourceBundle]);
    }

    return this.lastState;
  }

  async refreshState(): Promise<GdbSessionState> {
    this.ensureRpc();
    this.lastState = await this.rpc!.call<GdbSessionState>("gdb.state", [this.sessionId]);
    return this.lastState;
  }

  async next(): Promise<GdbSessionState> {
    this.ensureRpc();
    this.lastState = await this.rpc!.call<GdbSessionState>("gdb.next", [this.sessionId]);
    await this.handleSequenceProgress();
    return this.lastState!;
  }

  async continue(): Promise<GdbSessionState> {
    this.ensureRpc();
    this.lastState = await this.rpc!.call<GdbSessionState>("gdb.continue", [this.sessionId]);
    await this.handleSequenceProgress();
    return this.lastState!;
  }

  async setSourceBreakpoints(filePath: string, lines: number[]): Promise<void> {
    this.ensureRpc();
    const sourceName = this.resolveSourceName(filePath);
    this.sourcePathToName.set(filePath, sourceName);

    const existing = this.sourcePathToBreakpointIds.get(filePath) ?? [];
    for (const id of existing) {
      await this.rpc!.call("gdb.deleteBreakpoint", [this.sessionId, { id }]);
    }

    const created: string[] = [];
    for (const line of lines) {
      const id = `bp-${sourceName}-${line}`;
      await this.rpc!.call("gdb.setSourceBreakpoint", [
        this.sessionId,
        {
          id,
          sourceName,
          line,
          column: 1
        }
      ]);
      created.push(id);
    }
    this.sourcePathToBreakpointIds.set(filePath, created);
  }

  getState(): GdbSessionState | undefined {
    return this.lastState;
  }

  resolveLocalPath(sourceName: string | undefined): string | undefined {
    if (!sourceName) {
      return undefined;
    }
    for (const [filePath, mapped] of this.sourcePathToName.entries()) {
      if (mapped === sourceName) {
        return filePath;
      }
    }
    const root = (this.launch as InspethctLaunchConfig & { workspaceRoot?: string }).workspaceRoot;
    if (!root) {
      return undefined;
    }
    return path.join(root, sourceName);
  }

  stop(): void {
    if (this.launch.autoStartDbgserver !== false) {
      this.processManager.stop(this.managedProcessKey);
    }
  }

  private async resolveEndpoint(): Promise<string> {
    if (this.launch.autoStartDbgserver === false) {
      if (!this.launch.dbgserverUrl) {
        throw new Error("attach mode requires dbgserverUrl");
      }
      return this.launch.dbgserverUrl;
    }

    const host = this.launch.listenHost ?? "127.0.0.1";
    const port = this.launch.listenPort ?? 18548;
    const binaryPath = this.launch.dbgserverBinaryPath;
    if (!binaryPath) {
      throw new Error("dbgserver binary path is required in autoStart mode");
    }
    const result = await this.processManager.start({
      key: this.managedProcessKey,
      binaryPath,
      upstream: this.launch.upstream,
      listenHost: host,
      listenPort: port,
      block: this.launch.block ?? "latest",
      fork: this.launch.fork ?? "cancun",
      forkMode: this.launch.forkMode ?? "diff"
    });
    return result.endpoint;
  }

  private async startCallSession(callConfig: SequenceStep): Promise<GdbSessionState> {
    this.ensureRpc();
    const block = callConfig.block ?? this.launch.block ?? "latest";
    const state = await this.rpc!.call<GdbSessionState>("gdb.startCallSession", [
      {
        from: callConfig.from,
        to: callConfig.to,
        input: callConfig.input,
        value: callConfig.value ?? "0x0",
        gas: callConfig.gas ?? "0x0"
      },
      block
    ]);
    this.sessionId = state.id;

    let stepped = await this.rpc!.call<GdbSessionState>("gdb.next", [this.sessionId]);
    if ((this.launch.carryUserMutations ?? true) && this.sequence && this.sequence.carryMutations.length > 0) {
      for (const mutation of this.sequence.carryMutations) {
        if (mutation.kind === "storage") {
          await this.rpc!.call("gdb.writeStorage", [
            this.sessionId,
            {
              address: mutation.address,
              scope: mutation.scope,
              slot: mutation.slot,
              value: mutation.value
            }
          ]);
        } else if (mutation.kind === "memory") {
          await this.rpc!.call("gdb.writeMemory", [
            this.sessionId,
            {
              offset: mutation.offset,
              data: mutation.data
            }
          ]);
        }
      }
      stepped = await this.rpc!.call<GdbSessionState>("gdb.state", [this.sessionId]);
    }
    return stepped;
  }

  private resolveSourceName(filePath: string): string {
    const root = (this.launch as InspethctLaunchConfig & { workspaceRoot?: string }).workspaceRoot;
    if (!root) {
      return path.basename(filePath);
    }
    const relativePath = path.relative(root, filePath);
    return relativePath.split(path.sep).join("/");
  }

  private async handleSequenceProgress(): Promise<void> {
    if (!this.sequence || !this.lastState?.done) {
      return;
    }

    if ((this.launch.carryUserMutations ?? true) && this.lastState.mutations) {
      this.sequence.carryMutations = this.lastState.mutations as Array<Record<string, unknown>>;
    }

    const nextIndex = this.sequence.index + 1;
    if (nextIndex >= this.sequence.steps.length) {
      return;
    }

    this.sequence.index = nextIndex;
    this.lastState = await this.startCallSession(this.sequence.steps[nextIndex]);

    if (this.launch.sourceBundle) {
      this.lastState = await this.rpc!.call<GdbSessionState>("gdb.loadSourceBundle", [this.sessionId, this.launch.sourceBundle]);
    }

    for (const [filePath, _] of this.sourcePathToBreakpointIds.entries()) {
      const lineIds = this.sourcePathToBreakpointIds.get(filePath) ?? [];
      const lines = lineIds
        .map((id) => Number.parseInt(id.slice(id.lastIndexOf("-") + 1), 10))
        .filter((line) => Number.isFinite(line));
      await this.setSourceBreakpoints(filePath, lines);
    }
  }

  private ensureRpc(): void {
    if (!this.rpc) {
      throw new Error("runtime is not started");
    }
  }
}
