import * as vscode from "vscode";
import { randomBytes } from "crypto";
import { InspethctLaunchConfig, SequenceScript, SequenceStep } from "../types";
import { encodeFunctionInput, functionInputsForSignature, scanWorkspaceAbiCatalog } from "../services/abiCatalog";

interface BuilderStartMessage {
  type: "start";
  payload: SequenceScript;
}

interface BuilderExportMessage {
  type: "export";
  payload: SequenceScript;
}

interface BuilderApplyAbiMessage {
  type: "applyAbi";
  stepIndex: number;
}

interface BuilderEncodeAbiMessage {
  type: "encodeAbi";
  stepIndex: number;
  contextId: string;
  argsJson: string;
}

type BuilderMessage = BuilderStartMessage | BuilderExportMessage | BuilderApplyAbiMessage | BuilderEncodeAbiMessage;

interface BuilderSetInputMessage {
  type: "setInput";
  stepIndex: number;
  input: string;
}

interface BuilderAbiTemplateMessage {
  type: "abiTemplate";
  stepIndex: number;
  contextId: string;
  functionDisplay: string;
  params: Array<{ name: string; type: string }>;
}

interface BuilderAbiErrorMessage {
  type: "abiError";
  stepIndex: number;
  message: string;
}

type BuilderOutboundMessage = BuilderSetInputMessage | BuilderAbiTemplateMessage | BuilderAbiErrorMessage;

interface AbiContext {
  abi: string;
  signature: string;
}

export class SequenceBuilderPanel {
  static readonly viewType = "inspethct.sequenceBuilder";
  private static currentPanel: vscode.WebviewPanel | undefined;
  private readonly abiContexts = new Map<string, AbiContext>();

  static open(
    context: vscode.ExtensionContext,
    defaults: { upstream: string; block: string; fork: string; forkMode: string },
    onStart: (config: InspethctLaunchConfig) => Promise<void>
  ): void {
    if (SequenceBuilderPanel.currentPanel) {
      SequenceBuilderPanel.currentPanel.reveal(vscode.ViewColumn.Active);
      return;
    }

    const panel = vscode.window.createWebviewPanel(
      SequenceBuilderPanel.viewType,
      "Inspethct Sequence Builder",
      vscode.ViewColumn.Active,
      {
        enableScripts: true,
        retainContextWhenHidden: true
      }
    );

    SequenceBuilderPanel.currentPanel = panel;
    panel.onDidDispose(() => {
      SequenceBuilderPanel.currentPanel = undefined;
    });

    const instance = new SequenceBuilderPanel(panel, context, defaults, onStart);
    instance.render();
  }

  private constructor(
    private readonly panel: vscode.WebviewPanel,
    private readonly context: vscode.ExtensionContext,
    private readonly defaults: { upstream: string; block: string; fork: string; forkMode: string },
    private readonly onStart: (config: InspethctLaunchConfig) => Promise<void>
  ) {
    this.panel.webview.onDidReceiveMessage(async (raw: BuilderMessage) => {
      await this.handleMessage(raw);
    });
  }

  private render(): void {
    this.panel.webview.html = this.html(this.panel.webview, this.defaults);
  }

