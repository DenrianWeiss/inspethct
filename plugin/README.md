# Inspethct VS Code Plugin

This extension provides interactive Solidity debugging through inspethctd dbgserver.

## Core Features

- Start dbgserver automatically with upstream RPC from launch config.
- Clean up only extension-managed processes when debug session ends.
- Replay mode and synthetic call mode with custom sender address.
- Sequence mode with optional carry-over of user-applied mutations.
- Sequence script command for repeatable multi-call debug runs.
- Visual sequence builder panel for form-based multi-step debugging.
- Solidity function entry CodeLens for one-click debug launch.
- Source breakpoints synced from VS Code editor to gdb.setSourceBreakpoint.
- Source bundle selection prompt at session launch.

## Current Limitation (Sequence)

The current dbgserver API has no direct snapshot import/export method in gdb-only mode.
Sequence carry-over currently reuses user mutation journal (gdb.writeStorage/writeMemory),
which captures intentional debug edits, not all state diffs.
