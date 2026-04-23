import { GdbSessionState, InspethctRuntime, LocalVariable, PeekSnapshot, PeekVariable, StorageVariable } from "./runtime";

/**
 * runRepl interprets a single CLI-style command line typed into the VS Code
 * Debug Console (or the Inspethct REPL output channel) and executes it
 * against the active runtime. Returns the user-visible result text.
 */
export async function runRepl(
  expression: string,
  runtime: InspethctRuntime,
  log: (line: string) => void
): Promise<string> {
  const trimmed = (expression || "").trim();
  if (trimmed.length === 0) {
    return "";
  }
  const fields = tokenize(trimmed);
  const verb = fields[0].toLowerCase();
  const rest = fields.slice(1);

  switch (verb) {
    case "help":
    case "?":
      return helpText(rest[0]);
    case "c":
    case "continue": {
      const state = await runtime.continue();
      return formatStateLine(state);
    }
    case "n":
    case "s":
    case "step":
    case "next": {
      const state = await runtime.next();
      return formatStateLine(state);
    }
    case "si":
    case "stepin":
    case "step-in": {
      const state = await runtime.stepIn();
      return formatStateLine(state);
    }
    case "so":
    case "stepout":
    case "step-out": {
      const state = await runtime.stepOut();
      return formatStateLine(state);
    }
    case "state": {
      const state = await runtime.refreshState();
      return formatStateLine(state);
    }
    case "b":
    case "break":
      return handleBreak(rest, runtime);
    case "info":
    case "i":
      return handleInfo(rest, runtime);
    case "print":
    case "p":
      return handlePrint(rest, runtime);
    case "x":
      return handleExamine(rest, runtime);
    case "set":
      return handleSet(rest, runtime);
    case "rpc": {
      // Escape hatch: rpc <method> <jsonParamsArray>
      if (rest.length < 1) {
        return "usage: rpc <method> [jsonParams]";
      }
      const method = rest[0];
      const paramsJson = rest.slice(1).join(" ").trim() || "[]";
      const params = JSON.parse(paramsJson);
      const result = await runtime.rawCall(method, Array.isArray(params) ? params : [params]);
      log(JSON.stringify(result, null, 2));
      return "ok";
    }
    default:
      return `unknown command "${verb}". try: help`;
  }
}