  private async handleMessage(message: BuilderMessage): Promise<void> {
    if (message.type === "start") {
      const script = sanitizeScript(message.payload);
      if (script.sequence.length === 0) {
        void vscode.window.showErrorMessage("Sequence cannot be empty.");
        return;
      }
      const folder = vscode.workspace.workspaceFolders?.[0];
      const launch: InspethctLaunchConfig = {
        type: "inspethct",
        request: "launch",
        name: script.name || "Inspethct Visual Sequence",
        sessionType: "sequence",
        upstream: script.upstream || this.defaults.upstream,
        block: script.block || this.defaults.block,
        fork: script.fork || this.defaults.fork,
        forkMode: script.forkMode || this.defaults.forkMode,
        sequence: script.sequence,
        sourceBundle: script.sourceBundle,
        autoStartDbgserver: true,
        promptSourceBundle: script.promptSourceBundle ?? false,
        carryUserMutations: script.carryUserMutations ?? true
      };
      await this.onStart(launch);
      return;
    }

    if (message.type === "export") {
      const script = sanitizeScript(message.payload);
      const uri = await vscode.window.showSaveDialog({
        title: "Export sequence script",
        defaultUri: vscode.Uri.joinPath(this.context.globalStorageUri, "sequence.script.json"),
        filters: { JSON: ["json"] }
      });
      if (!uri) {
        return;
      }
      const content = JSON.stringify(script, null, 2);
      await vscode.workspace.fs.writeFile(uri, Buffer.from(content, "utf8"));
      void vscode.window.showInformationMessage(`Sequence script exported to ${uri.fsPath}`);
      return;
    }

    if (message.type === "applyAbi") {
      await this.applyAbiToStep(message.stepIndex);
      return;
    }

    if (message.type === "encodeAbi") {
      await this.encodeAbiForStep(message);
      return;
    }
  }

  private async applyAbiToStep(stepIndex: number): Promise<void> {
    const folder = vscode.workspace.workspaceFolders?.[0];
    if (!folder) {
      void vscode.window.showErrorMessage("No workspace folder found.");
      return;
    }

    const artifacts = await scanWorkspaceAbiCatalog(folder);
    if (artifacts.length === 0) {
      void vscode.window.showWarningMessage("No ABI artifacts found under out/ or artifacts/.");
      return;
    }

    const pickedArtifact = await vscode.window.showQuickPick(
      artifacts.map((item) => ({ label: item.label, description: item.sourcePath, artifact: item })),
      { title: "Select ABI artifact" }
    );
    if (!pickedArtifact) {
      return;
    }

    const artifact = pickedArtifact.artifact;
    const pickedFunction = await vscode.window.showQuickPick(
      artifact.functions.map((fn) => ({ label: fn.signature, description: fn.display, signature: fn.signature })),
      { title: "Select function" }
    );
    if (!pickedFunction) {
      return;
    }

    try {
      const contextId = `abi-${Date.now()}-${Math.floor(Math.random() * 100000)}`;
      this.abiContexts.set(contextId, { abi: artifact.abi, signature: pickedFunction.signature });
      const params = functionInputsForSignature(artifact.abi, pickedFunction.signature);
      const payload: BuilderAbiTemplateMessage = {
        type: "abiTemplate",
        stepIndex,
        contextId,
        functionDisplay: pickedFunction.signature,
        params
      };
      this.panel.webview.postMessage(payload satisfies BuilderOutboundMessage);
    } catch (error) {
      void vscode.window.showErrorMessage(`Failed to encode ABI call: ${String(error)}`);
    }
  }

  private async encodeAbiForStep(message: BuilderEncodeAbiMessage): Promise<void> {
    const context = this.abiContexts.get(message.contextId);
    if (!context) {
      const payload: BuilderAbiErrorMessage = {
        type: "abiError",
        stepIndex: message.stepIndex,
        message: "ABI context expired, please select function again."
      };
      this.panel.webview.postMessage(payload satisfies BuilderOutboundMessage);
      return;
    }

    try {
      const input = encodeFunctionInput(context.abi, context.signature, message.argsJson);
      const payload: BuilderSetInputMessage = { type: "setInput", stepIndex: message.stepIndex, input };
      this.panel.webview.postMessage(payload satisfies BuilderOutboundMessage);
    } catch (error) {
      const payload: BuilderAbiErrorMessage = {
        type: "abiError",
        stepIndex: message.stepIndex,
        message: String(error)
      };
      this.panel.webview.postMessage(payload satisfies BuilderOutboundMessage);
    }
  }

