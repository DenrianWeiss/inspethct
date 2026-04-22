import {
  InitializedEvent,
  LoggingDebugSession,
  OutputEvent,
  Scope,
  Source,
  StoppedEvent,
  TerminatedEvent,
  Thread,
  Handles,
  Breakpoint
} from "@vscode/debugadapter";
import * as path from "node:path";
import { DebugProtocol } from "@vscode/debugprotocol";
import { InspethctLaunchConfig } from "../types";
import { InspethctRuntime } from "./runtime";
import { DbgserverProcessManager } from "../services/processManager";
import { runRepl } from "./repl";
import { getAddress, toUtf8String } from "ethers";

const THREAD_ID = 1;

interface VariableBag {
  title: string;
  content: unknown;
}

export class InspethctDebugSession extends LoggingDebugSession {
  private launchConfig?: InspethctLaunchConfig;
  private runtime?: InspethctRuntime;
  private readonly variableHandles = new Handles<VariableBag>();
  private readonly pendingBreakpoints = new Map<string, number[]>();
  private readonly configurationDonePromise: Promise<void>;
  private resolveConfigurationDone?: () => void;
  private readonly processKey: string;

  constructor(private readonly processManager: DbgserverProcessManager) {
    super("inspethct-debug.txt");
    this.setDebuggerLinesStartAt1(true);
    this.setDebuggerColumnsStartAt1(true);
    this.processKey = `inspethct-${Date.now()}-${Math.floor(Math.random() * 100000)}`;
    this.configurationDonePromise = new Promise<void>((resolve) => {
      this.resolveConfigurationDone = resolve;
    });
  }

  protected initializeRequest(
    response: DebugProtocol.InitializeResponse,
    _args: DebugProtocol.InitializeRequestArguments
  ): void {
    response.body = {
      supportsConfigurationDoneRequest: true,
      supportsSetVariable: false,
      supportsEvaluateForHovers: true,
      supportsReadMemoryRequest: true
    };
    this.sendResponse(response);
    this.sendEvent(new InitializedEvent());
  }

  protected configurationDoneRequest(
    response: DebugProtocol.ConfigurationDoneResponse,
    _args: DebugProtocol.ConfigurationDoneArguments
  ): void {
    this.resolveConfigurationDone?.();
    this.sendResponse(response);
  }

  protected async launchRequest(
    response: DebugProtocol.LaunchResponse,
    args: DebugProtocol.LaunchRequestArguments
  ): Promise<void> {
    try {
      const launchArgs = args as InspethctLaunchConfig & { workspaceRoot?: string };
      this.launchConfig = launchArgs;
      await this.waitForConfigurationDone();

      this.runtime = new InspethctRuntime(this.processManager, launchArgs, this.processKey, {
        info: (msg) => this.sendEvent(new OutputEvent(`${msg}\n`, "console")),
        warn: (msg) => this.sendEvent(new OutputEvent(`${msg}\n`, "stderr"))
      });
      const state = await this.runtime.start();

      for (const [filePath, lines] of this.pendingBreakpoints.entries()) {
        await this.runtime.setSourceBreakpoints(filePath, lines);
      }

      this.sendResponse(response);
      this.sendEvent(new OutputEvent(`Connected to dbgserver\n`, "console"));

      const stopOnEntry = launchArgs.stopOnEntry === true;
      this.sendEvent(new OutputEvent(`[dap] launch stopOnEntry=${String(stopOnEntry)}\n`, "console"));
      let activeState = state;
      this.sendEvent(new OutputEvent(`[dap] initial state: ${this.describeState(activeState)}\n`, "console"));
      if (!activeState.current && !activeState.done) {
        // Ensure we have an initial paused state before deciding whether to auto-continue.
        activeState = await this.runtime.next();
        this.sendEvent(new OutputEvent(`[dap] primed state via next: ${this.describeState(activeState)}\n`, "console"));
      }

      if (!stopOnEntry && !activeState.done) {
        this.sendEvent(new OutputEvent(`[dap] auto-continue enabled, running to first breakpoint\n`, "console"));
        activeState = await this.autoRunToBreakpoint(activeState);
        this.sendEvent(new OutputEvent(`[dap] state after auto-run: ${this.describeState(activeState)}\n`, "console"));
      }

      if (activeState.done && !activeState.current) {
        this.sendEvent(new TerminatedEvent());
      } else {
        this.sendEvent(new StoppedEvent(activeState.current?.reason || (stopOnEntry ? "entry" : "breakpoint"), THREAD_ID));
      }
    } catch (error) {
      response.success = false;
      response.message = String(error);
      this.sendResponse(response);
      this.sendEvent(new OutputEvent(`Launch failed: ${String(error)}\n`, "stderr"));
      this.runtime?.stop();
    }
  }

