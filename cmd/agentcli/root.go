package agentcli

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strings"
	"time"

	"agentcli/internal/version"
	"agentcli/pkg/agent"
	appconfig "agentcli/pkg/config"
	contextmgr "agentcli/pkg/context"
	"agentcli/pkg/llm"
	"agentcli/pkg/policy"
	"agentcli/pkg/session"
	"agentcli/pkg/tools"
	"agentcli/pkg/tui"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
)

type options struct {
	cwd         string
	configPath  string
	profile     string
	provider    string
	model       string
	fastModel   string
	editModel   string
	deepModel   string
	maxSteps    int
	maxMessages int
	maxContext  int
	maxOutput   int
	reasoning   string
	store       bool
	stream      bool
	approval    string
	sandbox     string
	sessionID   string
}

func Execute() {
	if err := newRootCommand().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func newRootCommand() *cobra.Command {
	opts := options{}
	root := &cobra.Command{
		Use:     "agentcli",
		Short:   "Agentic coding CLI",
		Version: version.Version,
	}

	root.PersistentFlags().StringVar(&opts.cwd, "cwd", ".", "workspace directory")
	root.PersistentFlags().StringVar(&opts.configPath, "config", "", "config file path")
	root.PersistentFlags().StringVar(&opts.profile, "profile", "", "config profile")
	root.PersistentFlags().StringVar(&opts.provider, "provider", "", "LLM provider: mock, openai, chat, ollama, anthropic")
	root.PersistentFlags().StringVar(&opts.model, "model", "", "model name; for openai can also use OPENAI_MODEL")
	root.PersistentFlags().StringVar(&opts.fastModel, "fast-model", "", "model for inspection/search tasks")
	root.PersistentFlags().StringVar(&opts.editModel, "edit-model", "", "model for coding/edit tasks")
	root.PersistentFlags().StringVar(&opts.deepModel, "deep-model", "", "model for architecture/security/deep tasks")
	root.PersistentFlags().IntVar(&opts.maxSteps, "max-steps", 0, "maximum agent loop steps")
	root.PersistentFlags().IntVar(&opts.maxMessages, "max-messages", 0, "maximum context messages")
	root.PersistentFlags().IntVar(&opts.maxContext, "max-context-tokens", 0, "estimated maximum context tokens")
	root.PersistentFlags().IntVar(&opts.maxOutput, "max-output-tokens", 0, "maximum model output tokens")
	root.PersistentFlags().StringVar(&opts.reasoning, "reasoning", "", "reasoning effort for providers that support it")
	root.PersistentFlags().BoolVar(&opts.store, "store", false, "allow provider-side response storage when supported")
	root.PersistentFlags().BoolVar(&opts.stream, "stream", false, "stream model output when the provider supports it")
	root.PersistentFlags().StringVar(&opts.approval, "approval", "", "tool approval mode: auto, ask, never")
	root.PersistentFlags().StringVar(&opts.sandbox, "sandbox", "", "sandbox profile: read-only, workspace-write, danger-full-access")
	root.PersistentFlags().StringVar(&opts.sessionID, "session", "", "session id for persistent conversations")

	runCmd := &cobra.Command{
		Use:   "run [task]",
		Short: "Run an agent task",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			runtimeCfg, err := effectiveConfig(cmd, opts)
			if err != nil {
				return err
			}
			provider, model, err := buildProvider(runtimeCfg.Provider, runtimeCfg.Model)
			if err != nil {
				return err
			}
			runner, err := agent.New(agent.Config{
				CWD:          opts.cwd,
				Model:        model,
				ModelRoutes:  runtimeCfg.ModelRoutes,
				MaxSteps:     runtimeCfg.MaxSteps,
				MaxMessages:  runtimeCfg.MaxMessages,
				MaxContext:   runtimeCfg.MaxContext,
				MaxOutput:    runtimeCfg.MaxOutput,
				Reasoning:    runtimeCfg.Reasoning,
				Store:        runtimeCfg.StoreResponses,
				Stream:       opts.stream,
				ApprovalMode: runtimeCfg.ApprovalMode,
				Sandbox:      runtimeCfg.Sandbox,
				Provider:     provider,
				Input:        os.Stdin,
				Output:       cmd.OutOrStdout(),
			})
			if err != nil {
				return err
			}
			task := strings.Join(args, " ")
			if strings.TrimSpace(opts.sessionID) == "" {
				_, err = runner.Run(context.Background(), task)
				return err
			}
			store, err := session.NewStore(opts.cwd)
			if err != nil {
				return err
			}
			saved, err := store.LoadOrCreate(opts.sessionID, opts.cwd)
			if err != nil {
				return err
			}
			result, err := runner.RunConversation(context.Background(), saved.Messages, task)
			saved.Messages = result.Messages
			if saveErr := store.Save(saved); saveErr != nil {
				return saveErr
			}
			fmt.Fprintf(cmd.OutOrStdout(), "session: %s\n", saved.ID)
			return err
		},
	}

	root.AddCommand(runCmd)
	root.AddCommand(newChatCommand(&opts))
	root.AddCommand(newTUICommand(&opts))
	root.AddCommand(newStatusCommand(&opts))
	root.AddCommand(newCheckpointCommand(&opts))
	root.AddCommand(newDiffCommand(&opts))
	root.AddCommand(newPolicyCommand())
	root.AddCommand(newToolsCommand())
	root.AddCommand(newDoctorCommand())
	root.AddCommand(newConfigCommand(&opts))
	root.AddCommand(newSessionsCommand(&opts))
	return root
}