  private html(webview: vscode.Webview, defaults: { upstream: string; block: string; fork: string; forkMode: string }): string {
    const nonce = randomBytes(16).toString("hex");
    const initial = {
      name: "Visual Sequence",
      upstream: defaults.upstream,
      block: defaults.block,
      fork: defaults.fork,
      forkMode: defaults.forkMode,
      carryUserMutations: true,
      promptSourceBundle: false,
      sequence: [
        {
          label: "step-1",
          from: "0x0000000000000000000000000000000000000001",
          to: "",
          input: "0x",
          value: "0x0",
          gas: "0x0",
          block: defaults.block
        }
      ]
    };

    return `<!doctype html>
<html lang="en">
<head>
  <meta charset="UTF-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1.0" />
  <meta http-equiv="Content-Security-Policy" content="default-src 'none'; style-src 'unsafe-inline'; script-src 'nonce-${nonce}';" />
  <style>
    :root {
      --bg: var(--vscode-editor-background, #111822);
      --card: var(--vscode-editorWidget-background, #192434);
      --line: var(--vscode-widget-border, #33475f);
      --text: var(--vscode-editor-foreground, #e7eef8);
      --muted: var(--vscode-descriptionForeground, #9fb3ca);
      --accent: var(--vscode-button-background, #3fb17e);
      --accent-2: var(--vscode-button-secondaryBackground, #4d9fff);
      --danger: var(--vscode-errorForeground, #dc5b6f);
    }
    body {
      margin: 0;
      font-family: ui-sans-serif, -apple-system, BlinkMacSystemFont, Segoe UI, sans-serif;
      background: var(--bg);
      color: var(--text);
    }
    .container {
      max-width: 1200px;
      margin: 20px auto;
      padding: 0 18px 32px;
    }
    .header {
      display: flex;
      justify-content: space-between;
      align-items: center;
      gap: 12px;
      margin-bottom: 16px;
    }
    h1 {
      margin: 0;
      font-size: 20px;
      letter-spacing: 0.3px;
    }
    .meta {
      display: grid;
      grid-template-columns: repeat(4, minmax(160px, 1fr));
      gap: 10px;
      margin-bottom: 14px;
    }
    .card {
      border: 1px solid var(--line);
      background: color-mix(in srgb, var(--card) 92%, #000 8%);
      border-radius: 12px;
      padding: 12px;
      box-shadow: 0 8px 30px rgb(0 0 0 / 18%);
    }
    .row {
      display: grid;
      grid-template-columns: repeat(7, minmax(120px, 1fr)) auto;
      gap: 8px;
      margin-bottom: 8px;
      align-items: end;
    }
    label {
      display: block;
      font-size: 12px;
      color: var(--muted);
      margin-bottom: 4px;
    }
    input, select {
      width: 100%;
      box-sizing: border-box;
      border: 1px solid var(--vscode-input-border, var(--line));
      background: var(--vscode-input-background, #0f1824);
      color: var(--vscode-input-foreground, var(--text));
      border-radius: 8px;
      padding: 8px;
      font-size: 12px;
    }
    .toolbar {
      display: flex;
      justify-content: space-between;
      margin: 12px 0;
      gap: 8px;
      flex-wrap: wrap;
    }
    .status {
      margin: 8px 0 14px;
      font-size: 12px;
      color: var(--muted);
    }
    .status.error {
      color: #ff9aa9;
    }
    .step-title {
      display: flex;
      justify-content: space-between;
      align-items: center;
      gap: 10px;
      margin-bottom: 8px;
    }
    .chip {
      border: 1px solid var(--line);
      border-radius: 999px;
      padding: 2px 8px;
      font-size: 11px;
      color: var(--muted);
    }
    .chip.bad {
      border-color: #915464;
      color: #ff9aa9;
      background: #2a1a20;
    }
    button {
      border: 1px solid transparent;
      border-radius: 999px;
      padding: 8px 14px;
      color: #fff;
      font-size: 12px;
      cursor: pointer;
      transition: 120ms ease;
    }
    .btn-primary { background: var(--accent); }
    .btn-secondary { background: var(--accent-2); }
    .btn-danger { background: var(--danger); }
    .btn-ghost {
      background: transparent;
      border-color: var(--line);
      color: var(--text);
    }
    button:hover { transform: translateY(-1px); filter: brightness(1.07); }
    .muted { color: var(--muted); font-size: 12px; }
    .small-grid {
      display: grid;
      grid-template-columns: 1fr 1fr;
      gap: 8px;
      margin-top: 8px;
    }
    @media (max-width: 980px) {
      .meta { grid-template-columns: 1fr 1fr; }
      .row { grid-template-columns: 1fr 1fr; }
    }
  </style>
</head>
<body>
  <div class="container">
    <div class="header">
      <h1>Inspethct Sequence Builder</h1>
      <div class="muted">Visual compose for multi-step transaction debugging</div>
    </div>

    <div class="card">
      <div class="meta">
        <div>
          <label>Sequence Name</label>
          <input id="name" value="${escapeHtml(initial.name)}" />
        </div>
        <div>
          <label>Upstream RPC</label>
          <input id="upstream" value="${escapeHtml(initial.upstream)}" />
        </div>
        <div>
          <label>Block</label>
          <input id="block" value="${escapeHtml(initial.block)}" />
        </div>
        <div>
          <label>Fork</label>
          <input id="fork" value="${escapeHtml(initial.fork)}" />
        </div>
      </div>
      <div class="small-grid">
        <div>
          <label>Fork Mode</label>
          <select id="forkMode">
            <option value="diff" ${initial.forkMode === "diff" ? "selected" : ""}>diff</option>
            <option value="pinned" ${initial.forkMode === "pinned" ? "selected" : ""}>pinned</option>
          </select>
        </div>
        <div>
          <label>Carry User Mutations</label>
          <select id="carry">
            <option value="true" selected>true</option>
            <option value="false">false</option>
          </select>
        </div>
      </div>
    </div>

    <div class="toolbar">
      <button class="btn-secondary" id="add">Add Step</button>
      <button class="btn-ghost" id="import">Import JSON</button>
      <button class="btn-ghost" id="export">Export JSON</button>
      <button class="btn-primary" id="start">Start Debugging</button>
    </div>

    <div id="status" class="status"></div>

    <div id="steps"></div>
  </div>

  <script nonce="${nonce}">
    const vscode = acquireVsCodeApi();
    const state = ${JSON.stringify(initial)};

    const stepsEl = document.getElementById('steps');
    const nameEl = document.getElementById('name');
    const upstreamEl = document.getElementById('upstream');
    const blockEl = document.getElementById('block');
    const forkEl = document.getElementById('fork');
    const forkModeEl = document.getElementById('forkMode');
    const carryEl = document.getElementById('carry');
    const statusEl = document.getElementById('status');
    const startBtn = document.getElementById('start');

    function renderSteps() {
      stepsEl.innerHTML = '';
      state.sequence.forEach((step, index) => {
        const card = document.createElement('div');
        card.className = 'card';
        const issues = validateStep(step, index);
        card.innerHTML = [
          '<div class="step-title"><div class="muted">Step ' + (index + 1) + '</div>' +
            (issues.length > 0 ? '<div class="chip bad">' + escapeHtml(issues.join('; ')) + '</div>' : '<div class="chip">ready</div>') +
          '</div>',
          '<div class="row">',
          field('Label', 'label', step.label || ''),
          field('From', 'from', step.from || ''),
          field('To', 'to', step.to || ''),
          field('Input', 'input', step.input || '0x'),
          field('Value', 'value', step.value || '0x0'),
          field('Gas', 'gas', step.gas || '0x0'),
          field('Block', 'block', step.block || state.block || 'latest'),
          '<button class="btn-ghost" data-up="' + index + '">Up</button>',
          '<button class="btn-ghost" data-down="' + index + '">Down</button>',
          '<button class="btn-ghost" data-copy="' + index + '">Copy</button>',
          '<button class="btn-secondary" data-abi="' + index + '">From ABI</button>',
          '<button class="btn-danger" data-remove="' + index + '">Delete</button>',
          '</div>'
        ].join('');

        const abiEditor = step.__abiEditor;
        if (abiEditor) {
          const panel = document.createElement('div');
          panel.className = 'card';
          panel.style.marginTop = '8px';
          panel.innerHTML = [
            '<div class="muted" style="margin-bottom:8px;">ABI Parameters: ' + escapeHtml(abiEditor.functionDisplay) + '</div>',
            '<div class="small-grid">',
            ...abiEditor.params.map((param, idx) => renderAbiParamField(param, idx, abiEditor.values[idx] || '')),
            '</div>',
            '<div class="toolbar" style="margin-top:10px;">',
            '<button class="btn-primary" data-encode="' + index + '">Encode To Input</button>',
            '<button class="btn-ghost" data-close-abi="' + index + '">Close</button>',
            '</div>'
          ].join('');

          panel.querySelectorAll('[data-abi-arg]').forEach((input) => {
            input.addEventListener('input', (ev) => {
              const idx = Number(ev.target.getAttribute('data-abi-arg'));
              abiEditor.values[idx] = ev.target.value;
            });
          });
          panel.querySelectorAll('[data-abi-bool]').forEach((select) => {
            select.addEventListener('change', (ev) => {
              const idx = Number(ev.target.getAttribute('data-abi-bool'));
              abiEditor.values[idx] = ev.target.value;
            });
          });

          const encodeBtn = panel.querySelector('[data-encode]');
          encodeBtn.addEventListener('click', () => {
            const args = abiEditor.values.map((item) => normalizeAbiArg(item));
            vscode.postMessage({
              type: 'encodeAbi',
              stepIndex: index,
              contextId: abiEditor.contextId,
              argsJson: JSON.stringify(args)
            });
          });

          const closeBtn = panel.querySelector('[data-close-abi]');
          closeBtn.addEventListener('click', () => {
            delete step.__abiEditor;
            renderSteps();
          });

          card.appendChild(panel);
        }

        card.querySelectorAll('input').forEach((input) => {
          input.addEventListener('input', (ev) => {
            const key = ev.target.getAttribute('data-key');
            state.sequence[index][key] = ev.target.value;
          });
        });
        const remove = card.querySelector('[data-remove]');
        remove.addEventListener('click', () => {
          state.sequence.splice(index, 1);
          renderSteps();
        });
        const abiBtn = card.querySelector('[data-abi]');
        abiBtn.addEventListener('click', () => {
          vscode.postMessage({ type: 'applyAbi', stepIndex: index });
        });
        const upBtn = card.querySelector('[data-up]');
        upBtn.addEventListener('click', () => {
          if (index === 0) {
            return;
          }
          const tmp = state.sequence[index - 1];
          state.sequence[index - 1] = state.sequence[index];
          state.sequence[index] = tmp;
          renderSteps();
        });
        const downBtn = card.querySelector('[data-down]');
        downBtn.addEventListener('click', () => {
          if (index >= state.sequence.length - 1) {
            return;
          }
          const tmp = state.sequence[index + 1];
          state.sequence[index + 1] = state.sequence[index];
          state.sequence[index] = tmp;
          renderSteps();
        });
        const copyBtn = card.querySelector('[data-copy]');
        copyBtn.addEventListener('click', () => {
          const cloned = JSON.parse(JSON.stringify(state.sequence[index]));
          cloned.label = (cloned.label || 'step-' + (index + 1)) + '-copy';
          state.sequence.splice(index + 1, 0, cloned);
          renderSteps();
        });
        stepsEl.appendChild(card);
      });
      refreshStatus();
    }

    function field(label, key, value) {
      return '<div><label>' + label + '</label><input data-key="' + key + '" value="' + escapeHtml(value) + '" /></div>';
    }

    function escapeHtml(text) {
      return String(text)
        .replaceAll('&', '&amp;')
        .replaceAll('<', '&lt;')
        .replaceAll('>', '&gt;')
        .replaceAll('"', '&quot;')
        .replaceAll("'", '&#39;');
    }

    function syncMeta() {
      state.name = nameEl.value;
      state.upstream = upstreamEl.value;
      state.block = blockEl.value;
      state.fork = forkEl.value;
      state.forkMode = forkModeEl.value;
      state.carryUserMutations = carryEl.value === 'true';
      state.promptSourceBundle = false;
      refreshStatus();
    }

    [nameEl, upstreamEl, blockEl, forkEl, forkModeEl, carryEl].forEach((el) => {
      el.addEventListener('input', syncMeta);
      el.addEventListener('change', syncMeta);
    });

    document.getElementById('add').addEventListener('click', () => {
      state.sequence.push({
        label: 'step-' + (state.sequence.length + 1),
        from: '0x0000000000000000000000000000000000000001',
        to: '',
        input: '0x',
        value: '0x0',
        gas: '0x0',
        block: state.block || 'latest'
      });
      renderSteps();
    });

    document.getElementById('start').addEventListener('click', () => {
      syncMeta();
      const errors = validateScript(state);
      if (errors.length > 0) {
        alert('Cannot start debugging:\n' + errors.join('\n'));
        return;
      }
      vscode.postMessage({ type: 'start', payload: state });
    });

    document.getElementById('export').addEventListener('click', () => {
      syncMeta();
      vscode.postMessage({ type: 'export', payload: state });
    });

    document.getElementById('import').addEventListener('click', async () => {
      const raw = prompt('Paste sequence JSON');
      if (!raw) {
        return;
      }
      try {
        const parsed = JSON.parse(raw);
        if (!Array.isArray(parsed.sequence)) {
          throw new Error('Missing sequence array');
        }
        Object.assign(state, parsed);
        nameEl.value = state.name || '';
        upstreamEl.value = state.upstream || '';
        blockEl.value = state.block || 'latest';
        forkEl.value = state.fork || 'cancun';
        forkModeEl.value = state.forkMode || 'diff';
        carryEl.value = String(state.carryUserMutations !== false);
        renderSteps();
      } catch (error) {
        alert('Import failed: ' + String(error));
      }
    });

    window.addEventListener('message', (event) => {
      const message = event.data;
      if (!message || !message.type) {
        return;
      }
      if (message.type === 'setInput') {
        const step = state.sequence[message.stepIndex];
        if (!step) {
          return;
        }
        step.input = message.input;
        if (step.__abiEditor) {
          delete step.__abiEditor;
        }
        renderSteps();
        return;
      }
      if (message.type === 'abiTemplate') {
        const step = state.sequence[message.stepIndex];
        if (!step) {
          return;
        }
        step.__abiEditor = {
          contextId: message.contextId,
          functionDisplay: message.functionDisplay,
          params: message.params,
          values: message.params.map(() => '')
        };
        renderSteps();
        return;
      }
      if (message.type === 'abiError') {
        alert('ABI encode failed: ' + message.message);
      }
    });

    syncMeta();
    renderSteps();

    function validateAddress(value) {
      return /^0x[0-9a-fA-F]{40}$/.test(String(value || '').trim());
    }

    function validateHexLike(value) {
      return /^0x[0-9a-fA-F]*$/.test(String(value || '').trim());
    }

    function validateStep(step, index) {
      const issues = [];
      if (!validateAddress(step.from)) {
        issues.push('invalid from');
      }
      if (!validateAddress(step.to)) {
        issues.push('invalid to');
      }
      if (!validateHexLike(step.input || '0x')) {
        issues.push('input must be hex');
      }
      if (!validateHexLike(step.value || '0x0')) {
        issues.push('value must be hex');
      }
      if (!validateHexLike(step.gas || '0x0')) {
        issues.push('gas must be hex');
      }
      if (!step.block || String(step.block).trim().length === 0) {
        issues.push('block required');
      }
      if (!step.label || String(step.label).trim().length === 0) {
        issues.push('label recommended');
      }
      return issues;
    }

    function validateScript(script) {
      const errors = [];
      if (!script.upstream || String(script.upstream).trim().length === 0) {
        errors.push('upstream RPC is required');
      }
      if (!Array.isArray(script.sequence) || script.sequence.length === 0) {
        errors.push('at least one step is required');
        return errors;
      }
      script.sequence.forEach((step, index) => {
        const issues = validateStep(step, index).filter((item) => item !== 'label recommended');
        issues.forEach((issue) => errors.push('step ' + (index + 1) + ': ' + issue));
      });
      return errors;
    }

    function refreshStatus() {
      const errors = validateScript(state);
      startBtn.disabled = false;
      startBtn.style.opacity = '1';
      if (errors.length === 0) {
        statusEl.className = 'status';
        statusEl.textContent = 'Ready to start. ' + state.sequence.length + ' step(s) configured.';
      } else {
        statusEl.className = 'status error';
        statusEl.textContent = errors[0] + (errors.length > 1 ? ' (+' + (errors.length - 1) + ' more)' : '');
      }
    }

    function normalizeAbiArg(raw) {
      const text = String(raw || '').trim();
      if (text.length === 0) {
        return '';
      }
      if (text === 'true') {
        return true;
      }
      if (text === 'false') {
        return false;
      }
      if (text.startsWith('{') || text.startsWith('[') || text.startsWith('"')) {
        try {
          return JSON.parse(text);
        } catch {
          return text;
        }
      }
      return text;
    }

    function renderAbiParamField(param, idx, value) {
      const typeText = String(param.type || '').toLowerCase();
      if (typeText.includes('bool')) {
        const current = String(value || 'false');
        return '<div><label>' + escapeHtml(param.name + ' - ' + param.type) + '</label>' +
          '<select data-abi-bool="' + idx + '">' +
            '<option value="false" ' + (current === 'false' ? 'selected' : '') + '>false</option>' +
            '<option value="true" ' + (current === 'true' ? 'selected' : '') + '>true</option>' +
          '</select></div>';
      }
      return '<div><label>' + escapeHtml(param.name + ' - ' + param.type) + '</label><input data-abi-arg="' + idx + '" value="' + escapeHtml(value) + '" placeholder="JSON value or plain string" /></div>';
    }
  </script>
</body>
</html>`;
  }
}

function sanitizeScript(input: SequenceScript): SequenceScript {
  const sequence = (input.sequence || [])
    .map((item) => sanitizeStep(item))
    .filter((item) => item.to.length > 0);

  return {
    name: (input.name || "").trim(),
    upstream: (input.upstream || "").trim(),
    block: (input.block || "latest").trim(),
    fork: (input.fork || "cancun").trim(),
    forkMode: (input.forkMode || "diff").trim(),
    carryUserMutations: input.carryUserMutations ?? true,
    promptSourceBundle: input.promptSourceBundle ?? false,
    sourceBundle: input.sourceBundle,
    sequence
  };
}

function sanitizeStep(step: SequenceStep): SequenceStep {
  return {
    label: (step.label || "").trim(),
    from: (step.from || "").trim(),
    to: (step.to || "").trim(),
    input: (step.input || "0x").trim(),
    value: (step.value || "0x0").trim(),
    gas: (step.gas || "0x0").trim(),
    block: (step.block || "latest").trim()
  };
}

function escapeHtml(value: string): string {
  return value
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;")
    .replaceAll("'", "&#39;");
}
