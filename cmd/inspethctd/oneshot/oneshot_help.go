package oneshot

import (
	"flag"
	"fmt"
	"io"
	"strings"

	"inspethct/internal/clihelp"
)

func isHelpToken(token string) bool {
	switch strings.ToLower(strings.TrimSpace(token)) {
	case "help", "h", "?":
		return true
	default:
		return false
	}
}

func canonicalHelpTopic(token string) string {
	switch strings.ToLower(strings.TrimSpace(token)) {
	case "r", "run":
		return "run"
	case "c", "continue":
		return "continue"
	case "n", "s", "step", "next":
		return "next"
	case "b", "break":
		return "break"
	case "i", "info":
		return "info"
	case "p", "print":
		return "print"
	case "x":
		return "x"
	case "set":
		return "set"
	case "load":
		return "load"
	case "encode":
		return "encode"
	case "state":
		return "state"
	case "restart":
		return "restart"
	case "q", "quit", "exit":
		return "quit"
	case "line":
		return "line"
	case "function", "func":
		return "function"
	case "call":
		return "call"
	case "storage":
		return "storage"
	case "memory":
		return "memory"
	case "breakpoints", "bp":
		return "breakpoints"
	case "config":
		return "config"
	case "bundle":
		return "bundle"
	case "local":
		return "local"
	case "explorer":
		return "explorer"
	case "clear":
		return "clear"
	case "last":
		return "last"
	default:
		return strings.ToLower(strings.TrimSpace(token))
	}
}

func sourceBundleLoadHint() string {
	return "load local <contract> [source] or load explorer <address> [contract] [source]"
}

func noSourceBundleMessage() string {
	return strings.Join([]string{
		"No source bundle loaded.",
		"",
		"Load source:",
		"  load local <contract> [source]",
		"  load explorer <address> [contract] [source]",
		"",
		"Still available without source:",
		"  break function <signature>",
		"  break call <address> [signature]",
		"  break storage <slot|name> [read|write|rw]",
		"  break memory <offset> [size] [read|write|rw]",
	}, "\n")
}

func sourceBundleRequiredMessage(feature string) string {
	return strings.Join([]string{
		fmt.Sprintf("%s require a loaded source bundle.", feature),
		"",
		"Load source:",
		"  load local <contract> [source]",
		"  load explorer <address> [contract] [source]",
		"",
		"Still available without source:",
		"  break function <signature>",
		"  break call <address> [signature]",
		"  break storage <slot|name> [read|write|rw]",
		"  break memory <offset> [size] [read|write|rw]",
	}, "\n")
}

func printOneShotCLIHelp(writer io.Writer, flags *flag.FlagSet) {
	clihelp.Render(writer, clihelp.Doc{
		Usage: "inspethctd oneshot [flags]",
		Summary: []string{
			"Starts an interactive single-session debugger for replaying a transaction or simulating a call.",
		},
		Sections: []clihelp.Section{
			{
				Title: "Interactive Commands",
				Entries: []clihelp.Entry{
					{Label: "run, r", Summary: "Start execution from the beginning."},
					{Label: "continue, c", Summary: "Resume until completion or the next breakpoint."},
					{Label: "next, n, step, s", Summary: "Advance one execution step."},
					{Label: "break, info, print", Summary: "Manage breakpoints and inspect the paused state."},
					{Label: "x, set, load", Summary: "Examine memory, update session state, and load metadata."},
					{Label: "encode, state", Summary: "Encode calldata and print the current execution status."},
					{Label: "restart, quit", Summary: "Reset execution progress or exit the REPL."},
				},
			},
			{
				Title: "Help",
				Entries: []clihelp.Entry{
					{Label: "help <command>", Summary: "Show help for a command."},
					{Label: "<command> help", Summary: "Alternative help form."},
					{Label: "<command> <subcommand> help", Summary: "Show second-level help such as break storage help."},
				},
			},
			{
				Title: "Breakpoints Without Source",
				Entries: []clihelp.Entry{
					{Label: "break function", Summary: "Break when the root or nested call matches a selector."},
					{Label: "break call", Summary: "Break when execution enters a target address, optionally filtered by selector."},
					{Label: "break storage", Summary: "Break on slot access. Use concrete slots when no metadata is loaded."},
					{Label: "break memory", Summary: "Break on offset-based memory access."},
				},
			},
		},
	})
	clihelp.RenderFlagSet(writer, "Flags", flags)
}

