import * as path from "node:path";
import { RpcClient } from "../services/rpcClient";
import { DbgserverProcessManager } from "../services/processManager";
import { InspethctLaunchConfig, SequenceStep, SourceBundleConfig } from "../types";

interface DbgserverCapabilities {
  mode?: string;
  methods?: string[];
  features?: {
    statePatch?: boolean;
    sequenceSession?: boolean;
    tupleAbiAssist?: boolean;
  };
}

export interface RuntimeNotifier {
  info(message: string): void;
  warn(message: string): void;
}

export interface MemoryRegionInfo {
  kind: string;
  label: string;
  offset: number;
  length: number;
}

export interface StorageVariable {
  scope?: string;
  name: string;
  slot: string;
  value: string;
  type?: string;
}

export interface LocalVariable {
  name: string;
  type: string;
  storageLocation?: string;
  kind: string; // "parameter" | "return" | "local"
  declaredAtLine?: number;
  value?: string;
  confidence?: string;
  stackIndex?: number;
  memoryPointer?: number;
  note?: string;
}

export interface CallFrameInfo {
  depth: number;
  contractAddress: string;
  codeAddress: string;
  callType?: string;
  selector?: string;
  inputSize: number;
}

export interface GdbSessionState {
  kind: "replay" | "call";
  id: string;
  done: boolean;
  position: number;
  current?: {
    reason: string;
    breakpoint?: string;
    stepIndex: number;
    codeAddress?: string;
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
    memoryTruncated?: boolean;
    memoryRegions?: MemoryRegionInfo[];
    freeMemoryPointer?: number;
    stack?: string[];
    locals?: LocalVariable[];
    storage?: StorageVariable[];
    transient?: StorageVariable[];
    callAccess?: unknown;
    storageAccess?: unknown;
    memoryAccess?: unknown;
    callStack?: CallFrameInfo[];
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
  bundles?: Array<{ codeAddress: string; sourceName?: string; contractName?: string }>;
  bundleSource?: string;
  bundleDiagnostics?: string[];
}

interface RuntimeSequenceState {
  index: number;
  steps: SequenceStep[];
  carryMutations: Array<Record<string, unknown>>;
  usingNativeSequence: boolean;
  sequenceId?: string;
}

interface NativeSequenceResponse {
  sequenceId: string;
  requestedStateCarry?: string;
  stateCarry?: string;
  currentStepIndex: number;
  done: boolean;
  activeSessionId?: string;
  activeSession?: GdbSessionState;
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
  private capabilities?: DbgserverCapabilities;
  private readonly autoLoadedAddresses = new Set<string>();

  constructor(
    private readonly processManager: DbgserverProcessManager,
    private readonly launch: InspethctLaunchConfig,
    processKey: string,
    private readonly notifier?: RuntimeNotifier
  ) {
    this.managedProcessKey = processKey;
  }