func newTUICommand(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:   "tui",
		Short: "Start the Bubble Tea terminal UI",
		RunE: func(cmd *cobra.Command, _ []string) error {
			runtimeCfg, err := effectiveConfig(cmd, *opts)
			if err != nil {
				return err
			}
			provider, model, err := buildProvider(runtimeCfg.Provider, runtimeCfg.Model)
			if err != nil {
				return err
			}
			store, err := session.NewStore(opts.cwd)
			if err != nil {
				return err
			}
			saved, err := store.LoadOrCreate(opts.sessionID, opts.cwd)
			if err != nil {
				return err
			}
			handler := func(input string) (string, error) {
				switch strings.TrimSpace(input) {
				case "/status":
					return tuiStatus(opts.cwd)
				case "/compact":
					compacted, stats := contextmgr.CompactMessages(saved.Messages, runtimeCfg.MaxContext, runtimeCfg.MaxMessages/2)
					saved.Messages = compacted
					if err := store.Save(saved); err != nil {
						return "", err
					}
					return fmt.Sprintf("compacted: messages %d -> %d, estimated tokens %d -> %d",
						stats.OriginalMessages, stats.PackedMessages, stats.OriginalTokens, stats.PackedTokens), nil
				}
				var output strings.Builder
				runnerWithOutput, err := agent.New(agent.Config{
					CWD:          opts.cwd,
					Model:        model,
					ModelRoutes:  runtimeCfg.ModelRoutes,
					MaxSteps:     runtimeCfg.MaxSteps,
					MaxMessages:  runtimeCfg.MaxMessages,
					MaxContext:   runtimeCfg.MaxContext,
					MaxOutput:    runtimeCfg.MaxOutput,
					Reasoning:    runtimeCfg.Reasoning,
					Store:        runtimeCfg.StoreResponses,
					Stream:       false,
					ApprovalMode: runtimeCfg.ApprovalMode,
					Sandbox:      runtimeCfg.Sandbox,
					Provider:     provider,
					Input:        os.Stdin,
					Output:       &output,
				})
				if err != nil {
					return "", err
				}
				result, err := runnerWithOutput.RunConversation(context.Background(), saved.Messages, input)
				saved.Messages = result.Messages
				if saveErr := store.Save(saved); saveErr != nil {
					return "", saveErr
				}
				if strings.TrimSpace(output.String()) != "" {
					return strings.TrimSpace(output.String()), err
				}
				return result.Final, err
			}

			tuiModel := tui.New(tui.Config{
				Title:     "Agent CLI",
				SessionID: saved.ID,
				Provider:  runtimeCfg.Provider,
				Model:     model,
				Handler:   handler,
			})
			_, err = tea.NewProgram(tuiModel).Run()
			return err
		},
	}
}