  protected async disconnectRequest(
    response: DebugProtocol.DisconnectResponse,
    _args: DebugProtocol.DisconnectArguments
  ): Promise<void> {
    this.runtime?.stop();
    this.sendResponse(response);
    this.sendEvent(new TerminatedEvent());
  }

  protected threadsRequest(response: DebugProtocol.ThreadsResponse): void {
    response.body = { threads: [new Thread(THREAD_ID, "main")] };
    this.sendResponse(response);
  }

  protected async setBreakPointsRequest(
    response: DebugProtocol.SetBreakpointsResponse,
    args: DebugProtocol.SetBreakpointsArguments
  ): Promise<void> {
    const filePath = args.source.path;
    const sourceName = args.source.name;
    const clientLines = args.breakpoints?.map((b) => b.line).filter((n): n is number => typeof n === "number") ?? [];

    this.sendEvent(
      new OutputEvent(
        `[dap] setBreakPointsRequest source=${sourceName || "<unknown>"} path=${filePath || "<missing>"} lines=${clientLines.join(",") || "<none>"}\n`,
        "console"
      )
    );

    if (!filePath) {
      response.body = { breakpoints: [] };
      this.sendResponse(response);
      return;
    }

    // The dbgserver source map only understands Solidity source names.
    if (!this.isSolidityPath(filePath)) {
      this.sendEvent(new OutputEvent(`[dap] skip non-solidity breakpoint file=${filePath}\n`, "console"));
      response.body = {
        breakpoints: clientLines.map(
          (line): DebugProtocol.Breakpoint => ({
            verified: false,
            line,
            message: "Only Solidity source breakpoints are supported."
          })
        )
      };
      this.sendResponse(response);
      return;
    }

    this.pendingBreakpoints.set(filePath, clientLines);
    if (this.runtime) {
      try {
        await this.runtime.setSourceBreakpoints(filePath, clientLines);
      } catch (error) {
        const message = String(error);
        const knownMissingSource = /source\s+".+"\s+not found/i.test(message) && message.includes("gdb.setSourceBreakpoint");
        this.sendEvent(new OutputEvent(`[dap] set breakpoint failed file=${filePath}: ${message}\n`, "stderr"));
        response.body = {
          breakpoints: clientLines.map(
            (line): DebugProtocol.Breakpoint => ({
              verified: false,
              line,
              message: knownMissingSource
                ? "This file is not in the loaded contract source bundle."
                : `Failed to set breakpoint: ${message}`
            })
          )
        };
        this.sendResponse(response);
        return;
      }
    }

    this.sendEvent(new OutputEvent(`[dap] applied source breakpoints file=${filePath} count=${clientLines.length}\n`, "console"));

    response.body = {
      breakpoints: clientLines.map((line) => new Breakpoint(true, line))
    };
    this.sendResponse(response);
  }

  private isSolidityPath(filePath: string): boolean {
    return path.extname(filePath).toLowerCase() === ".sol";
  }

