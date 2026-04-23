export type SessionType = "replay" | "call" | "sequence";

export interface SourceBundleConfig {
  kind: "auto" | "local-project" | "standard-json" | "manual" | "explorer";
  contractName: string;
  sourceName?: string;
  runtime?: boolean;
  projectRoot?: string;
  standardJsonPath?: string;
  abiPath?: string;
  address?: string;
  codeAddress?: string;
  apiBase?: string;
  apiKey?: string;
  rpcUrl?: string;
  chainId?: string;
}

export interface LaunchCallConfig {
  from: string;
  to: string;
  input: string;
  value?: string;
  gas?: string;
  block?: string;
}

export interface SequenceStep extends LaunchCallConfig {
  label?: string;
}

export interface SequenceScript {
  name?: string;
  upstream?: string;
  block?: string;
  fork?: string;
  forkMode?: string;
  carryUserMutations?: boolean;
  promptSourceBundle?: boolean;
  sourceBundle?: SourceBundleConfig;
  sequence: SequenceStep[];
}

export interface InspethctLaunchConfig {
  type: "inspethct";
  request: "launch";
  name: string;
  upstream: string;
  dbgserverUrl?: string;
  listenHost?: string;
  listenPort?: number;
  stopOnEntry?: boolean;
  block?: string;
  fork?: string;
  forkMode?: string;
  autoStartDbgserver?: boolean;
  dbgserverBinaryPath?: string;
  sessionType?: SessionType;
  txHash?: string;
  call?: LaunchCallConfig;
  sequence?: SequenceStep[];
  sourceBundle?: SourceBundleConfig;
  promptSourceBundle?: boolean;
  autoSourceBundle?: boolean;
  explorerFallbackEnabled?: boolean;
  carryUserMutations?: boolean;
}

export interface JsonRpcError {
  code: number;
  message: string;
}