func tuiStatus(cwd string) (string, error) {
	status, err := (tools.GitStatus{}).Run(context.Background(), tools.Invocation{CWD: cwd, Args: map[string]any{}})
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(status.Output) == "" {
		return "clean", nil
	}
	return status.Output, nil
}

func newDiffCommand(opts *options) *cobra.Command {
	diffCmd := &cobra.Command{
		Use:   "diff",
		Short: "Preview and validate unified diffs",
	}
	diffCmd.AddCommand(&cobra.Command{
		Use:   "preview [patch-file]",
		Short: "Validate a unified diff without applying it; reads stdin when no file is provided",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			patch, err := readPatchInput(args)
			if err != nil {
				return err
			}
			result, err := (tools.DiffPreview{}).Run(context.Background(), tools.Invocation{
				CWD:  opts.cwd,
				Args: map[string]any{"patch": patch},
			})
			if result.Output != "" {
				fmt.Fprintln(cmd.OutOrStdout(), result.Output)
			}
			return err
		},
	})
	var yes bool
	var checkpoint bool
	applyCmd := &cobra.Command{
		Use:   "apply [patch-file]",
		Short: "Preview, checkpoint, and apply a unified diff",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			patch, err := readPatchInput(args)
			if err != nil {
				return err
			}
			preview, err := (tools.DiffPreview{}).Run(context.Background(), tools.Invocation{
				CWD:  opts.cwd,
				Args: map[string]any{"patch": patch},
			})
			if preview.Output != "" {
				fmt.Fprintln(cmd.OutOrStdout(), preview.Output)
			}
			if err != nil {
				return err
			}
			if !yes && !confirm(cmd.InOrStdin(), cmd.OutOrStdout(), "apply patch? [y/N]: ") {
				return fmt.Errorf("patch apply cancelled")
			}
			if checkpoint {
				id := "before-patch-" + time.Now().UTC().Format("20060102-150405")
				result, err := (tools.WorkspaceCheckpoint{}).Run(context.Background(), tools.Invocation{
					CWD:  opts.cwd,
					Args: map[string]any{"id": id},
				})
				if err != nil {
					return err
				}
				fmt.Fprintln(cmd.OutOrStdout(), result.Output)
			}
			result, err := (tools.DiffPatcher{}).Run(context.Background(), tools.Invocation{
				CWD:  opts.cwd,
				Args: map[string]any{"patch": patch},
			})
			if result.Output != "" {
				fmt.Fprintln(cmd.OutOrStdout(), result.Output)
			}
			return err
		},
	}
	applyCmd.Flags().BoolVar(&yes, "yes", false, "apply without interactive confirmation")
	applyCmd.Flags().BoolVar(&checkpoint, "checkpoint", true, "create workspace checkpoint before applying")
	diffCmd.AddCommand(applyCmd)
	return diffCmd
}