  private async autoRunToBreakpoint(state: Awaited<ReturnType<InspethctRuntime["start"]>>): Promise<Awaited<ReturnType<InspethctRuntime["start"]>>> {
    // Reasons that mean "we are not at a real stop yet, keep going". Anything
    // else (e.g. "breakpoint", "exception", custom break reasons) is a genuine
    // pause that the user should see.
    const transientReasons = new Set(["entry", "step", "", "none"]);
    let current = state;
    for (let attempt = 1; attempt <= 8; attempt++) {
      if (current.done) {
        return current;
      }
      const reason = current.current?.reason || "";
      if (current.current && !transientReasons.has(reason)) {
        return current;
      }
      this.sendEvent(new OutputEvent(`[dap] auto-run attempt=${attempt} continue from reason=${reason || "none"}\n`, "console"));
      const next = await this.runtime!.continue();
      if (this.samePausePoint(current, next) && !next.done) {
        this.sendEvent(new OutputEvent(`[dap] continue made no progress, issuing one next() before retry\n`, "console"));
        current = await this.runtime!.next();
        continue;
      }
      current = next;
    }
    return current;
  }

  private samePausePoint(a: Awaited<ReturnType<InspethctRuntime["start"]>>, b: Awaited<ReturnType<InspethctRuntime["start"]>>): boolean {
    const aPc = a.current?.step?.pc;
    const bPc = b.current?.step?.pc;
    const aReason = a.current?.reason || "";
    const bReason = b.current?.reason || "";
    return !a.done && !b.done && aPc === bPc && aReason === bReason;
  }

  private describeState(state: Awaited<ReturnType<InspethctRuntime["start"]>>): string {
    if (state.done && !state.current) {
      return "done=true current=none";
    }
    return `done=${String(state.done)} reason=${state.current?.reason || "none"} pc=${String(state.current?.step?.pc ?? "n/a")}`;
  }

  protected async nextRequest(response: DebugProtocol.NextResponse, _args: DebugProtocol.NextArguments): Promise<void> {
    try {
      const state = await this.runtime!.next();
      this.sendResponse(response);
      if (state.done && !state.current) {
        this.sendEvent(new TerminatedEvent());
        return;
      }
      this.sendEvent(new StoppedEvent(state.current?.reason || "step", THREAD_ID));
    } catch (error) {
      response.success = false;
      response.message = String(error);
      this.sendResponse(response);
    }
  }

  protected async continueRequest(
    response: DebugProtocol.ContinueResponse,
    _args: DebugProtocol.ContinueArguments
  ): Promise<void> {
    try {
      const state = await this.runtime!.continue();
      this.sendResponse(response);
      if (state.done && !state.current) {
        this.sendEvent(new TerminatedEvent());
        return;
      }
      this.sendEvent(new StoppedEvent(state.current?.reason || "breakpoint", THREAD_ID));
    } catch (error) {
      response.success = false;
      response.message = String(error);
      this.sendResponse(response);
    }
  }

  protected stackTraceRequest(
    response: DebugProtocol.StackTraceResponse,
    _args: DebugProtocol.StackTraceArguments
  ): void {
    const state = this.runtime?.getState();
    const current = state?.current;
    const sourceName = current?.source?.sourceName;
    const localPath = this.runtime?.resolveLocalPath(sourceName);

    const baseFrame: DebugProtocol.StackFrame = {
      id: 1,
      name: this.formatFrameName(current?.step?.op || "EVM", current?.callStack?.[current.callStack.length - 1]),
      line: current?.source?.line || 1,
      column: current?.source?.column || 1,
      source: localPath ? new Source(sourceName || "contract", localPath) : undefined
    };

    const frames: DebugProtocol.StackFrame[] = [baseFrame];
    const callStack = current?.callStack ?? [];
    // Surface parent frames (older callers) below the current frame as
    // synthetic entries so users can see who invoked the current contract.
    // The active frame is already represented by `baseFrame` so we append
    // depth-1 .. 0 in order from caller to root.
    for (let i = callStack.length - 2; i >= 0; i--) {
      const f = callStack[i];
      frames.push({
        id: 100 + f.depth,
        name: this.formatFrameName(`depth ${f.depth}`, f),
        line: 0,
        column: 0,
        presentationHint: "label"
      });
    }

    response.body = {
      stackFrames: frames,
      totalFrames: frames.length
    };
    this.sendResponse(response);
  }