function tokenize(input: string): string[] {
  const out: string[] = [];
  const re = /"([^"\\]*(?:\\.[^"\\]*)*)"|'([^'\\]*(?:\\.[^'\\]*)*)'|(\S+)/g;
  let match: RegExpExecArray | null;
  while ((match = re.exec(input)) !== null) {
    out.push(match[1] ?? match[2] ?? match[3]);
  }
  return out;
}

async function handleBreak(args: string[], runtime: InspethctRuntime): Promise<string> {
  if (args.length === 0) {
    return "usage: break <line <file:line> | func <sig> | call <addr> | storage <addr> <slot> [r|w|rw] | memory <off> <size> [r|w|rw]>";
  }
  const sub = args[0].toLowerCase();
  const sessionId = runtime.currentSessionId;
  switch (sub) {
    case "line":
    case "src":
    case "source": {
      if (args.length < 2) {
        return "usage: break line <sourceName>:<line>[:<column>]";
      }
      const spec = args.slice(1).join(" ");
      const parts = spec.split(":");
      if (parts.length < 2) {
        return "expected <sourceName>:<line>";
      }
      const line = Number(parts[parts.length - (parts.length === 3 ? 2 : 1)]);
      const column = parts.length === 3 ? Number(parts[2]) : 1;
      const sourceName = parts.length === 3 ? parts.slice(0, parts.length - 2).join(":") : parts.slice(0, -1).join(":");
      const id = `bp-${sourceName}-${line}`;
      await runtime.rawCall("gdb.setSourceBreakpoint", [sessionId, { id, sourceName, line, column }]);
      return `set ${id}`;
    }
    case "func":
    case "function": {
      if (args.length < 2) {
        return "usage: break func <signature> [address]";
      }
      // Allow trailing 0x... address to scope the function BP to one contract.
      let signature = args.slice(1).join(" ");
      let address: string | undefined;
      const tokens = signature.split(/\s+/);
      if (tokens.length > 1 && /^0x[0-9a-fA-F]{40}$/.test(tokens[tokens.length - 1])) {
        address = tokens[tokens.length - 1];
        signature = tokens.slice(0, -1).join(" ");
      }
      const id = `bp-fn-${signature}${address ? `-${address}` : ""}`;
      const body: Record<string, unknown> = { id, signature };
      if (address) {
        body.address = address;
      }
      await runtime.rawCall("gdb.setFunctionBreakpoint", [sessionId, body]);
      return `set ${id}`;
    }
    case "call": {
      if (args.length < 2) {
        return "usage: break call <addr|signature>";
      }
      const target = args[1];
      const body: Record<string, unknown> = { id: `bp-call-${target}` };
      if (target.startsWith("0x") && target.length === 42) {
        body.address = target;
      } else {
        body.signature = target;
      }
      await runtime.rawCall("gdb.setCallBreakpoint", [sessionId, body]);
      return `set ${body.id}`;
    }
    case "storage": {
      if (args.length < 3) {
        return "usage: break storage <address> <slot> [r|w|rw]";
      }
      const access = args[3] ?? "rw";
      const id = `bp-storage-${args[1]}-${args[2]}-${access}`;
      await runtime.rawCall("gdb.setStorageBreakpoint", [sessionId, { id, address: args[1], slot: args[2], access }]);
      return `set ${id}`;
    }
    case "memory": {
      if (args.length < 3) {
        return "usage: break memory <offset> <size> [r|w|rw]";
      }
      const access = args[3] ?? "rw";
      const offset = parseUint(args[1]);
      const size = parseUint(args[2]);
      const id = `bp-memory-${offset}-${size}-${access}`;
      await runtime.rawCall("gdb.setMemoryBreakpoint", [sessionId, { id, offset, size, access }]);
      return `set ${id}`;
    }
    default:
      return `unknown break kind "${sub}". try: help break`;
  }
}

async function handleInfo(args: string[], runtime: InspethctRuntime): Promise<string> {
  if (args.length === 0) {
    return "usage: info <breakpoints|locals|stack|frames|memory|storage|transient|peek|state>";
  }
  const state = runtime.getState();
  const current = state?.current;
  switch (args[0].toLowerCase()) {
    case "b":
    case "breakpoints": {
      const bps = (state?.breakpoints ?? []) as Array<{ id: string }>;
      if (bps.length === 0) {
        return "no breakpoints";
      }
      return bps.map((b) => `  ${JSON.stringify(b)}`).join("\n");
    }
    case "locals": {
      const locals = current?.locals ?? [];
      if (locals.length === 0) {
        return "no locals in scope";
      }
      return locals.map(formatLocal).join("\n");
    }
    case "stack": {
      const stack = current?.stack ?? [];
      if (stack.length === 0) {
        return "stack empty";
      }
      return stack.map((value, index) => `  [${index}] ${value}`).join("\n");
    }
    case "frames":
    case "calls":
    case "callstack": {
      const frames = current?.callStack ?? [];
      if (frames.length === 0) {
        return "no call stack info yet";
      }
      return frames
        .map((f) => {
          const contract = f.contractName || f.codeAddress;
          const fn = f.functionSignature || f.functionName || f.selector || "";
          const fnTag = fn ? ` ${fn}` : "";
          const sourceTag = f.functionSource === "openchain" ? " (4byte)" : "";
          const header = `  #${f.depth} [${f.callType ?? "?"}] ${contract}${fnTag}${sourceTag}${f.inputSize ? `  input=${f.inputSize}B` : ""}`;
          const lines = [header];
          if (f.callerAddress) {
            lines.push(`        from=${f.callerAddress}${f.value ? ` value=${f.value}` : ""}`);
          } else if (f.value) {
            lines.push(`        value=${f.value}`);
          }
          if (f.arguments && f.arguments.length > 0) {
            for (const arg of f.arguments) {
              lines.push(`        ${arg.name}: ${arg.type} = ${arg.value}`);
            }
          } else if (f.argumentsError) {
            lines.push(`        arguments: <decode failed: ${f.argumentsError}>`);
          }
          return lines.join("\n");
        })
        .join("\n");
    }
    case "memory": {
      const size = current?.memorySize ?? 0;
      const free = current?.freeMemoryPointer ?? 0;
      const regions = current?.memoryRegions ?? [];
      const lines = [`size=${size} freeMemoryPointer=0x${free.toString(16)}`];
      for (const r of regions) {
        lines.push(`  ${r.label.padEnd(24)} [0x${r.offset.toString(16)} +${r.length}]`);
      }
      return lines.join("\n");
    }
    case "storage": {
      const entries = current?.storage ?? [];
      if (entries.length === 0) {
        return "no decoded storage";
      }
      return entries.map(formatStorage).join("\n");
    }
    case "transient": {
      const entries = current?.transient ?? [];
      if (entries.length === 0) {
        return "no decoded transient";
      }
      return entries.map(formatStorage).join("\n");
    }
    case "peek":
    case "variables":
    case "vars": {
      const peek = current?.peek;
      if (!peek) {
        return "no peek snapshot for current pause";
      }
      return formatPeekSnapshot(peek);
    }
    case "state":
      return formatStateLine(state);
    default:
      return `unknown info subject "${args[0]}". try: help info`;
  }
}

async function handlePrint(args: string[], runtime: InspethctRuntime): Promise<string> {
  if (args.length === 0) {
    return "usage: print <storage <name>|memory <off> <size>>";
  }
  const sub = args[0].toLowerCase();
  if (sub === "storage") {
    if (args.length < 2) {
      return "usage: print storage <name|slot>";
    }
    const key = args[1];
    const entries = runtime.getState()?.current?.storage ?? [];
    const found = entries.find((e) => e.name === key || e.slot === key);
    return found ? formatStorage(found) : `no storage variable matching "${key}"`;
  }
  if (sub === "memory" || sub === "mem") {
    if (args.length < 3) {
      return "usage: print memory <offset> <size>";
    }
    const offset = parseUint(args[1]);
    const size = parseUint(args[2]);
    const result = await runtime.readMemory(offset, size);
    return formatHexDump(offset, hexToBytes(result.data));
  }
  return `unknown print subject "${sub}"`;
}

async function handleExamine(args: string[], runtime: InspethctRuntime): Promise<string> {
  if (args.length < 2) {
    return "usage: x <offset> <size>";
  }
  const offset = parseUint(args[0]);
  const size = parseUint(args[1]);
  const result = await runtime.readMemory(offset, size);
  return formatHexDump(offset, hexToBytes(result.data));
}

async function handleSet(args: string[], runtime: InspethctRuntime): Promise<string> {
  if (args.length === 0) {
    return "usage: set <memory <off> <hex> | storage <addr> <slot> <value>>";
  }
  const sessionId = runtime.currentSessionId;
  switch (args[0].toLowerCase()) {
    case "memory":
    case "mem": {
      if (args.length < 3) {
        return "usage: set memory <offset> <hex>";
      }
      const offset = parseUint(args[1]);
      const data = args[2];
      await runtime.rawCall("gdb.writeMemory", [sessionId, { offset, data }]);
      return "ok";
    }
    case "storage": {
      if (args.length < 4) {
        return "usage: set storage <address> <slot> <value> [scope]";
      }
      const scope = args[4] ?? "storage";
      await runtime.rawCall("gdb.writeStorage", [sessionId, { address: args[1], slot: args[2], value: args[3], scope }]);
      return "ok";
    }
    default:
      return `unknown set subject "${args[0]}"`;
  }
}

function helpText(topic?: string): string {
  if (!topic) {
    return [
      "Commands:",
      "  c | continue                  resume to next breakpoint",
      "  n | s | step | next           step (source line if available, else instruction)",
      "  si | stepin                   step in (prefer entering called source frame)",
      "  so | stepout                  step out (run until caller frame)",
      "  state                         show current pause summary",
      "  b line <src>:<line>[:<col>]   set source breakpoint",
      "  b func <signature>            set function breakpoint",
      "  b call <addr|signature>       set call breakpoint",
      "  b storage <addr> <slot> [acc] set storage breakpoint",
      "  b memory <off> <size> [acc]   set memory breakpoint",
      "  info breakpoints|locals|stack|frames|memory|storage|transient|peek|state",
      "  print storage <name|slot>",
      "  print memory <off> <size>     hex dump (uses gdb.readMemory)",
      "  x <off> <size>                alias for print memory",
      "  set memory <off> <hex>",
      "  set storage <addr> <slot> <value> [scope]",
      "  rpc <method> [jsonParams]     raw JSON-RPC escape hatch",
      "  help [command]"
    ].join("\n");
  }
  return helpText();
}

function formatStateLine(state: GdbSessionState | undefined): string {
  if (!state) {
    return "no state";
  }
  if (state.done && !state.current) {
    return "session done";
  }
  const c = state.current;
  if (!c) {
    return "no current step";
  }
  const src = c.source ? ` ${c.source.sourceName ?? "?"}:${c.source.line ?? "?"}` : "";
  return `reason=${c.reason} step=${c.stepIndex} pc=${c.step?.pc ?? "?"} op=${c.step?.op ?? "?"}${src}`;
}

function formatLocal(local: LocalVariable): string {
  const value = local.value && local.value.length > 0 ? local.value : `<${local.confidence ?? "unavailable"}>`;
  const tags: string[] = [];
  if (local.confidence && local.confidence !== "unavailable") {
    tags.push(local.confidence);
  }
  if (local.stackIndex && local.stackIndex > 0) {
    tags.push(`stack[${local.stackIndex - 1}]`);
  }
  if (local.memoryPointer && local.memoryPointer > 0) {
    tags.push(`mem@0x${local.memoryPointer.toString(16)}`);
  }
  const tagStr = tags.length > 0 ? ` (${tags.join(",")})` : "";
  return `  ${local.kind.padEnd(9)} ${local.name.padEnd(24)} ${(local.type || "").padEnd(20)} ${value}${tagStr}${local.declaredAtLine ? `  // L${local.declaredAtLine}` : ""}`;
}

function formatStorage(entry: StorageVariable): string {
  return `  ${(entry.name || entry.slot).padEnd(28)} ${entry.slot}  ${entry.value}  ${entry.type ?? ""}`.trimEnd();
}

function formatPeekSnapshot(snap: PeekSnapshot): string {
  const lines: string[] = [];
  const header = [snap.contract, snap.function].filter(Boolean).join(".");
  if (header) {
    lines.push(`# ${header} @ pc=${snap.pc}`);
  }
  const sections: Array<[string, PeekVariable[] | undefined]> = [
    ["locals", snap.locals],
    ["storage", snap.storage],
    ["transient", snap.transient],
    ["immutables", snap.immutables],
  ];
  for (const [label, vars] of sections) {
    if (!vars || vars.length === 0) {
      continue;
    }
    lines.push(`${label}:`);
    for (const v of vars) {
      formatPeekVariable(v, 1, lines);
    }
  }
  if (snap.notes && snap.notes.length > 0) {
    lines.push(`notes: ${snap.notes.join("; ")}`);
  }
  return lines.length === 0 ? "peek snapshot empty" : lines.join("\n");
}

function formatPeekVariable(v: PeekVariable, depth: number, out: string[]): void {
  const indent = "  ".repeat(depth);
  const value = v.value && v.value.length > 0 ? v.value : `<${v.confidence ?? "unavailable"}>`;
  const tags: string[] = [];
  if (v.confidence) tags.push(v.confidence);
  if (v.location?.kind && v.location.kind !== "none") tags.push(v.location.kind);
  if (v.location?.slot) tags.push(`slot=${v.location.slot}`);
  if (typeof v.location?.offset === "number" && v.location.offset > 0) tags.push(`off=0x${v.location.offset.toString(16)}`);
  const tagStr = tags.length > 0 ? ` (${tags.join(",")})` : "";
  const noteStr = v.note ? `  // ${v.note}` : "";
  out.push(`${indent}${v.kind.padEnd(9)} ${v.name.padEnd(24)} ${(v.type || "").padEnd(20)} ${value}${tagStr}${noteStr}`);
  if (v.children && v.children.length > 0) {
    for (const child of v.children) {
      formatPeekVariable(child, depth + 1, out);
    }
  }
}

function formatHexDump(baseOffset: number, bytes: Uint8Array): string {
  const lines: string[] = [];
  for (let i = 0; i < bytes.length; i += 32) {
    const chunk = bytes.subarray(i, i + 32);
    let hex = "";
    for (let j = 0; j < chunk.length; j++) {
      hex += chunk[j].toString(16).padStart(2, "0");
      if (j === 15) {
        hex += " ";
      }
    }
    lines.push(`  0x${(baseOffset + i).toString(16).padStart(4, "0")}  ${hex}`);
  }
  if (lines.length === 0) {
    return "  <empty>";
  }
  return lines.join("\n");
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

function parseUint(value: string): number {
  if (value.startsWith("0x") || value.startsWith("0X")) {
    return Number.parseInt(value.slice(2), 16);
  }
  return Number.parseInt(value, 10);
}