func newPolicyCommand() *cobra.Command {
	policyCmd := &cobra.Command{
		Use:   "policy",
		Short: "Inspect local safety policy decisions",
	}
	var sandbox string
	checkCmd := &cobra.Command{
		Use:   "check [command]",
		Short: "Classify a shell command by risk",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			assessment := policy.AssessShellCommand(strings.Join(args, " "))
			allowed, reason := policy.IsAllowedInSandbox(assessment, policy.NormalizeSandbox(sandbox))
			payload := map[string]any{
				"assessment": assessment,
				"sandbox":    policy.NormalizeSandbox(sandbox),
				"allowed":    allowed,
				"reason":     reason,
			}
			data, err := json.MarshalIndent(payload, "", "  ")
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), string(data))
			return nil
		},
	}
	checkCmd.Flags().StringVar(&sandbox, "sandbox", "workspace-write", "sandbox profile to evaluate")
	policyCmd.AddCommand(checkCmd)
	return policyCmd
}

func confirm(input io.Reader, output io.Writer, prompt string) bool {
	fmt.Fprint(output, prompt)
	reader := bufio.NewReader(input)
	answer, err := reader.ReadString('\n')
	if err != nil && strings.TrimSpace(answer) == "" {
		return false
	}
	answer = strings.ToLower(strings.TrimSpace(answer))
	return answer == "y" || answer == "yes"
}

func readPatchInput(args []string) (string, error) {
	if len(args) > 0 {
		data, err := os.ReadFile(args[0])
		if err != nil {
			return "", err
		}
		return string(data), nil
	}
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func newStatusCommand(opts *options) *cobra.Command {
	var showDiff bool
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show compact workspace status",
		RunE: func(cmd *cobra.Command, _ []string) error {
			status, err := (tools.GitStatus{}).Run(context.Background(), tools.Invocation{CWD: opts.cwd, Args: map[string]any{}})
			if err != nil {
				return err
			}
			if strings.TrimSpace(status.Output) == "" {
				fmt.Fprintln(cmd.OutOrStdout(), "clean")
			} else {
				fmt.Fprintln(cmd.OutOrStdout(), status.Output)
			}
			if showDiff {
				diff, err := (tools.GitDiff{}).Run(context.Background(), tools.Invocation{CWD: opts.cwd, Args: map[string]any{"max_lines": 240}})
				if err != nil {
					return err
				}
				fmt.Fprintln(cmd.OutOrStdout(), "\ndiff:")
				fmt.Fprintln(cmd.OutOrStdout(), diff.Output)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&showDiff, "diff", false, "include compressed tracked diff")
	return cmd
}

func newCheckpointCommand(opts *options) *cobra.Command {
	checkpointCmd := &cobra.Command{
		Use:   "checkpoint",
		Short: "Create, list, and restore workspace checkpoints",
	}
	checkpointCmd.AddCommand(&cobra.Command{
		Use:   "create [id]",
		Short: "Create a workspace checkpoint",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := ""
			if len(args) > 0 {
				id = args[0]
			}
			result, err := (tools.WorkspaceCheckpoint{}).Run(context.Background(), tools.Invocation{
				CWD:  opts.cwd,
				Args: map[string]any{"id": id},
			})
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), result.Output)
			return nil
		},
	})
	checkpointCmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List workspace checkpoints",
		RunE: func(cmd *cobra.Command, _ []string) error {
			result, err := (tools.WorkspaceCheckpointList{}).Run(context.Background(), tools.Invocation{CWD: opts.cwd, Args: map[string]any{}})
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), result.Output)
			return nil
		},
	})
	checkpointCmd.AddCommand(&cobra.Command{
		Use:   "restore [id]",
		Short: "Restore files from a workspace checkpoint",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := (tools.WorkspaceRestore{}).Run(context.Background(), tools.Invocation{
				CWD:  opts.cwd,
				Args: map[string]any{"id": args[0]},
			})
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), result.Output)
			return nil
		},
	})
	return checkpointCmd
}