  private formatFrameName(prefix: string, frame?: import("./runtime").CallFrameInfo): string {
    if (!frame) {
      return prefix;
    }
    const tag = frame.callType ? `[${frame.callType}]` : "";
    const sel = frame.selector ? ` ${frame.selector}` : "";
    return `${prefix} ${tag} ${frame.codeAddress}${sel}`.trim();
  }

  protected scopesRequest(response: DebugProtocol.ScopesResponse, _args: DebugProtocol.ScopesArguments): void {
    const state = this.runtime?.getState();
    const current = state?.current;

    const scopes = [
      new Scope("Step", this.variableHandles.create({ title: "step", content: current?.step ?? {} }), false),
      new Scope("Locals", this.variableHandles.create({ title: "locals", content: { kind: "locals", locals: current?.locals ?? [] } }), false),
      new Scope("Storage", this.variableHandles.create({ title: "storage", content: { kind: "storage-list", entries: current?.storage ?? [] } }), false),
      new Scope("Transient", this.variableHandles.create({ title: "transient", content: { kind: "storage-list", entries: current?.transient ?? [] } }), false),
      new Scope("Memory", this.variableHandles.create({
        title: "memory",
        content: {
          kind: "memory-root",
          memory: current?.memory,
          memorySize: current?.memorySize ?? 0,
          truncated: current?.memoryTruncated === true,
          regions: current?.memoryRegions ?? [],
          freeMemoryPointer: current?.freeMemoryPointer ?? 0
        }
      }), false),
      new Scope("Stack", this.variableHandles.create({ title: "stack", content: { kind: "stack", values: current?.stack ?? [] } }), false),
      new Scope("Access", this.variableHandles.create({
        title: "access",
        content: {
          callAccess: current?.callAccess,
          storageAccess: current?.storageAccess,
          memoryAccess: current?.memoryAccess
        }
      }), false)
    ];

    response.body = { scopes };
    this.sendResponse(response);
  }

  protected variablesRequest(
    response: DebugProtocol.VariablesResponse,
    args: DebugProtocol.VariablesArguments
  ): void {
    const bag = this.variableHandles.get(args.variablesReference);
    if (!bag) {
      response.body = { variables: [] };
      this.sendResponse(response);
      return;
    }
    const variables = this.renderVariables(bag.content);
    response.body = { variables };
    this.sendResponse(response);
  }

  private renderVariables(content: unknown): DebugProtocol.Variable[] {
    if (content && typeof content === "object" && !Array.isArray(content)) {
      const tagged = content as { kind?: string };
      switch (tagged.kind) {
        case "memory-root":
          return this.renderMemoryRoot(content as MemoryRootBag);
        case "memory-region":
          return this.renderMemoryRegion(content as MemoryRegionBag);
        case "memory-words":
          return this.renderMemoryWords(content as MemoryWordsBag);
        case "stack":
          return this.renderStack(content as StackBag);
        case "locals":
          return this.renderLocals(content as LocalsBag);
        case "storage-list":
          return this.renderStorageList(content as StorageListBag);
        case "storage-entry":
          return this.renderStorageEntry(content as StorageEntryBag);
      }
    }
    return flattenObject(content);
  }

  private renderMemoryRoot(bag: MemoryRootBag): DebugProtocol.Variable[] {
    const variables: DebugProtocol.Variable[] = [];
    variables.push({
      name: "size",
      value: `${bag.memorySize} bytes${bag.truncated ? " (inline truncated; use readMemory)" : ""}`,
      variablesReference: 0
    });
    variables.push({
      name: "freeMemoryPointer",
      value: `0x${bag.freeMemoryPointer.toString(16)}`,
      variablesReference: 0
    });
    for (const region of bag.regions) {
      const ref = this.variableHandles.create({
        title: region.label,
        content: {
          kind: "memory-region",
          region
        } satisfies MemoryRegionBag
      });
      variables.push({
        name: region.label,
        value: `[0x${region.offset.toString(16)} +${region.length}]`,
        variablesReference: ref,
        memoryReference: `mem:${region.offset}:${region.length}`
      });
    }
    if (bag.regions.length === 0 && bag.memorySize > 0) {
      const ref = this.variableHandles.create({
        title: "memory",
        content: { kind: "memory-words", offset: 0, length: bag.memorySize } satisfies MemoryWordsBag
      });
      variables.push({
        name: "memory",
        value: `${bag.memorySize} bytes`,
        variablesReference: ref,
        memoryReference: `mem:0:${bag.memorySize}`
      });
    }
    return variables;
  }