  async start(): Promise<GdbSessionState> {
    this.endpoint = await this.resolveEndpoint();
    this.rpc = new RpcClient(this.endpoint);
    this.capabilities = await this.rpc.call<DbgserverCapabilities>("dbgserver.capabilities", []);

    const sessionType = this.launch.sessionType ?? "replay";
    if (sessionType === "sequence") {
      const steps = this.launch.sequence ?? [];
      if (steps.length === 0) {
        throw new Error("sequence mode requires at least one sequence step");
      }
      const useNativeSequence = this.capabilities?.features?.sequenceSession === true;
      this.sequence = { index: 0, steps, carryMutations: [], usingNativeSequence: useNativeSequence };
      if (useNativeSequence) {
        this.lastState = await this.startNativeSequenceSession(steps);
      } else {
        this.lastState = await this.startCallSession(steps[0]);
      }
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

    await this.configureAutoMatch();

    if (this.launch.sourceBundle) {
      await this.loadSourceBundle(this.launch.sourceBundle, "initial");
    }

    await this.maybeAutoLoadForCurrent();

    return this.lastState;
  }

  async refreshState(): Promise<GdbSessionState> {
    this.ensureRpc();
    this.lastState = await this.rpc!.call<GdbSessionState>("gdb.state", [this.sessionId]);
    return this.lastState;
  }

  /** Generic JSON-RPC passthrough used by the REPL command surface. */
  async rawCall<T = unknown>(method: string, params: unknown[]): Promise<T> {
    this.ensureRpc();
    return this.rpc!.call<T>(method, params);
  }

  /** Returns the current session id (empty string if not started). */
  get currentSessionId(): string {
    return this.sessionId;
  }

  async readMemory(offset: number, length: number): Promise<{ data: string; offset: number; length: number; memorySize: number; truncated: boolean }> {
    this.ensureRpc();
    return this.rpc!.call("gdb.readMemory", [this.sessionId, { offset, length }]);
  }

  async next(): Promise<GdbSessionState> {
    this.ensureRpc();
    this.lastState = await this.rpc!.call<GdbSessionState>("gdb.next", [this.sessionId]);
    await this.handleSequenceProgress();
    await this.maybeAutoLoadForCurrent();
    return this.lastState!;
  }

  async continue(): Promise<GdbSessionState> {
    this.ensureRpc();
    this.lastState = await this.rpc!.call<GdbSessionState>("gdb.continue", [this.sessionId]);
    await this.handleSequenceProgress();
    await this.maybeAutoLoadForCurrent();
    return this.lastState!;
  }

  async setSourceBreakpoints(filePath: string, lines: number[]): Promise<void> {
    this.ensureRpc();
    const sourceName = this.resolveSourceName(filePath);
    this.sourcePathToName.set(filePath, sourceName);
    this.notifier?.info(`[runtime] setSourceBreakpoints path=${filePath} sourceName=${sourceName} lines=${lines.join(",") || "<none>"}`);

    const existing = this.sourcePathToBreakpointIds.get(filePath) ?? [];
    for (const id of existing) {
      await this.rpc!.call("gdb.deleteBreakpoint", [this.sessionId, { id }]);
      this.notifier?.info(`[runtime] deleted breakpoint id=${id}`);
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
      this.notifier?.info(`[runtime] created source breakpoint id=${id}`);
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

  private async startNativeSequenceSession(steps: SequenceStep[]): Promise<GdbSessionState> {
    this.ensureRpc();
    const payloadSteps = steps.map((step, index) => ({
      kind: "call",
      label: step.label || `step-${index + 1}`,
      request: {
        from: step.from,
        to: step.to,
        input: step.input,
        value: step.value ?? "0x0",
        gas: step.gas ?? "0x0"
      },
      block: step.block ?? this.launch.block ?? "latest"
    }));

    const carry = this.launch.carryUserMutations === false ? "none" : "full";
    const response = await this.rpc!.call<NativeSequenceResponse>("gdb.startSequenceSession", [
      {
        steps: payloadSteps,
        stateCarry: carry
      }
    ]);

    if (!response.sequenceId) {
      throw new Error("native sequence did not return sequenceId");
    }
    if (!response.activeSessionId || !response.activeSession) {
      throw new Error("native sequence did not return active session");
    }

    this.sequence = this.sequence || { index: 0, steps, carryMutations: [], usingNativeSequence: true };
    this.sequence.sequenceId = response.sequenceId;
    this.sequence.index = response.currentStepIndex;
    this.sequence.usingNativeSequence = true;
    this.sessionId = response.activeSessionId;

    return response.activeSession;
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

    if (this.sequence.usingNativeSequence && this.sequence.sequenceId) {
      const advanced = await this.rpc!.call<NativeSequenceResponse>("gdb.nextStepSession", [this.sequence.sequenceId]);
      if (advanced.done) {
        this.lastState = { ...this.lastState, done: true, current: undefined };
        return;
      }
      if (!advanced.activeSessionId || !advanced.activeSession) {
        throw new Error("native sequence advance missing active session");
      }
      this.sessionId = advanced.activeSessionId;
      this.sequence.index = advanced.currentStepIndex;
      this.lastState = advanced.activeSession;
      this.autoLoadedAddresses.clear();

      if (this.launch.sourceBundle) {
        await this.loadSourceBundle(this.launch.sourceBundle, "sequence step");
      }

      for (const filePath of this.sourcePathToBreakpointIds.keys()) {
        await this.reapplyBreakpointsForFile(filePath);
      }
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
    this.autoLoadedAddresses.clear();

    if (this.launch.sourceBundle) {
      await this.loadSourceBundle(this.launch.sourceBundle, "sequence step");
    }

    for (const filePath of this.sourcePathToBreakpointIds.keys()) {
      await this.reapplyBreakpointsForFile(filePath);
    }
  }

  private ensureRpc(): void {
    if (!this.rpc) {
      throw new Error("runtime is not started");
    }
  }

  private async loadSourceBundle(bundle: SourceBundleConfig, reason: string): Promise<void> {
    this.ensureRpc();
    try {
      const updated = await this.rpc!.call<GdbSessionState>("gdb.loadSourceBundle", [this.sessionId, bundle]);
      this.lastState = updated;
      const tag = updated.bundleSource ? `via ${updated.bundleSource}` : reason;
      this.notifier?.info(`Source bundle loaded ${tag}`);
      if (updated.bundleDiagnostics?.length) {
        for (const line of updated.bundleDiagnostics) {
          this.notifier?.info(`  ${line}`);
        }
      }
      // Re-apply any pre-existing source breakpoints because PCs may now resolve.
      for (const filePath of this.sourcePathToBreakpointIds.keys()) {
        await this.reapplyBreakpointsForFile(filePath);
      }
    } catch (error) {
      const message = String(error);
      if (bundle.kind === "auto") {
        this.notifier?.warn(`Auto source detection failed: ${message}`);
      } else {
        this.notifier?.warn(`Source bundle load failed: ${message}`);
      }
    }
  }

  private async configureAutoMatch(): Promise<void> {
    if (!this.rpc || !this.sessionId) {
      return;
    }
    const cfg = this.launch as InspethctLaunchConfig & { workspaceRoot?: string; explorerFallbackEnabled?: boolean };
    const projectRoot = cfg.workspaceRoot ?? "";
    const baseBundle = this.launch.sourceBundle;
    try {
      await this.rpc.call("gdb.configureAutoMatch", [
        this.sessionId,
        {
          projectRoot,
          explorerEnabled: cfg.explorerFallbackEnabled === true,
          apiBase: baseBundle?.apiBase ?? "",
          apiKey: baseBundle?.apiKey ?? "",
          rpcUrl: this.launch.upstream ?? "",
          chainId: baseBundle?.chainId ?? "1"
        }
      ]);
    } catch {
      // Best-effort configuration; keep debugging flow working even when unavailable.
    }
  }

  private async maybeAutoLoadForCurrent(): Promise<void> {
    if (this.launch.autoSourceBundle === false) {
      return;
    }
    const codeAddress = this.lastState?.current?.codeAddress?.toLowerCase();
    if (!codeAddress || codeAddress === "0x" || codeAddress === "") {
      return;
    }
    if (this.autoLoadedAddresses.has(codeAddress)) {
      return;
    }
    if (this.lastState?.bundles?.some((b) => b.codeAddress?.toLowerCase() === codeAddress)) {
      this.autoLoadedAddresses.add(codeAddress);
      return;
    }
    this.autoLoadedAddresses.add(codeAddress);
    const baseBundle = this.launch.sourceBundle;
    const folderRoot = (this.launch as InspethctLaunchConfig & { workspaceRoot?: string }).workspaceRoot;
    const autoBundle: SourceBundleConfig = {
      kind: "auto",
      contractName: "",
      runtime: true,
      projectRoot: baseBundle?.projectRoot || folderRoot,
      apiBase: baseBundle?.apiBase,
      apiKey: baseBundle?.apiKey,
      chainId: baseBundle?.chainId,
      address: codeAddress,
      codeAddress
    };
    await this.loadSourceBundle(autoBundle, `auto for ${codeAddress}`);
  }

  private async reapplyBreakpointsForFile(filePath: string): Promise<void> {
    const ids = this.sourcePathToBreakpointIds.get(filePath) ?? [];
    if (ids.length === 0) {
      return;
    }
    const lines = ids
      .map((id) => Number.parseInt(id.slice(id.lastIndexOf("-") + 1), 10))
      .filter((line) => Number.isFinite(line));
    await this.setSourceBreakpoints(filePath, lines);
  }
}