func newChatCommand(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:   "chat",
		Short: "Start an interactive coding session",
		RunE: func(cmd *cobra.Command, _ []string) error {
			runtimeCfg, err := effectiveConfig(cmd, *opts)
			if err != nil {
				return err
			}
			provider, model, err := buildProvider(runtimeCfg.Provider, runtimeCfg.Model)
			if err != nil {
				return err
			}
			store, err := session.NewStore(opts.cwd)
			if err != nil {
				return err
			}
			saved, err := store.LoadOrCreate(opts.sessionID, opts.cwd)
			if err != nil {
				return err
			}
			runner, err := agent.New(agent.Config{
				CWD:          opts.cwd,
				Model:        model,
				ModelRoutes:  runtimeCfg.ModelRoutes,
				MaxSteps:     runtimeCfg.MaxSteps,
				MaxMessages:  runtimeCfg.MaxMessages,
				MaxContext:   runtimeCfg.MaxContext,
				MaxOutput:    runtimeCfg.MaxOutput,
				Reasoning:    runtimeCfg.Reasoning,
				Store:        runtimeCfg.StoreResponses,
				Stream:       opts.stream,
				ApprovalMode: runtimeCfg.ApprovalMode,
				Sandbox:      runtimeCfg.Sandbox,
				Provider:     provider,
				Input:        os.Stdin,
				Output:       cmd.OutOrStdout(),
			})
			if err != nil {
				return err
			}

			fmt.Fprintf(cmd.OutOrStdout(), "session: %s\n", saved.ID)
			fmt.Fprintln(cmd.OutOrStdout(), "type /exit to quit, /save to persist now, /compact to compress history")
			scanner := bufio.NewScanner(os.Stdin)
			for {
				fmt.Fprint(cmd.OutOrStdout(), "> ")
				if !scanner.Scan() {
					break
				}
				line := strings.TrimSpace(scanner.Text())
				switch line {
				case "":
					continue
				case "/exit", "/quit":
					return store.Save(saved)
				case "/help":
					fmt.Fprintln(cmd.OutOrStdout(), "commands: /help, /status, /compact, /save, /exit")
					continue
				case "/status":
					status, err := tuiStatus(opts.cwd)
					if err != nil {
						return err
					}
					fmt.Fprintln(cmd.OutOrStdout(), status)
					continue
				case "/save":
					if err := store.Save(saved); err != nil {
						return err
					}
					fmt.Fprintf(cmd.OutOrStdout(), "saved: %s\n", saved.ID)
					continue
				case "/compact":
					compacted, stats := contextmgr.CompactMessages(saved.Messages, runtimeCfg.MaxContext, runtimeCfg.MaxMessages/2)
					saved.Messages = compacted
					if err := store.Save(saved); err != nil {
						return err
					}
					fmt.Fprintf(cmd.OutOrStdout(), "compacted: messages %d -> %d, estimated tokens %d -> %d\n",
						stats.OriginalMessages, stats.PackedMessages, stats.OriginalTokens, stats.PackedTokens)
					continue
				}
				result, err := runner.RunConversation(context.Background(), saved.Messages, line)
				saved.Messages = result.Messages
				if saveErr := store.Save(saved); saveErr != nil {
					return saveErr
				}
				if err != nil {
					fmt.Fprintf(cmd.OutOrStdout(), "error: %v\n", err)
				}
			}
			if err := scanner.Err(); err != nil {
				return err
			}
			return store.Save(saved)
		},
	}
}

func newToolsCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "tools",
		Short: "List built-in tools available to the agent",
		RunE: func(cmd *cobra.Command, _ []string) error {
			specs := tools.NewDefaultRegistry().Specs()
			sort.Slice(specs, func(i, j int) bool { return specs[i].Name < specs[j].Name })
			for _, spec := range specs {
				fmt.Fprintf(cmd.OutOrStdout(), "%-16s %s\n", spec.Name, spec.Description)
			}
			return nil
		},
	}
}

func newDoctorCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check local runtime support for agentcli",
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "agentcli: %s\n", version.Version)
			fmt.Fprintf(out, "go: %s\n", runtime.Version())
			fmt.Fprintf(out, "os/arch: %s/%s\n", runtime.GOOS, runtime.GOARCH)
			printExecutableStatus(out, "git")
			printExecutableStatus(out, "rg")
			printEnvStatus(out, "OPENAI_API_KEY")
			printEnvStatus(out, "OPENAI_MODEL")
			printEnvStatus(out, "OPENAI_BASE_URL")
			printEnvStatus(out, "OLLAMA_MODEL")
			printEnvStatus(out, "OLLAMA_BASE_URL")
			printEnvStatus(out, "ANTHROPIC_API_KEY")
			printEnvStatus(out, "ANTHROPIC_MODEL")
			printEnvStatus(out, "ANTHROPIC_BASE_URL")
			return nil
		},
	}
}

func newConfigCommand(opts *options) *cobra.Command {
	configCmd := &cobra.Command{
		Use:   "config",
		Short: "Manage agentcli configuration",
	}
	configCmd.AddCommand(&cobra.Command{
		Use:   "init",
		Short: "Write a default config file",
		RunE: func(cmd *cobra.Command, _ []string) error {
			path, err := appconfig.WriteDefault(opts.configPath)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "wrote %s\n", path)
			return nil
		},
	})
	configCmd.AddCommand(&cobra.Command{
		Use:   "show",
		Short: "Print effective configuration",
		RunE: func(cmd *cobra.Command, _ []string) error {
			runtimeCfg, err := effectiveConfig(cmd, *opts)
			if err != nil {
				return err
			}
			data, err := json.MarshalIndent(runtimeCfg, "", "  ")
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), string(data))
			return nil
		},
	})
	return configCmd
}

func newSessionsCommand(opts *options) *cobra.Command {
	sessionsCmd := &cobra.Command{
		Use:   "sessions",
		Short: "List saved workspace sessions",
		RunE: func(cmd *cobra.Command, _ []string) error {
			store, err := session.NewStore(opts.cwd)
			if err != nil {
				return err
			}
			sessions, err := store.List()
			if err != nil {
				return err
			}
			sort.Slice(sessions, func(i, j int) bool {
				return sessions[i].UpdatedAt.After(sessions[j].UpdatedAt)
			})
			for _, session := range sessions {
				fmt.Fprintf(cmd.OutOrStdout(), "%s messages=%d updated=%s\n", session.ID, len(session.Messages), session.UpdatedAt.Format("2006-01-02 15:04:05"))
			}
			return nil
		},
	}
	sessionsCmd.AddCommand(&cobra.Command{
		Use:   "compact [id]",
		Short: "Compact a saved session to reduce future context tokens",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			runtimeCfg, err := effectiveConfig(cmd, *opts)
			if err != nil {
				return err
			}
			store, err := session.NewStore(opts.cwd)
			if err != nil {
				return err
			}
			saved, err := store.Load(args[0])
			if err != nil {
				return err
			}
			compacted, stats := contextmgr.CompactMessages(saved.Messages, runtimeCfg.MaxContext, runtimeCfg.MaxMessages/2)
			saved.Messages = compacted
			if err := store.Save(saved); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "compacted %s: messages %d -> %d, estimated tokens %d -> %d\n",
				saved.ID, stats.OriginalMessages, stats.PackedMessages, stats.OriginalTokens, stats.PackedTokens)
			return nil
		},
	})
	return sessionsCmd
}