  private renderMemoryRegion(bag: MemoryRegionBag): DebugProtocol.Variable[] {
    const wordsRef = this.variableHandles.create({
      title: bag.region.label,
      content: { kind: "memory-words", offset: bag.region.offset, length: bag.region.length } satisfies MemoryWordsBag
    });
    return [
      { name: "kind", value: bag.region.kind, variablesReference: 0 },
      { name: "offset", value: `0x${bag.region.offset.toString(16)}`, variablesReference: 0 },
      { name: "length", value: `${bag.region.length}`, variablesReference: 0 },
      {
        name: "words",
        value: `${Math.ceil(bag.region.length / 32)} x 32-byte words`,
        variablesReference: wordsRef,
        memoryReference: `mem:${bag.region.offset}:${bag.region.length}`
      }
    ];
  }

  private renderMemoryWords(bag: MemoryWordsBag): DebugProtocol.Variable[] {
    const state = this.runtime?.getState();
    const memHex = state?.current?.memory ?? "0x";
    const inline = hexToBytes(memHex);
    const variables: DebugProtocol.Variable[] = [];
    const end = bag.offset + bag.length;
    for (let off = bag.offset; off < end; off += 32) {
      const remain = Math.min(32, end - off);
      const slice = off + remain <= inline.length
        ? inline.subarray(off, off + remain)
        : null;
      const value = slice
        ? `0x${bufferToHex(slice)}`
        : `<paged: readMemory>`;
      variables.push({
        name: `0x${off.toString(16).padStart(4, "0")}`,
        value,
        variablesReference: 0,
        memoryReference: `mem:${off}:${remain}`
      });
    }
    return variables;
  }

  private renderStack(bag: StackBag): DebugProtocol.Variable[] {
    return bag.values.map((value, index) => ({
      name: index === 0 ? "top" : `[${index}]`,
      value,
      variablesReference: 0
    }));
  }

  private renderLocals(bag: LocalsBag): DebugProtocol.Variable[] {
    if (bag.locals.length === 0) {
      return [{ name: "<no locals>", value: "AST scope unavailable at current PC", variablesReference: 0 }];
    }
    return bag.locals.map((local) => {
      const hasValue = !!local.value && local.value.length > 0;
      const value = hasValue ? local.value! : `<${local.confidence || "unavailable"}>`;
      const tags: string[] = [local.kind];
      if (local.storageLocation && local.storageLocation !== "stack") {
        tags.push(local.storageLocation);
      }
      if (local.confidence && local.confidence !== "unavailable") {
        tags.push(`conf=${local.confidence}`);
      }
      if (local.stackIndex && local.stackIndex > 0) {
        tags.push(`stack[${local.stackIndex - 1}]`);
      }
      if (local.memoryPointer && local.memoryPointer > 0) {
        tags.push(`mem@0x${local.memoryPointer.toString(16)}`);
      }
      const note = local.note ? `  // ${local.note}` : "";
      const line = local.declaredAtLine ? `  L${local.declaredAtLine}` : "";
      return {
        name: local.name,
        type: local.type,
        value: `${value}  [${tags.join(", ")}]${line}${note}`,
        variablesReference: 0
      };
    });
  }

  private renderStorageList(bag: StorageListBag): DebugProtocol.Variable[] {
    if (bag.entries.length === 0) {
      return [{ name: "<no entries>", value: "", variablesReference: 0 }];
    }
    return bag.entries.map((entry) => {
      const ref = this.variableHandles.create({
        title: entry.name,
        content: { kind: "storage-entry", entry } satisfies StorageEntryBag
      });
      const decoded = decodeStorageValue(entry);
      return {
        name: entry.name || entry.slot,
        type: entry.type,
        value: decoded,
        variablesReference: ref
      };
    });
  }

