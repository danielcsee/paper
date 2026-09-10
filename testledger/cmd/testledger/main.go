package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/danielcsee/sciterm/testledger/internal/app"
	"github.com/danielcsee/sciterm/testledger/internal/config"
	"github.com/danielcsee/sciterm/testledger/internal/mcpserver"
	"github.com/danielcsee/sciterm/testledger/internal/model"
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

func run() error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	global := flag.NewFlagSet("testledger", flag.ContinueOnError)
	root := global.String("root", cwd, "project root")
	configPath := global.String("config", "", "configuration path, relative to project root")
	resetConfig := global.Bool("reset-config", false, "back up and restore the embedded default configuration")
	global.SetOutput(os.Stderr)
	if err := global.Parse(os.Args[1:]); err != nil {
		return err
	}
	args := global.Args()
	if *resetConfig {
		path, backup, err := config.Reset(*root, *configPath)
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
		return usageError()
	}

	ctx := context.Background()
	application, err := app.Open(*root, *configPath)
	if err != nil {
		return err
	}
	defer application.Close()

	switch args[0] {
	case "check":
		return runCheck(ctx, application, args[1:])
	case "test":
		return runTest(ctx, application, args[1:])
	case "affected":
		return runAffected(ctx, application, args[1:])
	case "mutation":
		return runMutation(ctx, application, args[1:])
	case "status":
		return runStatus(ctx, application, args[1:])
	case "skip":
		return runSkip(ctx, application, args[1:])
	case "next":
		return runNext(ctx, application, args[1:])
	case "propose":
		return runPropose(ctx, application, args[1:])
	case "proposals":
		return runProposals(ctx, application, args[1:])
	case "decide":
		return runDecide(ctx, application, args[1:])
	case "implemented":
		return runImplemented(ctx, application, args[1:])
	case "mcp":
		return mcpserver.New(application, os.Stdout).Serve(ctx, os.Stdin)
	case "help", "-h", "--help":
		printUsage()
		return nil
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func runCheck(ctx context.Context, application *app.App, args []string) error {
	flags := flag.NewFlagSet("check", flag.ContinueOnError)
	asJSON := flags.Bool("json", false, "write structured JSON")
	if err := flags.Parse(args); err != nil {
		return err
	}
	result, err := application.Check(ctx)
	if err != nil {
		return err
	}
	if *asJSON {
		return printJSON(result)
	}
	fmt.Printf("Inventory %s: %d functions, %d changed, %d active skips\n", result.InventoryRunID, result.Scanned, result.Changed, result.Skipped)
	if len(result.ChangedGaps) == 0 {
		fmt.Println("No new or changed functions require coverage.")
	} else {
		fmt.Printf("%d changed functions require tests:\n", len(result.ChangedGaps))
		for _, gap := range result.ChangedGaps {
			fmt.Printf("  %s (%s)\n", gap.SymbolKey, gap.Reason)
		}
	}
	if unchanged := len(result.AllGaps) - len(result.ChangedGaps); unchanged > 0 {
		fmt.Printf("%d unchanged coverage gaps remain in the ledger.\n", unchanged)
	}
	return nil
}

func runTest(ctx context.Context, application *app.App, args []string) error {
	flags := flag.NewFlagSet("test", flag.ContinueOnError)
	asJSON := flags.Bool("json", false, "write structured JSON")
	language := flags.String("language", "", "configured language name")
	affected := flags.Bool("affected", false, "select tests from prior dynamic coverage")
	if err := flags.Parse(args); err != nil {
		return err
	}
	selectors := flags.Args()
	if *affected {
		if len(selectors) > 0 {
			return fmt.Errorf("--affected cannot be combined with explicit test selectors")
		}
		selection, err := application.SelectAffectedTestsForLanguage(ctx, *language)
		if err != nil {
			return err
		}
		if !selection.Fallback && len(selection.TestKeys) == 0 {
			return fmt.Errorf("no changed functions have affected tests")
		}
		if !selection.Fallback {
			selectors = selection.TestKeys
		}
	}
	result, err := application.TestLanguage(ctx, *language, selectors)
	if err != nil {
		return err
	}
	if *asJSON {
		if err := printJSON(result); err != nil {
			return err
		}
	} else {
		fmt.Printf("Test run %s: %s (%d passed, %d failed, %d errors, %d skipped)\n", result.RunID, result.Status, result.Passed, result.Failed, result.Errors, result.Skipped)
		fmt.Printf("Coverage mappings recorded: %d\nArtifacts: %s\n", result.CoverageMappings, result.ArtifactDirectory)
		if result.InfrastructureErr != "" {
			fmt.Println("Infrastructure:", result.InfrastructureErr)
		}
	}
	if result.Status != "passed" {
		code := result.ExitCode
		if code <= 0 {
			code = 1
		}
		return exitError{code: code}
	}
	return nil
}

func runAffected(ctx context.Context, application *app.App, args []string) error {
	flags := flag.NewFlagSet("affected", flag.ContinueOnError)
	asJSON := flags.Bool("json", false, "write structured JSON")
	language := flags.String("language", "", "optional configured language name")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if len(flags.Args()) != 0 {
		return fmt.Errorf("affected accepts no positional arguments")
	}
	result, err := application.SelectAffectedTestsForLanguage(ctx, *language)
	if err != nil {
		return err
	}
	if *asJSON {
		return printJSON(result)
	}
	if result.Fallback {
		fmt.Println("Full-suite fallback:", result.Reason)
	} else if len(result.TestKeys) == 0 {
		fmt.Println("No affected tests: no functions changed.")
	} else {
		for _, key := range result.TestKeys {
			fmt.Println(key)
		}
	}
	return nil
}

func runMutation(ctx context.Context, application *app.App, args []string) error {
	flags := flag.NewFlagSet("mutation", flag.ContinueOnError)
	language := flags.String("language", "", "configured language name")
	asJSON := flags.Bool("json", false, "write structured JSON")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if len(flags.Args()) != 0 {
		return fmt.Errorf("mutation accepts no positional arguments")
	}
	result, err := application.RunMutation(ctx, *language)
	if err != nil {
		return err
	}
	if *asJSON {
		return printJSON(result)
	}
	fmt.Printf("Mutation run %s: %s (%d signals)\n", result.RunID, result.Status, len(result.Results))
	if result.Status != "passed" {
		code := result.ExitCode
		if code <= 0 {
			code = 1
		}
		return exitError{code: code}
	}
	return nil
}

func runStatus(ctx context.Context, application *app.App, args []string) error {
	flags := flag.NewFlagSet("status", flag.ContinueOnError)
	asJSON := flags.Bool("json", false, "write structured JSON")
	if err := flags.Parse(args); err != nil {
		return err
	}
	status, err := application.Status(ctx)
	if err != nil {
		return err
	}
	if *asJSON {
		return printJSON(status)
	}
	fmt.Printf("Latest inventory: %s (%d functions)\n", valueOr(status.LatestInventoryID, "none"), status.SymbolCount)
	fmt.Printf("Outstanding gaps: %d\nActive skips: %d\n", status.OutstandingGaps, status.ActiveSkips)
	if status.LatestTestRun != nil {
		fmt.Printf("Latest test run: %s (%s)\n", status.LatestTestRun.RunID, status.LatestTestRun.Status)
	}
	return nil
}

func runSkip(ctx context.Context, application *app.App, args []string) error {
	flags := flag.NewFlagSet("skip", flag.ContinueOnError)
	reason := flags.String("reason", "", "required human rationale")
	durable := flags.Bool("durable", false, "apply to future versions of this symbol")
	expires := flags.String("expires", "", "optional RFC3339 expiration")
	asJSON := flags.Bool("json", false, "write structured JSON")
	if err := flags.Parse(args); err != nil {
		return err
	}
	remaining := flags.Args()
	if len(remaining) != 1 {
		return fmt.Errorf("skip requires exactly one symbol key")
	}
	var expiresAt *time.Time
	if *expires != "" {
		parsed, err := time.Parse(time.RFC3339, *expires)
		if err != nil {
			return fmt.Errorf("parse --expires: %w", err)
		}
		expiresAt = &parsed
	}
	if err := application.AddSkip(ctx, remaining[0], *reason, *durable, expiresAt); err != nil {
		return err
	}
	response := map[string]any{"symbol_key": remaining[0], "recorded": true, "durable": *durable, "reason": *reason}
	if *asJSON {
		return printJSON(response)
	}
	fmt.Println("Recorded skip for", remaining[0])
	return nil
}

func runNext(ctx context.Context, application *app.App, args []string) error {
	flags := flag.NewFlagSet("next", flag.ContinueOnError)
	limit := flags.Int("limit", 10, "maximum actions")
	asJSON := flags.Bool("json", false, "write structured JSON")
	if err := flags.Parse(args); err != nil {
		return err
	}
	result, err := application.NextActions(ctx, *limit)
	if err != nil {
		return err
	}
	if *asJSON {
		return printJSON(result)
	}
	fmt.Println("State:", result.State)
	for _, action := range result.Actions {
		fmt.Printf("  %s %s %s\n", action.Type, action.ID, action.Description)
	}
	return nil
}

type proposalInput struct {
	Targets   []string             `json:"targets"`
	Cases     []model.ProposalCase `json:"cases"`
	Rationale string               `json:"rationale"`
	CreatedBy string               `json:"created_by"`
}

func decodeFile(path string, target any) error {
	var contents []byte
	var err error
	if path == "-" {
		contents, err = io.ReadAll(os.Stdin)
	} else {
		contents, err = os.ReadFile(path)
	}
	if err != nil {
		return err
	}
	if err := json.Unmarshal(contents, target); err != nil {
		return fmt.Errorf("decode %s: %w", path, err)
	}
	return nil
}

func runPropose(ctx context.Context, application *app.App, args []string) error {
	flags := flag.NewFlagSet("propose", flag.ContinueOnError)
	asJSON := flags.Bool("json", false, "write structured JSON")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if len(flags.Args()) != 1 {
		return fmt.Errorf("propose requires a JSON file path or - for stdin")
	}
	var input proposalInput
	if err := decodeFile(flags.Args()[0], &input); err != nil {
		return err
	}
	result, err := application.CreateProposal(ctx, input.Targets, input.Cases, input.Rationale, input.CreatedBy)
	if err != nil {
		return err
	}
	if *asJSON {
		return printJSON(result)
	}
	fmt.Printf("Recorded proposal %s; awaiting human decision.\n", result.ID)
	return nil
}

func runProposals(ctx context.Context, application *app.App, args []string) error {
	flags := flag.NewFlagSet("proposals", flag.ContinueOnError)
	status := flags.String("status", "", "optional status")
	limit := flags.Int("limit", 20, "page size")
	cursor := flags.Int("cursor", 0, "offset cursor")
	asJSON := flags.Bool("json", false, "write structured JSON")
	if err := flags.Parse(args); err != nil {
		return err
	}
	page, err := application.Store.ListProposals(ctx, *status, *limit, *cursor)
	if err != nil {
		return err
	}
	if *asJSON {
		return printJSON(page)
	}
	for _, proposal := range page.Items {
		fmt.Printf("%s %s (%d targets, %d cases)\n", proposal.ID, proposal.Status, len(proposal.Targets), len(proposal.Cases))
	}
	if page.NextCursor > 0 {
		fmt.Println("Next cursor:", page.NextCursor)
	}
	return nil
}

func runDecide(ctx context.Context, application *app.App, args []string) error {
	flags := flag.NewFlagSet("decide", flag.ContinueOnError)
	decision := flags.String("decision", "", "approved or rejected")
	by := flags.String("by", "human", "human actor")
	reason := flags.String("reason", "", "required rationale")
	asJSON := flags.Bool("json", false, "write structured JSON")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if len(flags.Args()) != 1 {
		return fmt.Errorf("decide requires one proposal ID")
	}
	result, err := application.DecideProposal(ctx, flags.Args()[0], *decision, *by, *reason)
	if err != nil {
		return err
	}
	if *asJSON {
		return printJSON(result)
	}
	fmt.Printf("Proposal %s is now %s.\n", result.ID, result.Status)
	return nil
}

func runImplemented(ctx context.Context, application *app.App, args []string) error {
	flags := flag.NewFlagSet("implemented", flag.ContinueOnError)
	linksPath := flags.String("links", "", "JSON file containing an array of symbol_key/test_key links")
	asJSON := flags.Bool("json", false, "write structured JSON")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if len(flags.Args()) != 1 || *linksPath == "" {
		return fmt.Errorf("implemented requires one proposal ID and --links FILE")
	}
	var links []model.IntendedTestLink
	if err := decodeFile(*linksPath, &links); err != nil {
		return err
	}
	result, err := application.MarkProposalImplemented(ctx, flags.Args()[0], links)
	if err != nil {
		return err
	}
	if *asJSON {
		return printJSON(result)
	}
	fmt.Printf("Proposal %s implementation recorded; coverage verification is required.\n", result.ID)
	return nil
}

func printJSON(value any) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func valueOr(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func usageError() error { printUsage(); return fmt.Errorf("command is required") }

func printUsage() {
	lines := []string{
		"Usage: testledger [--root PATH] [--config PATH] [--reset-config] COMMAND [OPTIONS]",
		"",
		"Commands:",
		"  check    inventory functions and report deterministic coverage gaps",
		"  test     run a configured language test runner with structured results",
		"  affected select tests from changed functions and historical coverage",
		"  mutation run an optional configured mutation adapter",
		"  status   read current ledger state without rescanning",
		"  skip     record a human-approved function disposition",
		"  next     show the compact agent workflow state",
		"  propose  submit a test proposal from JSON",
		"  proposals list persisted proposals",
		"  decide   record a human proposal decision",
		"  implemented record exact implemented pytest links",
		"  mcp      serve MCP over standard input/output",
	}
	fmt.Println(strings.Join(lines, "\n"))
}