func effectiveConfig(cmd *cobra.Command, opts options) (appconfig.Config, error) {
	cfg, err := appconfig.Load(opts.configPath)
	if err != nil {
		return appconfig.Config{}, err
	}
	cfg, err = cfg.WithProfile(opts.profile)
	if err != nil {
		return appconfig.Config{}, err
	}
	flags := cmd.Root().PersistentFlags()
	if flags.Changed("provider") {
		cfg.Provider = opts.provider
	}
	if flags.Changed("model") {
		cfg.Model = opts.model
	}
	if cfg.ModelRoutes == nil {
		cfg.ModelRoutes = map[string]string{}
	}
	if flags.Changed("fast-model") {
		cfg.ModelRoutes["fast"] = opts.fastModel
	}
	if flags.Changed("edit-model") {
		cfg.ModelRoutes["edit"] = opts.editModel
	}
	if flags.Changed("deep-model") {
		cfg.ModelRoutes["deep"] = opts.deepModel
	}
	if flags.Changed("max-steps") {
		cfg.MaxSteps = opts.maxSteps
	}
	if flags.Changed("max-messages") {
		cfg.MaxMessages = opts.maxMessages
	}
	if flags.Changed("max-context-tokens") {
		cfg.MaxContext = opts.maxContext
	}
	if flags.Changed("max-output-tokens") {
		cfg.MaxOutput = opts.maxOutput
	}
	if flags.Changed("reasoning") {
		cfg.Reasoning = opts.reasoning
	}
	if flags.Changed("approval") {
		cfg.ApprovalMode = opts.approval
	}
	if flags.Changed("sandbox") {
		cfg.Sandbox = opts.sandbox
	}
	if flags.Changed("store") {
		cfg.StoreResponses = opts.store
	}
	return cfg, nil
}

func printExecutableStatus(out interface{ Write([]byte) (int, error) }, name string) {
	path, err := exec.LookPath(name)
	if err != nil {
		fmt.Fprintf(out, "%s: missing\n", name)
		return
	}
	fmt.Fprintf(out, "%s: %s\n", name, path)
}

func printEnvStatus(out interface{ Write([]byte) (int, error) }, name string) {
	if strings.TrimSpace(os.Getenv(name)) == "" {
		fmt.Fprintf(out, "%s: unset\n", name)
		return
	}
	fmt.Fprintf(out, "%s: set\n", name)
}

func buildProvider(name, model string) (llm.Provider, string, error) {
	normalized := strings.ToLower(strings.TrimSpace(name))
	switch normalized {
	case "", "mock":
		if strings.TrimSpace(model) == "" {
			model = "mock-agent"
		}
		return llm.NewMockProvider(), model, nil
	case "openai", "responses":
		provider, err := llm.NewResponsesProviderFromEnv()
		if err != nil {
			return nil, "", err
		}
		if strings.TrimSpace(model) == "" || model == "mock-agent" {
			model = os.Getenv("OPENAI_MODEL")
		}
		return provider, model, nil
	case "chat", "chat-completions", "openai-chat", "openai-compatible":
		provider, err := llm.NewOpenAIProviderFromEnv()
		if err != nil {
			return nil, "", err
		}
		if strings.TrimSpace(model) == "" || model == "mock-agent" {
			model = os.Getenv("OPENAI_MODEL")
		}
		return provider, model, nil
	case "ollama":
		baseURL := os.Getenv("OLLAMA_BASE_URL")
		if strings.TrimSpace(baseURL) == "" {
			baseURL = "http://localhost:11434/v1"
		}
		if strings.TrimSpace(model) == "" || model == "mock-agent" {
			model = os.Getenv("OLLAMA_MODEL")
		}
		provider, err := llm.NewOpenAIProvider("ollama", baseURL)
		if err != nil {
			return nil, "", err
		}
		return provider, model, nil
	case "anthropic", "claude":
		provider, err := llm.NewAnthropicProviderFromEnv()
		if err != nil {
			return nil, "", err
		}
		if strings.TrimSpace(model) == "" || model == "mock-agent" {
			model = os.Getenv("ANTHROPIC_MODEL")
		}
		if strings.TrimSpace(model) == "" {
			return nil, "", fmt.Errorf("model is required for provider %q; pass --model or set ANTHROPIC_MODEL", normalized)
		}
		return provider, model, nil
	default:
		return nil, "", fmt.Errorf("provider %q is not implemented yet", name)
	}
}
