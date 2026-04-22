import { JsonRpcError } from "../types";

interface JsonRpcResponse<T> {
  jsonrpc: string;
  id: number;
  result?: T;
  error?: JsonRpcError;
}

export class RpcClient {
  private readonly endpoint: string;

  constructor(endpoint: string) {
    this.endpoint = endpoint;
  }

  async call<T>(method: string, params: unknown[] = [], timeoutMs = 30000): Promise<T> {
    const controller = new AbortController();
    const timeout = setTimeout(() => controller.abort(), timeoutMs);
    try {
      const response = await fetch(this.endpoint, {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ jsonrpc: "2.0", id: 1, method, params }),
        signal: controller.signal
      });
      if (!response.ok) {
        throw new Error(`RPC transport error ${response.status}`);
      }
      const json = (await response.json()) as JsonRpcResponse<T>;
      if (json.error) {
        throw new Error(`${json.error.code}: ${json.error.message}`);
      }
      return json.result as T;
    } finally {
      clearTimeout(timeout);
    }
  }
}
