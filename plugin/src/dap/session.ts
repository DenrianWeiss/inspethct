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
  private configurationDone = false;
  private readonly processKey: string;

  constructor(private readonly processManager: DbgserverProcessManager) {
    super("inspethct-debug.txt");
    this.setDebuggerLinesStartAt1(true);
    this.setDebuggerColumnsStartAt1(true);
    this.processKey = `inspethct-${Date.now()}-${Math.floor(Math.random() * 100000)}`;
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
    this.configurationDone = true;
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

      this.runtime = new InspethctRuntime(this.processManager, launchArgs, this.processKey);
      const state = await this.runtime.start();

      for (const [filePath, lines] of this.pendingBreakpoints.entries()) {
        await this.runtime.setSourceBreakpoints(filePath, lines);
      }

      this.sendResponse(response);
      this.sendEvent(new OutputEvent(`Connected to dbgserver\n`, "console"));

      if (state.current) {
        this.sendEvent(new StoppedEvent(state.current.reason || "entry", THREAD_ID));
      } else if (!state.done) {
        const paused = await this.runtime.next();
        this.sendEvent(new StoppedEvent(paused.current?.reason || "entry", THREAD_ID));
      } else {
        this.sendEvent(new TerminatedEvent());
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
    const clientLines = args.breakpoints?.map((b) => b.line).filter((n): n is number => typeof n === "number") ?? [];

    if (!filePath) {
      response.body = { breakpoints: [] };
      this.sendResponse(response);
      return;
    }

    this.pendingBreakpoints.set(filePath, clientLines);
    if (this.runtime) {
      await this.runtime.setSourceBreakpoints(filePath, clientLines);
    }

    response.body = {
      breakpoints: clientLines.map((line) => new Breakpoint(true, line))
    };
    this.sendResponse(response);
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
    const startedAt = Date.now();
    while (!this.configurationDone && Date.now() - startedAt < 5000) {
      await new Promise((resolve) => setTimeout(resolve, 25));
    }
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