func printOneShotHelp(writer io.Writer, topics ...string) {
	if len(topics) == 0 {
		clihelp.Render(writer, clihelp.Doc{
			Usage:   "help [command]",
			Summary: []string{"Interactive command reference for the oneshot REPL."},
			Sections: []clihelp.Section{
				{
					Title: "Commands",
					Entries: []clihelp.Entry{
						{Label: "run, r", Summary: "Start execution from the beginning."},
						{Label: "continue, c", Summary: "Resume until completion or the next breakpoint."},
						{Label: "next, n, step, s", Summary: "Advance one execution step."},
						{Label: "break, b", Summary: "Create breakpoints or show breakpoint help."},
						{Label: "info, i", Summary: "Inspect breakpoints, config, bundle state, storage, or memory."},
						{Label: "print, p", Summary: "Print storage values or memory slices."},
						{Label: "x", Summary: "Alias for examining a memory range."},
						{Label: "set", Summary: "Update config, call input, or paused memory."},
						{Label: "load", Summary: "Load or clear a source bundle."},
						{Label: "encode", Summary: "ABI-encode calldata and update the active call in call mode."},
						{Label: "state", Summary: "Print the current execution state."},
						{Label: "restart", Summary: "Reset execution progress."},
						{Label: "quit, q", Summary: "Exit the session."},
					},
				},
				{
					Title: "Help Forms",
					Entries: []clihelp.Entry{
						{Label: "help <command>", Summary: "Show help for a command."},
						{Label: "<command> help", Summary: "Alternative help form."},
						{Label: "<command> <subcommand> help", Summary: "Show second-level help such as set config help."},
					},
				},
			},
		})
		return
	}

	mainTopic := canonicalHelpTopic(topics[0])
	subTopic := ""
	if len(topics) > 1 {
		subTopic = canonicalHelpTopic(topics[1])
	}

	switch mainTopic {
	case "run":
		clihelp.Render(writer, clihelp.Doc{Usage: "run", Summary: []string{"Restart execution from the beginning and run until completion or a breakpoint."}})
	case "continue":
		clihelp.Render(writer, clihelp.Doc{Usage: "continue", Summary: []string{"Resume from the current pause until completion or another breakpoint."}})
	case "next":
		clihelp.Render(writer, clihelp.Doc{Usage: "next", Summary: []string{"Advance exactly one execution step from the current pause."}})
	case "break":
		switch subTopic {
		case "", "break":
			clihelp.Render(writer, clihelp.Doc{
				Usage:   "break <variant>",
				Summary: []string{"Create source, function, call, storage, or memory breakpoints."},
				Sections: []clihelp.Section{
					{
						Title: "Forms",
						Entries: []clihelp.Entry{
							{Label: "break <file:line[:column]>", Summary: "Set a source breakpoint using the active source bundle."},
							{Label: "break line <file:line[:column]>", Summary: "Explicit source-line form."},
							{Label: "break function <signature>", Summary: "Break when a function selector is entered."},
							{Label: "break call <address> [signature]", Summary: "Break when execution enters a target address, optionally filtered by selector."},
							{Label: "break storage <slot|name> [read|write|rw]", Summary: "Break on storage access by concrete slot or resolved variable name."},
							{Label: "break memory <offset> [size] [read|write|rw]", Summary: "Break on offset-based memory access."},
							{Label: "break memory ... at <file:line[:column]>", Summary: "Restrict an offset-based memory breakpoint to a source location."},
							{Label: "break memory line <file:line[:column]> [read|write|rw]", Summary: "Break on memory access mapped to a source line."},
						},
					},
					{
						Title: "Works Without Source",
						Entries: []clihelp.Entry{
							{Label: "break function", Summary: "Selector-based function entry breakpoint."},
							{Label: "break call", Summary: "Address or selector filtered call breakpoint."},
							{Label: "break storage", Summary: "Storage slot access breakpoint. Prefer concrete slots when no metadata is loaded."},
							{Label: "break memory", Summary: "Offset-based memory access breakpoint."},
						},
					},
					{
						Title: "Requires Source Bundle",
						Entries: []clihelp.Entry{
							{Label: "break <file:line[:column]>", Summary: "Source line breakpoint resolved through source maps."},
							{Label: "break line <file:line[:column]>", Summary: "Explicit source line breakpoint."},
							{Label: "break memory ... at <file:line[:column]>", Summary: "Memory access breakpoint constrained by source location."},
							{Label: "break memory line <file:line[:column]>", Summary: "Line-scoped memory breakpoint."},
						},
					},
				},
			})
		case "line":
			clihelp.Render(writer, clihelp.Doc{Usage: "break line <file:line[:column]>", Summary: []string{"Set a source line breakpoint. Requires a loaded source bundle."}})
		case "function":
			clihelp.Render(writer, clihelp.Doc{Usage: "break function <signature>", Summary: []string{"Break when the root call or a nested call matches the function selector. Works without source metadata."}})
		case "call":
			clihelp.Render(writer, clihelp.Doc{Usage: "break call <address> [signature]", Summary: []string{"Break when execution enters a call to the given address, optionally filtered by selector. Works without source metadata."}})
		case "storage":
			clihelp.Render(writer, clihelp.Doc{Usage: "break storage <slot|name> [read|write|rw]", Summary: []string{"Break on storage access. Slot names resolve through source metadata when available; without source, use a concrete slot value."}})
		case "memory":
			clihelp.Render(writer, clihelp.Doc{Usage: "break memory <offset> [size] [read|write|rw]", Summary: []string{"Offset-based memory breakpoints work without source. The 'at <file:line[:column]>' and 'break memory line ...' forms require a loaded source bundle."}})
		default:
			clihelp.Render(writer, clihelp.Doc{Summary: []string{"Unknown help topic for break: " + subTopic + ". Try: help break"}})
		}
	case "info":
		switch subTopic {
		case "", "info":
			clihelp.Render(writer, clihelp.Doc{Usage: "info <breakpoints|config|bundle|storage|memory>", Summary: []string{"Inspect breakpoints, config, bundle state, or paused storage and memory. Use 'info <topic> help' for a specific subcommand."}})
		case "breakpoints":
			clihelp.Render(writer, clihelp.Doc{Usage: "info breakpoints", Summary: []string{"List configured breakpoints."}})
		case "config":
			clihelp.Render(writer, clihelp.Doc{Usage: "info config", Summary: []string{"Print the current oneshot configuration."}})
		case "bundle":
			clihelp.Render(writer, clihelp.Doc{Usage: "info bundle", Summary: []string{"Print the active source bundle summary."}})
		case "storage":
			clihelp.Render(writer, clihelp.Doc{Usage: "info storage", Summary: []string{"Print decoded storage variables at the current pause. Requires a paused state and source metadata for names."}})
		case "memory":
			clihelp.Render(writer, clihelp.Doc{Usage: "info memory [limit]", Summary: []string{"Print non-zero memory words from the current pause."}})
		default:
			clihelp.Render(writer, clihelp.Doc{Summary: []string{"Unknown help topic for info: " + subTopic + ". Try: help info"}})
		}
	case "print":
		switch subTopic {
		case "", "print":
			clihelp.Render(writer, clihelp.Doc{Usage: "print <storage|memory>", Summary: []string{"Print decoded storage values or memory ranges from the current pause."}})
		case "storage":
			clihelp.Render(writer, clihelp.Doc{Usage: "print storage <name|slot>", Summary: []string{"Print a decoded storage value or the last accessed slot from the current pause."}})
		case "memory":
			clihelp.Render(writer, clihelp.Doc{Usage: "print memory <offset> <size>", Summary: []string{"Print a memory range from the current pause."}})
		default:
			clihelp.Render(writer, clihelp.Doc{Summary: []string{"Unknown help topic for print: " + subTopic + ". Try: help print"}})
		}
	case "x":
		clihelp.Render(writer, clihelp.Doc{Usage: "x <offset> <size>", Summary: []string{"Alias for printing a memory range from the current pause."}})
	case "set":
		switch subTopic {
		case "", "set":
			clihelp.Render(writer, clihelp.Doc{Usage: "set <config|call|memory>", Summary: []string{"Update session config, current call input, or paused memory."}})
		case "config":
			clihelp.Render(writer, clihelp.Doc{
				Usage:   "set config <key> <value>",
				Summary: []string{"Update debugger configuration and reset execution progress when required."},
				Sections: []clihelp.Section{
					{Title: "Supported Keys", Entries: []clihelp.Entry{{Label: "upstream", Summary: "Upstream RPC endpoint."}, {Label: "explorer.api-base", Summary: "Explorer API base URL."}, {Label: "explorer.api-key", Summary: "Explorer API key."}, {Label: "explorer.chain-id", Summary: "Explorer chain ID."}, {Label: "engine.chain-id", Summary: "Local chain ID override."}}},
				},
			})
		case "call":
			clihelp.Render(writer, clihelp.Doc{Usage: "set call <signature> [args...]", Summary: []string{"Only available in call mode. Encode calldata and replace the active call input."}})
		case "memory":
			clihelp.Render(writer, clihelp.Doc{Usage: "set memory <offset|last|line> ...", Summary: []string{"Patch paused memory and record the mutation for later replay. Requires a paused state."}})
		case "last":
			clihelp.Render(writer, clihelp.Doc{Usage: "set memory last <hex-data>", Summary: []string{"Replace the most recently observed memory access range at the current pause."}})
		case "line":
			clihelp.Render(writer, clihelp.Doc{Usage: "set memory line <file:line[:column]> <hex-data>", Summary: []string{"Best-effort source-scoped memory patching. Requires a loaded source bundle and a pause on a matching memory access."}})
		default:
			clihelp.Render(writer, clihelp.Doc{Summary: []string{"Unknown help topic for set: " + subTopic + ". Try: help set"}})
		}
	case "load":
		switch subTopic {
		case "", "load":
			clihelp.Render(writer, clihelp.Doc{Usage: "load <local|explorer|clear>", Summary: []string{"Load source metadata from the local project or explorer APIs, or clear the active bundle."}})
		case "local":
			clihelp.Render(writer, clihelp.Doc{Usage: "load local [contract] [source]", Summary: []string{"Load metadata from the detected local project."}})
		case "explorer":
			clihelp.Render(writer, clihelp.Doc{Usage: "load explorer <address> [contract] [source]", Summary: []string{"Load metadata from the configured explorer API. Configure explorer settings with set config explorer.api-base <url>, set config explorer.api-key <key>, and set config explorer.chain-id <id>."}})
		case "clear":
			clihelp.Render(writer, clihelp.Doc{Usage: "load clear", Summary: []string{"Remove the active source bundle."}})
		default:
			clihelp.Render(writer, clihelp.Doc{Summary: []string{"Unknown help topic for load: " + subTopic + ". Try: help load"}})
		}
	case "encode":
		clihelp.Render(writer, clihelp.Doc{Usage: "encode <signature> [args...]", Summary: []string{"ABI-encode calldata. In call mode it also updates the active call input."}})
	case "state":
		clihelp.Render(writer, clihelp.Doc{Usage: "state", Summary: []string{"Print whether the session is ready, paused, or finished."}})
	case "restart":
		clihelp.Render(writer, clihelp.Doc{Usage: "restart", Summary: []string{"Clear execution progress so the next run starts from the beginning."}})
	case "quit":
		clihelp.Render(writer, clihelp.Doc{Usage: "quit", Summary: []string{"Exit the oneshot session."}})
	default:
		clihelp.Render(writer, clihelp.Doc{Summary: []string{"Unknown help topic: " + mainTopic + ". Try: help"}})
	}
}

