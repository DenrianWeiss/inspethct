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
      supportsEvaluateForHovers: false
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

    response.body = {
      stackFrames: [
        {
          id: 1,
          name: current?.step?.op || "EVM",
          line: current?.source?.line || 1,
          column: current?.source?.column || 1,
          source: localPath ? new Source(sourceName || "contract", localPath) : undefined
        }
      ],
      totalFrames: 1
    };
    this.sendResponse(response);
  }

  protected scopesRequest(response: DebugProtocol.ScopesResponse, _args: DebugProtocol.ScopesArguments): void {
    const state = this.runtime?.getState();
    const current = state?.current;

    const scopes = [
      new Scope("Step", this.variableHandles.create({ title: "step", content: current?.step ?? {} }), false),
      new Scope("Storage", this.variableHandles.create({ title: "storage", content: current?.storage ?? [] }), false),
      new Scope("Transient", this.variableHandles.create({ title: "transient", content: current?.transient ?? [] }), false),
      new Scope("Memory", this.variableHandles.create({ title: "memory", content: { memory: current?.memory, memorySize: current?.memorySize } }), false),
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

    const variables = flattenObject(bag.content);
    response.body = { variables };
    this.sendResponse(response);
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