  private renderStorageEntry(bag: StorageEntryBag): DebugProtocol.Variable[] {
    const entry = bag.entry;
    return [
      { name: "slot", value: entry.slot, variablesReference: 0 },
      { name: "raw", value: entry.value, variablesReference: 0 },
      { name: "type", value: entry.type ?? "unknown", variablesReference: 0 },
      { name: "decoded", value: decodeStorageValue(entry), variablesReference: 0 }
    ];
  }

  protected async readMemoryRequest(
    response: DebugProtocol.ReadMemoryResponse,
    args: DebugProtocol.ReadMemoryArguments
  ): Promise<void> {
    try {
      const parsed = parseMemoryReference(args.memoryReference);
      const baseOffset = parsed?.offset ?? 0;
      const offset = baseOffset + (args.offset ?? 0);
      const count = args.count;
      const result = await this.runtime!.readMemory(offset, count);
      const data = hexToBytes(result.data);
      response.body = {
        address: `0x${offset.toString(16)}`,
        data: Buffer.from(data).toString("base64"),
        unreadableBytes: Math.max(0, count - data.length)
      };
      this.sendResponse(response);
    } catch (error) {
      response.success = false;
      response.message = String(error);
      this.sendResponse(response);
    }
  }

  protected async evaluateRequest(
    response: DebugProtocol.EvaluateResponse,
    args: DebugProtocol.EvaluateArguments
  ): Promise<void> {
    if (!this.runtime) {
      response.success = false;
      response.message = "runtime not started";
      this.sendResponse(response);
      return;
    }
    try {
      const text = await runRepl(args.expression, this.runtime, (line) => {
        this.sendEvent(new OutputEvent(`${line}\n`, "console"));
      });
      response.body = {
        result: text,
        variablesReference: 0
      };
      this.sendResponse(response);
      // Refresh stopped state if a continue/step was issued.
      if (/^(c|continue|n|s|step|next)\b/i.test(args.expression.trim())) {
        const state = this.runtime.getState();
        if (state?.done && !state.current) {
          this.sendEvent(new TerminatedEvent());
        } else {
          this.sendEvent(new StoppedEvent(state?.current?.reason || "breakpoint", THREAD_ID));
        }
      }
    } catch (error) {
      response.success = false;
      response.message = String(error);
      this.sendResponse(response);
    }
  }

  private async waitForConfigurationDone(): Promise<void> {
    await Promise.race([
      this.configurationDonePromise,
      new Promise<void>((resolve) => setTimeout(resolve, 5000))
    ]);
  }
}

function flattenObject(content: unknown): DebugProtocol.Variable[] {
  if (content === null || content === undefined) {
    return [];
  }
  if (Array.isArray(content)) {
    return content.map((value, index) => ({
      name: String(index),
      value: normalizeValue(value),
      variablesReference: 0
    }));
  }
  if (typeof content === "object") {
    return Object.entries(content as Record<string, unknown>).map(([name, value]) => ({
      name,
      value: normalizeValue(value),
      variablesReference: 0
    }));
  }
  return [
    {
      name: "value",
      value: normalizeValue(content),
      variablesReference: 0
    }
  ];
}

function normalizeValue(value: unknown): string {
  if (value === null || value === undefined) {
    return "";
  }
  if (typeof value === "string") {
    return value;
  }
  if (typeof value === "number" || typeof value === "boolean" || typeof value === "bigint") {
    return String(value);
  }
  try {
    return JSON.stringify(value);
  } catch {
    return String(value);
  }
}

interface MemoryRootBag {
  kind: "memory-root";
  memory?: string;
  memorySize: number;
  truncated: boolean;
  regions: { kind: string; label: string; offset: number; length: number }[];
  freeMemoryPointer: number;
}

interface MemoryRegionBag {
  kind: "memory-region";
  region: { kind: string; label: string; offset: number; length: number };
}

interface MemoryWordsBag {
  kind: "memory-words";
  offset: number;
  length: number;
}

interface StackBag {
  kind: "stack";
  values: string[];
}

interface LocalsBag {
  kind: "locals";
  locals: import("./runtime").LocalVariable[];
}