func replHelpTopicFromFields(fields []string) []string {
	if len(fields) == 0 {
		return nil
	}
	if isHelpToken(fields[0]) {
		if len(fields) == 1 {
			return nil
		}
		topics := make([]string, 0, len(fields)-1)
		for _, field := range fields[1:] {
			if isHelpToken(field) {
				continue
			}
			topics = append(topics, field)
		}
		return topics
	}
	if len(fields) > 1 && isHelpToken(fields[1]) {
		return []string{fields[0]}
	}
	if len(fields) > 2 && isHelpToken(fields[2]) {
		return []string{fields[0], fields[1]}
	}
	return nil
}

func maybePrintCommandHelp(writer io.Writer, fields []string) bool {
	topics := replHelpTopicFromFields(fields)
	if topics == nil && !(len(fields) > 0 && isHelpToken(fields[0])) {
		return false
	}
	printOneShotHelp(writer, topics...)
	return true
}

func oneshotUsage(path ...string) string {
	canonical := make([]string, 0, len(path))
	for _, item := range path {
		trimmed := canonicalHelpTopic(item)
		if trimmed == "" {
			continue
		}
		canonical = append(canonical, trimmed)
	}
	joined := strings.Join(canonical, " ")
	if joined == "" {
		return "usage: help <command>"
	}
	return "usage: " + joined
}

func oneshotUsageError(path ...string) error {
	usage := oneshotUsage(path...)
	joined := strings.Join(path, " ")
	if strings.TrimSpace(joined) == "" {
		return fmt.Errorf("%s", usage)
	}
	return fmt.Errorf("%s\nTry: help %s", usage, joined)
}
