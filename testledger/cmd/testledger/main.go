// The command-line transport.
//
// Every subcommand comes from the shared operation registry, so this file
// contains no per-command argument parsing and no per-command output shaping.
// What lives here is what is genuinely command-line: process exit codes,
// human-readable rendering, configuration management, and launching the MCP
// server.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/danielcsee/sciterm/testledger/internal/app"
	"github.com/danielcsee/sciterm/testledger/internal/config"
	"github.com/danielcsee/sciterm/testledger/internal/mcpserver"
	"github.com/danielcsee/sciterm/testledger/internal/ops"
)

func main() {
	if err := run(); err != nil {
		if exit, ok := err.(exitError); ok {
			os.Exit(exit.code)
		}
		fmt.Fprintln(os.Stderr, "testledger:", err)
		os.Exit(1)
	}
}

type exitError struct{ code int }

func (e exitError) Error() string { return fmt.Sprintf("exit status %d", e.code) }

// globals are the flags that apply before a subcommand is chosen.
type globals struct {
	root        string
	configPath  string
	resetConfig bool
	asJSON      bool
}

func run() error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	opts := globals{root: cwd}
	args := os.Args[1:]
	for len(args) > 0 && strings.HasPrefix(args[0], "-") {
		name := strings.TrimLeft(args[0], "-")
		value := ""
		if equals := strings.Index(name, "="); equals >= 0 {
			name, value = name[:equals], name[equals+1:]
		}
		needsValue := name == "root" || name == "config"
		if needsValue && value == "" {
			if len(args) < 2 {
				return fmt.Errorf("--%s requires a value", name)
			}
			value, args = args[1], args[1:]
		}
		switch name {
		case "root":
			opts.root = value
		case "config":
			opts.configPath = value
		case "reset-config":
			opts.resetConfig = true
		case "json":
			opts.asJSON = true
		case "h", "help":
			printUsage()
			return nil
		default:
			return fmt.Errorf("unknown global flag --%s", name)
		}
		args = args[1:]
	}

	if opts.resetConfig {
		path, backup, err := config.Reset(opts.root, opts.configPath)
		if err != nil {
			return err
		}
		if backup == "" {
			fmt.Fprintf(os.Stderr, "Created configuration %s from embedded defaults\n", path)
		} else {
			fmt.Fprintf(os.Stderr, "Reset configuration %s (backup: %s)\n", path, backup)
		}
		if len(args) == 0 {
			return nil
		}
	}
	if len(args) == 0 {
		printUsage()
		return exitError{code: 2}
	}

	verb, rest := args[0], args[1:]
	switch verb {
	case "help", "-h", "--help":
		if len(rest) > 0 {
			return printCommandHelp(rest[0])
		}
		printUsage()
		return nil
	case "mcp":
		return serveMCP(opts)
	}

	operation, known := ops.ByVerb(verb)
	if !known {
		return fmt.Errorf("unknown command %q (try: testledger help)", verb)
	}
	// --json may follow the subcommand as well as precede it.
	filtered := make([]string, 0, len(rest))
	for _, arg := range rest {
		if arg == "--json" || arg == "-json" {
			opts.asJSON = true
			continue
		}
		filtered = append(filtered, arg)
	}
	input, err := ops.ParseArgs(operation, filtered)
	if err != nil {
		return err
	}

	ctx := context.Background()
	application, err := app.Open(opts.root, opts.configPath)
	if err != nil {
		return err
	}
	defer application.Close()

	result, err := operation.Run(ctx, application, input)
	if err != nil {
		return err
	}
	if opts.asJSON {
		if err := printJSON(result.Value); err != nil {
			return err
		}
	} else {
		fmt.Println(result.Summary)
		for _, line := range result.Detail {
			fmt.Println(line)
		}
	}
	if result.ExitCode != 0 {
		return exitError{code: result.ExitCode}
	}
	return nil
}

func serveMCP(opts globals) error {
	ctx := context.Background()
	application, err := app.Open(opts.root, opts.configPath)
	if err != nil {
		return err
	}
	defer application.Close()
	return mcpserver.New(application, os.Stdout).Serve(ctx, os.Stdin)
}

func printJSON(value any) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func printCommandHelp(verb string) error {
	operation, known := ops.ByVerb(verb)
	if !known {
		return fmt.Errorf("unknown command %q", verb)
	}
	fmt.Print(ops.Usage(operation))
	fmt.Println("\n      Arguments marked * are required. --input <file> supplies the whole")
	fmt.Println("      argument set as JSON. This command is also the MCP tool " + operation.Name + ".")
	return nil
}

func printUsage() {
	fmt.Println("testledger -- a function-level test inventory and runner")
	fmt.Println("\nUsage: testledger [global flags] <command> [arguments]")
	fmt.Println("\nGlobal flags:")
	fmt.Println("      --root <dir>      project root (default: working directory)")
	fmt.Println("      --config <path>   configuration path, relative to the root")
	fmt.Println("      --json            print the structured result instead of prose")
	fmt.Println("      --reset-config    restore embedded defaults, keeping a .bak")
	fmt.Println("\nCommands:")
	for _, operation := range ops.For(ops.SurfaceCLI) {
		fmt.Printf("  %-12s %s\n", operation.Verb, operation.Summary)
	}
	fmt.Printf("  %-12s %s\n", "mcp", "Serve the same commands as MCP tools over stdio.")
	fmt.Printf("  %-12s %s\n", "help", "Show this text, or `help <command>` for one command's arguments.")
	fmt.Println("\nEvery command above is also an MCP tool of the same capability.")
	fmt.Println("Run `testledger help <command>` for its arguments.")
}
