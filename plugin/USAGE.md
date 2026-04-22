# Inspethct Plugin Usage

## 1. Build

- Run npm install
- Run npm run compile

## 2. Debug Config

Use type inspethct in launch.json.

Example replay config:

{
  "type": "inspethct",
  "request": "launch",
  "name": "Replay tx",
  "sessionType": "replay",
  "upstream": "http://127.0.0.1:8545",
  "txHash": "0x...",
  "autoStartDbgserver": true,
  "promptSourceBundle": true
}

Example call config:

{
  "type": "inspethct",
  "request": "launch",
  "name": "Call debug",
  "sessionType": "call",
  "upstream": "http://127.0.0.1:8545",
  "call": {
    "from": "0x0000000000000000000000000000000000000001",
    "to": "0x...",
    "input": "0x...",
    "value": "0x0",
    "gas": "0x0",
    "block": "latest"
  },
  "autoStartDbgserver": true,
  "promptSourceBundle": true
}

## 3. Sequence Mode

Use sessionType sequence with sequence array. Each item is equivalent to one call object.

Sequence carry-over behavior:

- If backend advertises `features.sequenceSession=true`, plugin uses native sequence session with backend patch carry.
- Otherwise plugin falls back to mutation-journal carry (`gdb.writeStorage`/`gdb.writeMemory`).

Visual mode:

- Run command Inspethct: Open Sequence Builder.
- Fill upstream and each step fields in the form.
- For each step, click From ABI to auto-scan Foundry/Hardhat artifacts under out/ and artifacts/.
- Pick contract artifact and function, then fill parameter form fields; calldata is auto-generated into input.
- Use Up/Down buttons to reorder sequence steps visually.
- Use Copy button to duplicate a step quickly.
- Builder validates address and hex inputs in real time and prevents invalid launch.
- bool ABI parameters are rendered as true/false dropdowns.
- Click Start Debugging to launch sequence mode directly.
- Use Export JSON for script reuse, Import JSON to load an existing script.

You can also run command Inspethct: Run Sequence Script and choose a JSON script file.
Example script:

{
  "name": "ERC20 staged debug",
  "upstream": "http://127.0.0.1:8545",
  "block": "latest",
  "fork": "cancun",
  "forkMode": "diff",
  "carryUserMutations": true,
  "promptSourceBundle": false,
  "sequence": [
    {
      "label": "approve",
      "from": "0x0000000000000000000000000000000000000001",
      "to": "0x0000000000000000000000000000000000000010",
      "input": "0x095ea7b30000000000000000000000000000000000000000000000000000000000000000",
      "value": "0x0",
      "gas": "0x0",
      "block": "latest"
    },
    {
      "label": "transferFrom",
      "from": "0x0000000000000000000000000000000000000002",
      "to": "0x0000000000000000000000000000000000000010",
      "input": "0x23b872dd0000000000000000000000000000000000000000000000000000000000000000",
      "value": "0x0",
      "gas": "0x0",
      "block": "latest"
    }
  ]
}

## 4. Solidity Entry Debug

Open Solidity file and click Debug with Inspethct above a function.
The composer supports tuple/array via JSON input.

## 5. Binary Resolution

- Extension first checks inspethct.binaryPath.
- If empty, it searches PATH for inspethctd.
- If still missing, UI prompts for manual path or download guide.

## 6. Process Cleanup

Only dbgserver processes started by extension are cleaned up on session end.