interface StorageListBag {
  kind: "storage-list";
  entries: import("./runtime").StorageVariable[];
}

interface StorageEntryBag {
  kind: "storage-entry";
  entry: import("./runtime").StorageVariable;
}

function hexToBytes(hex: string | undefined): Uint8Array {
  if (!hex || !hex.startsWith("0x")) {
    return new Uint8Array(0);
  }
  const stripped = hex.slice(2);
  if (stripped.length === 0) {
    return new Uint8Array(0);
  }
  const padded = stripped.length % 2 === 0 ? stripped : "0" + stripped;
  const out = new Uint8Array(padded.length / 2);
  for (let i = 0; i < out.length; i++) {
    out[i] = parseInt(padded.substr(i * 2, 2), 16);
  }
  return out;
}

function bufferToHex(buffer: Uint8Array): string {
  let s = "";
  for (let i = 0; i < buffer.length; i++) {
    s += buffer[i].toString(16).padStart(2, "0");
  }
  return s;
}

function parseMemoryReference(ref: string | undefined): { offset: number; length: number } | undefined {
  if (!ref || !ref.startsWith("mem:")) {
    return undefined;
  }
  const parts = ref.slice(4).split(":");
  if (parts.length < 1) {
    return undefined;
  }
  const offset = Number(parts[0]);
  const length = parts.length > 1 ? Number(parts[1]) : 0;
  if (Number.isNaN(offset) || Number.isNaN(length)) {
    return undefined;
  }
  return { offset, length };
}

function decodeStorageValue(entry: import("./runtime").StorageVariable): string {
  const raw = entry.value || "0x";
  const type = (entry.type || "").toLowerCase().replace(/^t_/, "");
  const bytes = hexToBytes(raw);
  if (bytes.length === 0) {
    return raw;
  }
  // address / contract / address payable
  if (type.startsWith("address") || type.startsWith("contract")) {
    try {
      const slice = bytes.length >= 20 ? bytes.subarray(bytes.length - 20) : bytes;
      return getAddress(`0x${bufferToHex(slice)}`);
    } catch {
      // fall through
    }
  }
  if (type === "bool") {
    return bytes[bytes.length - 1] === 0 ? "false" : "true";
  }
  // bytesN (fixed) — e.g. bytes32, bytes4
  const fixedBytes = type.match(/^bytes(\d+)$/);
  if (fixedBytes) {
    const n = Math.min(32, parseInt(fixedBytes[1], 10));
    return `0x${bufferToHex(bytes.subarray(0, n))}`;
  }
  // intN (signed)
  const intMatch = type.match(/^int(\d*)$/);
  if (intMatch) {
    const bits = intMatch[1] ? parseInt(intMatch[1], 10) : 256;
    try {
      const u = BigInt(raw);
      const signBit = 1n << BigInt(bits - 1);
      const signed = u >= signBit ? u - (1n << BigInt(bits)) : u;
      return `${signed.toString()} (${raw})`;
    } catch {
      return raw;
    }
  }
  // uintN (unsigned)
  if (/^uint(\d*)$/.test(type)) {
    try {
      const u = BigInt(raw);
      return `${u.toString()} (${raw})`;
    } catch {
      return raw;
    }
  }
  // string/bytes (storage short form: low byte = 2*len for short ≤31 bytes;
  // long form: low byte = 2*len+1, data lives at keccak(slot)). We can only
  // decode the short form from a single slot.
  if (type === "string" || type === "bytes") {
    const lowByte = bytes[bytes.length - 1] ?? 0;
    if ((lowByte & 1) === 0) {
      const len = lowByte / 2;
      const data = bytes.subarray(0, len);
      if (type === "string") {
        try {
          return `${JSON.stringify(toUtf8String(data))} (len=${len})`;
        } catch {
          return `0x${bufferToHex(data)} (len=${len})`;
        }
      }
      return `0x${bufferToHex(data)} (len=${len})`;
    }
    return `${raw} (long; data at keccak(slot))`;
  }
  // enum: numeric
  if (type.startsWith("enum")) {
    try {
      return BigInt(raw).toString();
    } catch {
      return raw;
    }
  }
  return raw;
}
