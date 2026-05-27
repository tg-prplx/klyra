package agentcli

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"agentcli/internal/version"
	"agentcli/pkg/agent"
	"agentcli/pkg/cockpit"
	appconfig "agentcli/pkg/config"
	contextmgr "agentcli/pkg/context"
	"agentcli/pkg/instructions"
	"agentcli/pkg/llm"
	"agentcli/pkg/policy"
	"agentcli/pkg/session"
	"agentcli/pkg/tools"
	"agentcli/pkg/tui"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
)

type options struct {
	cwd                    string
	configPath             string
	profile                string
	provider               string
	model                  string
	baseURL                string
	fastModel              string
	editModel              string
	deepModel              string
	maxSteps               int
	maxMessages            int
	maxContext             int
	maxInstructions        int
	maxOutput              int
	reasoning              string
	store                  bool
	stream                 bool
	approval               string
	sandbox                string
	mode                   string
	contextFiles           []string
	sessionID              string
	contextCockpit         bool
	noContextCockpit       bool
	contextCockpitInject   bool
	noContextCockpitInject bool
	contextCockpitTokens   int
	contextCockpitMaxFiles int
	contextRecipes         bool
	noContextRecipes       bool
	negativeContext        bool
	noNegativeContext      bool
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
	root.PersistentFlags().StringVar(&opts.provider, "provider", "", "LLM provider: mock, openai, chat, ollama, anthropic, gemini")
	root.PersistentFlags().StringVar(&opts.model, "model", "", "model name; can also use provider-specific *_MODEL env vars")
	root.PersistentFlags().StringVar(&opts.baseURL, "base-url", "", "provider endpoint base URL override")
	root.PersistentFlags().StringVar(&opts.fastModel, "fast-model", "", "model for inspection/search tasks")
	root.PersistentFlags().StringVar(&opts.editModel, "edit-model", "", "model for coding/edit tasks")
	root.PersistentFlags().StringVar(&opts.deepModel, "deep-model", "", "model for architecture/security/deep tasks")
	root.PersistentFlags().IntVar(&opts.maxSteps, "max-steps", 0, "maximum agent loop steps")
	root.PersistentFlags().IntVar(&opts.maxMessages, "max-messages", 0, "maximum context messages")
	root.PersistentFlags().IntVar(&opts.maxContext, "max-context-tokens", 0, "estimated maximum context tokens")
	root.PersistentFlags().IntVar(&opts.maxInstructions, "max-instruction-bytes", 0, "maximum bytes of project instruction files to add to the system prompt")
	root.PersistentFlags().IntVar(&opts.maxOutput, "max-output-tokens", 0, "maximum model output tokens")
	root.PersistentFlags().StringVar(&opts.reasoning, "reasoning", "", "reasoning effort for providers that support it")
	root.PersistentFlags().BoolVar(&opts.store, "store", false, "allow provider-side response storage when supported")
	root.PersistentFlags().BoolVar(&opts.stream, "stream", false, "stream model output when the provider supports it")
	root.PersistentFlags().StringVar(&opts.approval, "approval", "", "tool approval mode: auto, ask, never")
	root.PersistentFlags().StringVar(&opts.sandbox, "sandbox", "", "sandbox profile: read-only, workspace-write, danger-full-access")
	root.PersistentFlags().StringVar(&opts.mode, "mode", "", "agent mode: inspect, edit, repair, refactor")
	root.PersistentFlags().StringSliceVar(&opts.contextFiles, "context-file", nil, "file allowed in edit/refactor context cart; repeatable")
	root.PersistentFlags().StringVar(&opts.sessionID, "session", "", "session id for persistent conversations")
	root.PersistentFlags().BoolVar(&opts.contextCockpit, "context-cockpit", false, "enable context cockpit fact cards")
	root.PersistentFlags().BoolVar(&opts.noContextCockpit, "no-context-cockpit", false, "disable context cockpit fact cards")
	root.PersistentFlags().BoolVar(&opts.contextCockpitInject, "context-cockpit-inject", false, "inject context cockpit fact cards into model context")
	root.PersistentFlags().BoolVar(&opts.noContextCockpitInject, "no-context-cockpit-inject", false, "show context cockpit without injecting it into model context")
	root.PersistentFlags().IntVar(&opts.contextCockpitTokens, "context-cockpit-tokens", 0, "context cockpit token budget")
	root.PersistentFlags().IntVar(&opts.contextCockpitMaxFiles, "context-cockpit-files", 0, "maximum files ranked in context cockpit repo map")
	root.PersistentFlags().BoolVar(&opts.contextRecipes, "context-recipes", false, "enable scoped context recipes")
	root.PersistentFlags().BoolVar(&opts.noContextRecipes, "no-context-recipes", false, "disable scoped context recipes")
	root.PersistentFlags().BoolVar(&opts.negativeContext, "negative-context", false, "enable negative context deny-list cards")
	root.PersistentFlags().BoolVar(&opts.noNegativeContext, "no-negative-context", false, "disable negative context deny-list cards")

	runCmd := &cobra.Command{
		Use:   "run [task]",
		Short: "Run an agent task",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			runtimeCfg, err := effectiveConfig(cmd, opts)
			if err != nil {
				return err
			}
			provider, model, err := buildProviderFromConfig(runtimeCfg)
			if err != nil {
				return err
			}
			runner, err := agent.New(agent.Config{
				CWD:                    opts.cwd,
				Model:                  model,
				ModelRoutes:            runtimeCfg.ModelRoutes,
				MaxSteps:               runtimeCfg.MaxSteps,
				MaxMessages:            runtimeCfg.MaxMessages,
				MaxContext:             runtimeCfg.MaxContext,
				MaxInstructions:        runtimeCfg.MaxInstructions,
				MaxOutput:              runtimeCfg.MaxOutput,
				Reasoning:              runtimeCfg.Reasoning,
				Store:                  runtimeCfg.StoreResponses,
				Stream:                 opts.stream,
				ApprovalMode:           runtimeCfg.ApprovalMode,
				Sandbox:                runtimeCfg.Sandbox,
				Mode:                   runtimeCfg.Mode,
				ContextFiles:           runtimeCfg.ContextFiles,
				ContextCockpitEnabled:  runtimeCfg.ContextCockpit,
				ContextCockpitInject:   runtimeCfg.ContextCockpitInject,
				ContextCockpitTokens:   runtimeCfg.ContextCockpitTokens,
				ContextCockpitMaxFiles: runtimeCfg.ContextCockpitMaxFiles,
				ContextCockpitDiff:     runtimeCfg.ContextCockpitDiff,
				ContextRecipes:         runtimeCfg.ContextRecipes,
				NegativeContext:        runtimeCfg.NegativeContext,
				Provider:               provider,
				Input:                  os.Stdin,
				Output:                 cmd.OutOrStdout(),
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
			printContextDebug(cmd.OutOrStdout(), result.ContextDebug)
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
	root.AddCommand(newInstructionsCommand(&opts))
	root.AddCommand(newDoctorCommand(&opts))
	root.AddCommand(newConfigCommand(&opts))
	root.AddCommand(newSessionsCommand(&opts))
	return root
}

func newTUIApprover(p **tea.Program) agent.ApprovalFunc {
	return func(req agent.ApprovalRequest) (bool, error) {
		if p == nil || *p == nil {
			return false, fmt.Errorf("approval UI is not ready")
		}
		reply := make(chan bool, 1)
		(*p).Send(tui.ApprovalRequestMsg{
			Tool:   req.Tool,
			Risk:   req.Risk,
			Reason: req.Reason,
			Args:   req.Args,
			Reply:  reply,
		})
		return <-reply, nil
	}
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
			store, err := session.NewStore(opts.cwd)
			if err != nil {
				return err
			}
			saved, err := store.LoadOrCreate(opts.sessionID, opts.cwd)
			if err != nil {
				return err
			}
			tuiCommands := []tui.CommandDef{
				{Name: "/help", Description: "Show available commands"},
				{Name: "/clear", Description: "Clear chat history"},
				{Name: "/status", Description: "Show workspace status"},
				{Name: "/compact", Description: "Compact chat history to reduce tokens"},
				{Name: "/settings", Description: "Open settings form"},
				{Name: "/provider", Description: "Set provider: mock/openai/chat/ollama/anthropic/gemini"},
				{Name: "/model", Description: "Set the active model name"},
				{Name: "/endpoint", Description: "Set provider endpoint base URL"},
				{Name: "/reasoning", Description: "Set reasoning effort: minimal/low/medium/high/xhigh"},
				{Name: "/limits", Description: "Set token/step budgets"},
				{Name: "/approval", Description: "Set approval mode: auto/ask/never"},
				{Name: "/sandbox", Description: "Set sandbox: read-only/workspace-write/danger-full-access"},
				{Name: "/mode", Description: "Set mode: inspect/edit/repair/refactor"},
				{Name: "/cart", Description: "Show or add context cart files"},
				{Name: "/context", Description: "Show context cockpit fact cards"},
				{Name: "/attach", Description: "Attach an image to the next model request"},
				{Name: "/attachments", Description: "Show pending image attachments"},
				{Name: "/doctor", Description: "Check local runtime support"},
				{Name: "/tools", Description: "List available agent tools"},
				{Name: "/instructions", Description: "Show project instruction files"},
				{Name: "/sessions", Description: "List saved workspace sessions"},
				{Name: "/checkpoint", Description: "Open checkpoint actions"},
				{Name: "/checkpoint list", Description: "List workspace checkpoints"},
				{Name: "/checkpoint create", Description: "Create a workspace checkpoint"},
				{Name: "/checkpoint restore", Description: "Restore files from a checkpoint"},
				{Name: "/diff", Description: "Open diff actions"},
				{Name: "/diff preview", Description: "Preview a patch file"},
				{Name: "/diff apply", Description: "Apply a patch file (requires --yes)"},
				{Name: "/policy check", Description: "Classify a shell command by risk"},
				{Name: "/config", Description: "Open config actions"},
				{Name: "/config show", Description: "Print effective configuration"},
				{Name: "/config init", Description: "Write default config file"},
				{Name: "/save", Description: "Manually save the session state"},
				{Name: "/session", Description: "Switch to a saved session by id"},
				{Name: "/exit", Description: "Exit the TUI"},
				{Name: "/quit", Description: "Exit the TUI (alias)"},
			}

			var p *tea.Program
			type activeRun struct {
				cancel context.CancelFunc
			}
			var activeMu sync.Mutex
			var active *activeRun
			pendingAttachments := []llm.Attachment{}

			handler := func(input string) (string, error) {
				trimmed := strings.TrimSpace(input)
				if strings.HasPrefix(trimmed, "/") && !strings.HasPrefix(trimmed, "/exit") && !strings.HasPrefix(trimmed, "/quit") && !strings.HasPrefix(trimmed, "/clear") {
					args := strings.Fields(trimmed)
					cmdName := args[0]

					switch cmdName {
					case "/help":
						var helpOut strings.Builder
						helpOut.WriteString("Available commands:\n")
						for _, c := range tuiCommands {
							helpOut.WriteString(fmt.Sprintf("  %-20s %s\n", c.Name, c.Description))
						}
						return strings.TrimSpace(helpOut.String()), nil
					case "/status":
						return tuiStatus(opts.cwd)
					case "/save":
						if err := store.Save(saved); err != nil {
							return "", err
						}
						return fmt.Sprintf("Session Saved\n\n- ID: `%s`", saved.ID), nil
					case "/session":
						if len(args) < 2 {
							return "usage: /session <id>", nil
						}
						next, err := store.Load(args[1])
						if err != nil {
							return "", err
						}
						saved = next
						opts.sessionID = saved.ID
						if p != nil {
							p.Send(tui.SessionLoadedMsg{
								SessionID: saved.ID,
								Lines:     tuiLinesFromMessages(saved.Messages),
							})
						}
						return fmt.Sprintf("Session Switched\n\n- ID: `%s`\n- Messages: `%d`\n- Updated: `%s`",
							saved.ID, len(saved.Messages), saved.UpdatedAt.Format("2006-01-02 15:04:05")), nil
					case "/compact":
						compacted, stats := contextmgr.CompactMessages(saved.Messages, runtimeCfg.MaxContext, runtimeCfg.MaxMessages/2)
						saved.Messages = compacted
						if err := store.Save(saved); err != nil {
							return "", err
						}
						return fmt.Sprintf("Session Compacted\n\n- Messages: `%d` ➔ `%d`\n- Estimated tokens: `%d` ➔ `%d`",
							stats.OriginalMessages, stats.PackedMessages, stats.OriginalTokens, stats.PackedTokens), nil
					case "/settings":
						_ = runtimeCfg.Save(opts.configPath)
						return formatTUISettings(runtimeCfg, pendingAttachments), nil
					case "/set":
						if err := applyTUISet(&runtimeCfg, args[1:]); err != nil {
							return "", err
						}
						_ = runtimeCfg.Save(opts.configPath)
						return formatSettingSaved("settings", fmt.Sprintf("%d value(s)", len(args)-1)), nil
					case "/provider":
						if len(args) < 2 {
							return "usage: /provider mock|openai|chat|ollama|anthropic|gemini", nil
						}
						runtimeCfg.Provider = args[1]
						runtimeCfg.Model = ""
						if runtimeCfg.Provider == "mock" {
							runtimeCfg.Model = "mock-agent"
						}
						_ = runtimeCfg.Save(opts.configPath)
						return formatSettingSaved("provider", runtimeCfg.Provider), nil
					case "/model":
						if len(args) < 2 {
							return "usage: /model <model-name>", nil
						}
						runtimeCfg.Model = strings.Join(args[1:], " ")
						_ = runtimeCfg.Save(opts.configPath)
						return formatSettingSaved("model", runtimeCfg.Model), nil
					case "/endpoint":
						if len(args) < 2 {
							return "usage: /endpoint <base-url>", nil
						}
						setProviderBaseURL(&runtimeCfg, runtimeCfg.Provider, strings.Join(args[1:], " "))
						_ = runtimeCfg.Save(opts.configPath)
						return formatSettingSaved("endpoint", providerBaseURL(runtimeCfg, runtimeCfg.Provider)), nil
					case "/reasoning":
						if len(args) < 2 {
							return "usage: /reasoning minimal|low|medium|high|xhigh", nil
						}
						runtimeCfg.Reasoning = args[1]
						_ = runtimeCfg.Save(opts.configPath)
						return formatSettingSaved("reasoning", runtimeCfg.Reasoning), nil
					case "/limits":
						if len(args) == 1 {
							return "Limits Usage\n\n`Format:` `/limits [context|output|steps|messages|instructions] <value>`\n\n*Example:* `/limits context 32000`", nil
						}
						if err := applyTUILimit(&runtimeCfg, args[1:]); err != nil {
							return "", err
						}
						_ = runtimeCfg.Save(opts.configPath)
						return formatSettingSaved("limit "+args[1], args[2]), nil
					case "/approval":
						if len(args) < 2 {
							return "usage: /approval auto|ask|never", nil
						}
						runtimeCfg.ApprovalMode = args[1]
						_ = runtimeCfg.Save(opts.configPath)
						return formatSettingSaved("approval", runtimeCfg.ApprovalMode), nil
					case "/sandbox":
						if len(args) < 2 {
							return "usage: /sandbox read-only|workspace-write|danger-full-access", nil
						}
						runtimeCfg.Sandbox = args[1]
						_ = runtimeCfg.Save(opts.configPath)
						return formatSettingSaved("sandbox", runtimeCfg.Sandbox), nil
					case "/mode":
						if len(args) < 2 {
							return "usage: /mode inspect|edit|repair|refactor", nil
						}
						runtimeCfg.Mode = args[1]
						_ = runtimeCfg.Save(opts.configPath)
						return formatSettingSaved("mode", runtimeCfg.Mode), nil
					case "/cart":
						if len(args) >= 3 && args[1] == "add" {
							runtimeCfg.ContextFiles = append(runtimeCfg.ContextFiles, args[2:]...)
						}
						return formatContextCart(runtimeCfg.ContextFiles), nil
					case "/context":
						return formatContextCockpit(runtimeCfg, opts.cwd, strings.TrimSpace(strings.TrimPrefix(trimmed, "/context")))
					case "/attach":
						if len(args) < 2 {
							return "usage: /attach path/to/image.png", nil
						}
						attachment, err := loadImageAttachment(opts.cwd, strings.Join(args[1:], " "))
						if err != nil {
							return "", err
						}
						pendingAttachments = append(pendingAttachments, attachment)
						return fmt.Sprintf("Image Attached\n\n- Name: `%s`\n- Type: `%s`\n- Size: `%d` bytes\n\n*Attachment will be sent with the next request.*", attachment.Name, attachment.MIMEType, len(attachment.Data)), nil
					case "/attachments":
						return formatAttachments(pendingAttachments), nil
					case "/diff":
						if len(args) >= 2 && args[1] == "apply" {
							hasYes := false
							for _, a := range args {
								if a == "--yes" {
									hasYes = true
									break
								}
							}
							if !hasYes {
								return "diff apply requires --yes in the TUI to confirm.", nil
							}
						}
						fallthrough
					case "/doctor", "/tools", "/instructions", "/sessions", "/checkpoint", "/policy", "/config":
						cliCmdName := strings.TrimPrefix(cmdName, "/")
						var out strings.Builder
						subCmd := newRootCommand()
						subCmd.SetArgs(append([]string{cliCmdName}, args[1:]...))
						subCmd.SetOut(&out)
						subCmd.SetErr(&out)
						err := subCmd.Execute()
						return strings.TrimSpace(out.String()), err
					}
				}
				provider, model, err := buildProviderFromConfig(runtimeCfg)
				if err != nil {
					return "", err
				}
				var output strings.Builder
				sawStream := false
				runnerWithOutput, err := agent.New(agent.Config{
					CWD:                    opts.cwd,
					Model:                  model,
					ModelRoutes:            runtimeCfg.ModelRoutes,
					MaxSteps:               runtimeCfg.MaxSteps,
					MaxMessages:            runtimeCfg.MaxMessages,
					MaxContext:             runtimeCfg.MaxContext,
					MaxInstructions:        runtimeCfg.MaxInstructions,
					MaxOutput:              runtimeCfg.MaxOutput,
					Reasoning:              runtimeCfg.Reasoning,
					Store:                  runtimeCfg.StoreResponses,
					Stream:                 true,
					ApprovalMode:           runtimeCfg.ApprovalMode,
					Sandbox:                runtimeCfg.Sandbox,
					Mode:                   runtimeCfg.Mode,
					ContextFiles:           runtimeCfg.ContextFiles,
					ContextCockpitEnabled:  runtimeCfg.ContextCockpit,
					ContextCockpitInject:   runtimeCfg.ContextCockpitInject,
					ContextCockpitTokens:   runtimeCfg.ContextCockpitTokens,
					ContextCockpitMaxFiles: runtimeCfg.ContextCockpitMaxFiles,
					ContextCockpitDiff:     runtimeCfg.ContextCockpitDiff,
					ContextRecipes:         runtimeCfg.ContextRecipes,
					NegativeContext:        runtimeCfg.NegativeContext,
					Provider:               provider,
					Input:                  os.Stdin,
					Output:                 &output,
					Approver:               newTUIApprover(&p),
					StreamHandler: func(event llm.StreamEvent) error {
						if p == nil {
							return nil
						}
						if event.ToolName != "" || event.ToolArgumentsDelta != "" {
							sawStream = true
							p.Send(tui.ToolStreamMsg{
								Index:     event.ToolCallIndex,
								ID:        event.ToolCallID,
								Name:      event.ToolName,
								Arguments: event.ToolArgumentsDelta,
							})
						}
						if event.Delta != "" {
							sawStream = true
							p.Send(tui.StreamMsg(event.Delta))
						}
						return nil
					},
					ReasoningHandler: func(text string) error {
						if text == "" || p == nil {
							return nil
						}
						p.Send(tui.ReasoningMsg(text))
						return nil
					},
					ToolProgress: func(event agent.ToolProgressEvent) error {
						if p == nil {
							return nil
						}
						sawStream = true
						p.Send(tui.ToolProgressMsg{
							Phase:  event.Phase,
							Tool:   event.Tool,
							ID:     event.ID,
							Args:   event.Args,
							Output: event.Output,
							Error:  event.Error,
						})
						return nil
					},
				})
				if err != nil {
					return "", err
				}
				attachments := pendingAttachments
				pendingAttachments = nil
				runCtx, cancel := context.WithCancel(context.Background())
				runState := &activeRun{cancel: cancel}
				activeMu.Lock()
				active = runState
				activeMu.Unlock()
				defer func() {
					activeMu.Lock()
					if active == runState {
						active = nil
					}
					activeMu.Unlock()
					cancel()
				}()

				result, err := runnerWithOutput.RunConversationWithAttachments(runCtx, saved.Messages, input, attachments)
				saved.Messages = result.Messages
				if saveErr := store.Save(saved); saveErr != nil {
					return "", saveErr
				}
				captured := strings.TrimSpace(output.String())
				debug := formatContextDebug(result.ContextDebug)
				if sawStream {
					return strings.TrimSpace(debug), err
				}
				if strings.Contains(captured, "tool:") || strings.Contains(captured, "tool error:") || strings.Contains(captured, "usage:") {
					return strings.TrimSpace(output.String() + "\n\n" + debug), err
				}
				return strings.TrimSpace(result.Final + "\n\n" + debug), err
			}

			tuiModel := tui.New(tui.Config{
				CWD:                    opts.cwd,
				Title:                  "Klyra",
				SessionID:              saved.ID,
				Provider:               runtimeCfg.Provider,
				Model:                  runtimeCfg.Model,
				BaseURL:                providerBaseURL(runtimeCfg, runtimeCfg.Provider),
				Reasoning:              runtimeCfg.Reasoning,
				Sandbox:                runtimeCfg.Sandbox,
				Approval:               runtimeCfg.ApprovalMode,
				Mode:                   runtimeCfg.Mode,
				StoreResponses:         runtimeCfg.StoreResponses,
				CartCount:              len(runtimeCfg.ContextFiles),
				MaxContext:             runtimeCfg.MaxContext,
				MaxOutput:              runtimeCfg.MaxOutput,
				MaxSteps:               runtimeCfg.MaxSteps,
				MaxMessages:            runtimeCfg.MaxMessages,
				MaxInstructions:        runtimeCfg.MaxInstructions,
				ContextCockpit:         runtimeCfg.ContextCockpit,
				ContextCockpitInject:   runtimeCfg.ContextCockpitInject,
				ContextCockpitTokens:   runtimeCfg.ContextCockpitTokens,
				ContextCockpitMaxFiles: runtimeCfg.ContextCockpitMaxFiles,
				ContextCockpitDiff:     runtimeCfg.ContextCockpitDiff,
				ContextRecipes:         runtimeCfg.ContextRecipes,
				NegativeContext:        runtimeCfg.NegativeContext,
				FastModel:              runtimeCfg.ModelRoutes["fast"],
				EditModel:              runtimeCfg.ModelRoutes["edit"],
				DeepModel:              runtimeCfg.ModelRoutes["deep"],
				Handler:                handler,
				Interrupt: func() bool {
					activeMu.Lock()
					runState := active
					activeMu.Unlock()
					if runState == nil || runState.cancel == nil {
						return false
					}
					runState.cancel()
					return true
				},
				PickerProvider: func(field string) (tui.PickerModal, error) {
					switch field {
					case "session":
						return tuiSessionPicker(store, saved.ID)
					case "checkpoint_restore":
						return tuiCheckpointRestorePicker(opts.cwd)
					default:
						return tui.PickerModal{}, fmt.Errorf("unknown picker %q", field)
					}
				},
				Commands:     tuiCommands,
				InitialLines: tuiLinesFromMessages(saved.Messages),
				SidebarFiles: tuiSidebarFiles(opts.cwd),
				SidebarDiff:  tuiSidebarDiff(opts.cwd),
			})
			p = tea.NewProgram(tuiModel, tea.WithAltScreen(), tea.WithMouseCellMotion())
			_, err = p.Run()
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

func tuiSidebarFiles(cwd string) []string {
	result, err := (tools.ListFiles{}).Run(context.Background(), tools.Invocation{CWD: cwd, Args: map[string]any{"max_files": 80}})
	if err != nil {
		return nil
	}
	var files []string
	for _, line := range strings.Split(result.Output, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			files = append(files, line)
		}
	}
	return files
}

func tuiSidebarDiff(cwd string) string {
	result, err := (tools.GitDiff{}).Run(context.Background(), tools.Invocation{CWD: cwd, Args: map[string]any{"max_lines": 120}})
	if err != nil {
		status, statusErr := (tools.GitStatus{}).Run(context.Background(), tools.Invocation{CWD: cwd, Args: map[string]any{}})
		if statusErr == nil {
			return status.Output
		}
		return ""
	}
	return result.Output
}

func tuiSessionPicker(store *session.Store, currentID string) (tui.PickerModal, error) {
	sessions, err := store.List()
	if err != nil {
		return tui.PickerModal{}, err
	}
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].UpdatedAt.After(sessions[j].UpdatedAt)
	})

	seen := map[string]bool{}
	options := make([]tui.PickerOption, 0, len(sessions)+1)
	for _, saved := range sessions {
		if strings.TrimSpace(saved.ID) == "" {
			continue
		}
		seen[saved.ID] = true
		options = append(options, tui.PickerOption{
			Value:       saved.ID,
			Label:       saved.ID,
			Description: fmt.Sprintf("%d messages · updated %s", len(saved.Messages), saved.UpdatedAt.Format("2006-01-02 15:04")),
		})
	}
	if strings.TrimSpace(currentID) != "" && !seen[currentID] {
		options = append([]tui.PickerOption{{
			Value:       currentID,
			Label:       currentID,
			Description: "current session · not saved yet",
		}}, options...)
	}
	if len(options) == 0 {
		return tui.PickerModal{}, fmt.Errorf("no saved sessions")
	}
	return tui.SessionPicker(currentID, options), nil
}

func tuiCheckpointRestorePicker(cwd string) (tui.PickerModal, error) {
	result, err := (tools.WorkspaceCheckpointList{}).Run(context.Background(), tools.Invocation{CWD: cwd, Args: map[string]any{}})
	if err != nil {
		return tui.PickerModal{}, err
	}
	var options []tui.PickerOption
	for _, line := range strings.Split(result.Output, "\n") {
		id := strings.TrimSpace(line)
		if id == "" || id == "no checkpoints" {
			continue
		}
		options = append(options, tui.PickerOption{
			Value:       id,
			Label:       id,
			Description: "restore workspace files from this checkpoint",
		})
	}
	if len(options) == 0 {
		return tui.PickerModal{}, fmt.Errorf("no checkpoints")
	}
	return tui.CheckpointRestorePicker(options), nil
}

func tuiLinesFromMessages(messages []llm.Message) []string {
	var lines []string
	for _, message := range messages {
		content := strings.TrimSpace(message.Content)
		if content == "" {
			continue
		}
		if len(lines) > 0 {
			lines = append(lines, "")
		}
		switch message.Role {
		case llm.RoleUser:
			lines = append(lines, "you: "+content)
		case llm.RoleAssistant:
			if strings.TrimSpace(message.Reasoning) != "" {
				lines = append(lines, "thoughts:0:"+message.Reasoning)
			}
			lines = append(lines, "agent: "+content)
		case llm.RoleTool:
			lines = append(lines, "tool:0:"+content)
		}
	}
	return lines
}

func formatTUISettings(cfg appconfig.Config, attachments []llm.Attachment) string {
	var builder strings.Builder
	builder.WriteString("Settings\n\n")
	fmt.Fprintf(&builder, "- provider: `%s`\n", valueOrString(cfg.Provider, "mock"))
	fmt.Fprintf(&builder, "- model: `%s`\n", valueOrString(cfg.Model, "provider env / routed"))
	fmt.Fprintf(&builder, "- endpoint: `%s`\n", valueOrString(providerBaseURL(cfg, cfg.Provider), "provider default/env"))
	fmt.Fprintf(&builder, "- reasoning: `%s`\n", valueOrString(cfg.Reasoning, "default"))
	fmt.Fprintf(&builder, "- sandbox: `%s`\n", valueOrString(cfg.Sandbox, "workspace-write"))
	fmt.Fprintf(&builder, "- mode: `%s`\n", valueOrString(cfg.Mode, "edit"))
	fmt.Fprintf(&builder, "- provider store: `%s`\n", onOff(cfg.StoreResponses))
	fmt.Fprintf(&builder, "- context cart: `%d files`\n", len(cfg.ContextFiles))
	fmt.Fprintf(&builder, "- approval: `%s`\n", valueOrString(cfg.ApprovalMode, "auto"))
	fmt.Fprintf(&builder, "- max context tokens: `%d`\n", cfg.MaxContext)
	fmt.Fprintf(&builder, "- max output tokens: `%d`\n", cfg.MaxOutput)
	fmt.Fprintf(&builder, "- max steps: `%d`\n", cfg.MaxSteps)
	fmt.Fprintf(&builder, "- max messages: `%d`\n", cfg.MaxMessages)
	fmt.Fprintf(&builder, "- max instruction bytes: `%d`\n", cfg.MaxInstructions)
	fmt.Fprintf(&builder, "- context cockpit: `%s`\n", onOff(cfg.ContextCockpit))
	fmt.Fprintf(&builder, "- cockpit inject: `%s`\n", onOff(cfg.ContextCockpitInject))
	fmt.Fprintf(&builder, "- cockpit budget: `%d tokens / %d files`\n", cfg.ContextCockpitTokens, cfg.ContextCockpitMaxFiles)
	fmt.Fprintf(&builder, "- cockpit diff: `%s`\n", onOff(cfg.ContextCockpitDiff))
	fmt.Fprintf(&builder, "- context recipes: `%s`\n", onOff(cfg.ContextRecipes))
	fmt.Fprintf(&builder, "- negative context: `%s`\n", onOff(cfg.NegativeContext))
	fmt.Fprintf(&builder, "- pending images: `%d`\n", len(attachments))
	builder.WriteString("\nUse `/provider`, `/model`, `/reasoning`, `/limits`, `/approval`, `/sandbox`, `/mode`, `/cart add`, `/context`, and `/attach` to change this without leaving Klyra.")
	return builder.String()
}

func formatSettingSaved(name, value string) string {
	return fmt.Sprintf("setting saved: %s = `%s`", name, valueOrString(value, "default"))
}

func applyTUISet(cfg *appconfig.Config, args []string) error {
	for _, arg := range args {
		key, value, ok := strings.Cut(arg, "=")
		if !ok {
			return fmt.Errorf("settings value must use key=value: %s", arg)
		}
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "provider":
			cfg.Provider = value
			cfg.Model = ""
			if value == "mock" {
				cfg.Model = "mock-agent"
			}
		case "model":
			cfg.Model = value
		case "endpoint", "base_url", "base-url":
			setProviderBaseURL(cfg, cfg.Provider, value)
		case "reasoning":
			cfg.Reasoning = value
		case "approval":
			cfg.ApprovalMode = value
		case "sandbox":
			cfg.Sandbox = value
		case "mode":
			cfg.Mode = value
		case "store", "store_responses", "store-responses":
			parsed, err := parseBoolSetting(value)
			if err != nil {
				return fmt.Errorf("store must be on/off")
			}
			cfg.StoreResponses = parsed
		case "context":
			parsed, err := strconv.Atoi(value)
			if err != nil || parsed <= 0 {
				return fmt.Errorf("context must be a positive integer")
			}
			cfg.MaxContext = parsed
		case "output":
			parsed, err := strconv.Atoi(value)
			if err != nil || parsed <= 0 {
				return fmt.Errorf("output must be a positive integer")
			}
			cfg.MaxOutput = parsed
		case "steps":
			parsed, err := strconv.Atoi(value)
			if err != nil || parsed <= 0 {
				return fmt.Errorf("steps must be a positive integer")
			}
			cfg.MaxSteps = parsed
		case "messages":
			parsed, err := strconv.Atoi(value)
			if err != nil || parsed <= 0 {
				return fmt.Errorf("messages must be a positive integer")
			}
			cfg.MaxMessages = parsed
		case "instructions", "instruction_bytes", "instruction-bytes":
			parsed, err := strconv.Atoi(value)
			if err != nil || parsed <= 0 {
				return fmt.Errorf("instructions must be a positive integer")
			}
			cfg.MaxInstructions = parsed
		case "context_cockpit", "cockpit":
			parsed, err := parseBoolSetting(value)
			if err != nil {
				return fmt.Errorf("context_cockpit must be on/off")
			}
			cfg.ContextCockpit = parsed
		case "context_cockpit_inject", "cockpit_inject":
			parsed, err := parseBoolSetting(value)
			if err != nil {
				return fmt.Errorf("context_cockpit_inject must be on/off")
			}
			cfg.ContextCockpitInject = parsed
		case "context_cockpit_diff", "cockpit_diff":
			parsed, err := parseBoolSetting(value)
			if err != nil {
				return fmt.Errorf("context_cockpit_diff must be on/off")
			}
			cfg.ContextCockpitDiff = parsed
		case "context_cockpit_tokens", "cockpit_tokens":
			parsed, err := strconv.Atoi(value)
			if err != nil || parsed <= 0 {
				return fmt.Errorf("context_cockpit_tokens must be a positive integer")
			}
			cfg.ContextCockpitTokens = parsed
		case "context_cockpit_files", "cockpit_files":
			parsed, err := strconv.Atoi(value)
			if err != nil || parsed <= 0 {
				return fmt.Errorf("context_cockpit_files must be a positive integer")
			}
			cfg.ContextCockpitMaxFiles = parsed
		case "context_recipes", "recipes":
			parsed, err := parseBoolSetting(value)
			if err != nil {
				return fmt.Errorf("context_recipes must be on/off")
			}
			cfg.ContextRecipes = parsed
		case "negative_context", "negative":
			parsed, err := parseBoolSetting(value)
			if err != nil {
				return fmt.Errorf("negative_context must be on/off")
			}
			cfg.NegativeContext = parsed
		default:
			return fmt.Errorf("unknown setting %q", key)
		}
	}
	return nil
}

func formatContextCart(files []string) string {
	if len(files) == 0 {
		return "Context Cart\n\n*Cart is empty. Use `/cart add <file>` to attach files.*"
	}
	var builder strings.Builder
	builder.WriteString("Context Cart\n\n")
	for _, file := range files {
		fmt.Fprintf(&builder, "- `%s`\n", file)
	}
	return builder.String()
}

func formatContextCockpit(cfg appconfig.Config, cwd, focus string) (string, error) {
	snapshot, err := cockpit.Build(context.Background(), cockpit.Config{
		Enabled:         cfg.ContextCockpit,
		Inject:          cfg.ContextCockpitInject,
		MaxTokens:       cfg.ContextCockpitTokens,
		MaxFiles:        cfg.ContextCockpitMaxFiles,
		IncludeDiff:     cfg.ContextCockpitDiff,
		IncludeRecipes:  cfg.ContextRecipes,
		IncludeNegative: cfg.NegativeContext,
		MaxInstructions: cfg.MaxInstructions,
	}, cwd, focus, cfg.ContextFiles)
	if err != nil {
		return "", err
	}
	return "Context Cockpit\n\n" + snapshot.Markdown(), nil
}

func printContextDebug(out io.Writer, debug agent.ContextDebug) {
	text := formatContextDebug(debug)
	if strings.TrimSpace(text) != "" {
		fmt.Fprintln(out, text)
	}
}

func formatContextDebug(debug agent.ContextDebug) string {
	if debug.Mode == "" && len(debug.VisibleTools) == 0 && len(debug.Risks) == 0 {
		return ""
	}
	var builder strings.Builder
	builder.WriteString("Context Debugger\n\n")
	fmt.Fprintf(&builder, "- mode: `%s`\n", valueOrString(debug.Mode, "edit"))
	if len(debug.ContextFiles) == 0 {
		builder.WriteString("- context cart: empty\n")
	} else {
		builder.WriteString("- context cart:\n")
		for _, file := range debug.ContextFiles {
			builder.WriteString("  - `" + file + "`\n")
		}
	}
	if len(debug.VisibleTools) > 0 {
		builder.WriteString("- model could use: `" + strings.Join(debug.VisibleTools, "`, `") + "`\n")
	}
	if len(debug.Risks) > 0 {
		builder.WriteString("- risks:\n")
		for _, risk := range debug.Risks {
			builder.WriteString("  - " + risk + "\n")
		}
	}
	if strings.TrimSpace(debug.Cockpit) != "" {
		fmt.Fprintf(&builder, "\nContext Cockpit\n\n- estimated tokens: `%d`\n\n%s\n", debug.CockpitTokens, debug.Cockpit)
	}
	return strings.TrimSpace(builder.String())
}

func applyTUILimit(cfg *appconfig.Config, args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: /limits context|output|steps|messages|instructions <number>")
	}
	value, err := strconv.Atoi(args[1])
	if err != nil || value <= 0 {
		return fmt.Errorf("limit must be a positive integer")
	}
	switch strings.ToLower(args[0]) {
	case "context", "ctx":
		cfg.MaxContext = value
	case "output", "out":
		cfg.MaxOutput = value
	case "steps":
		cfg.MaxSteps = value
	case "messages":
		cfg.MaxMessages = value
	case "instructions", "instruction-bytes":
		cfg.MaxInstructions = value
	default:
		return fmt.Errorf("unknown limit %q", args[0])
	}
	return nil
}

func loadImageAttachment(cwd, path string) (llm.Attachment, error) {
	path = strings.Trim(path, "\"'")
	target := path
	if !filepath.IsAbs(target) {
		target = filepath.Join(cwd, target)
	}
	data, err := os.ReadFile(target)
	if err != nil {
		return llm.Attachment{}, err
	}
	mimeType := mime.TypeByExtension(strings.ToLower(filepath.Ext(target)))
	if idx := strings.Index(mimeType, ";"); idx >= 0 {
		mimeType = mimeType[:idx]
	}
	if !strings.HasPrefix(mimeType, "image/") {
		return llm.Attachment{}, fmt.Errorf("%s is not a supported image file", path)
	}
	return llm.Attachment{
		Type:     "image",
		MIMEType: mimeType,
		Name:     filepath.Base(target),
		Data:     base64.StdEncoding.EncodeToString(data),
	}, nil
}

func formatAttachments(attachments []llm.Attachment) string {
	if len(attachments) == 0 {
		return "Pending Attachments\n\n*No pending image attachments.*"
	}
	var builder strings.Builder
	builder.WriteString("Pending Attachments\n\n")
	for i, attachment := range attachments {
		fmt.Fprintf(&builder, "%d. `%s` (%s, `%d` bytes)\n", i+1, attachment.Name, attachment.MIMEType, len(attachment.Data))
	}
	return builder.String()
}

func valueOrString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func onOff(value bool) string {
	if value {
		return "on"
	}
	return "off"
}

func parseBoolSetting(value string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on", "enable", "enabled":
		return true, nil
	case "0", "false", "no", "off", "disable", "disabled":
		return false, nil
	default:
		return false, fmt.Errorf("invalid boolean value %q", value)
	}
}

func joinNonEmpty(parts ...string) string {
	var out []string
	for _, part := range parts {
		if strings.TrimSpace(part) != "" {
			out = append(out, strings.TrimSpace(part))
		}
	}
	return strings.Join(out, "\n\n")
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
			provider, model, err := buildProviderFromConfig(runtimeCfg)
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
				CWD:                    opts.cwd,
				Model:                  model,
				ModelRoutes:            runtimeCfg.ModelRoutes,
				MaxSteps:               runtimeCfg.MaxSteps,
				MaxMessages:            runtimeCfg.MaxMessages,
				MaxContext:             runtimeCfg.MaxContext,
				MaxInstructions:        runtimeCfg.MaxInstructions,
				MaxOutput:              runtimeCfg.MaxOutput,
				Reasoning:              runtimeCfg.Reasoning,
				Store:                  runtimeCfg.StoreResponses,
				Stream:                 opts.stream,
				ApprovalMode:           runtimeCfg.ApprovalMode,
				Sandbox:                runtimeCfg.Sandbox,
				Mode:                   runtimeCfg.Mode,
				ContextFiles:           runtimeCfg.ContextFiles,
				ContextCockpitEnabled:  runtimeCfg.ContextCockpit,
				ContextCockpitInject:   runtimeCfg.ContextCockpitInject,
				ContextCockpitTokens:   runtimeCfg.ContextCockpitTokens,
				ContextCockpitMaxFiles: runtimeCfg.ContextCockpitMaxFiles,
				ContextCockpitDiff:     runtimeCfg.ContextCockpitDiff,
				ContextRecipes:         runtimeCfg.ContextRecipes,
				NegativeContext:        runtimeCfg.NegativeContext,
				Provider:               provider,
				Input:                  os.Stdin,
				Output:                 cmd.OutOrStdout(),
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
				printContextDebug(cmd.OutOrStdout(), result.ContextDebug)
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

func newInstructionsCommand(opts *options) *cobra.Command {
	var showContent bool
	cmd := &cobra.Command{
		Use:   "instructions",
		Short: "Show project instruction files loaded into the system prompt",
		RunE: func(cmd *cobra.Command, _ []string) error {
			runtimeCfg, err := effectiveConfig(cmd, *opts)
			if err != nil {
				return err
			}
			result, err := instructions.Load(opts.cwd, runtimeCfg.MaxInstructions)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if len(result.Files) == 0 {
				fmt.Fprintln(out, "no project instructions found")
				return nil
			}
			for _, file := range result.Files {
				suffix := ""
				if file.Truncated {
					suffix = " truncated"
				}
				fmt.Fprintf(out, "%s bytes=%d%s\n", file.Path, file.Bytes, suffix)
			}
			if result.Truncated {
				fmt.Fprintf(out, "instruction budget reached: %d bytes\n", runtimeCfg.MaxInstructions)
			}
			if showContent {
				fmt.Fprintln(out, "\ncontent:")
				fmt.Fprintln(out, result.Content)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&showContent, "content", false, "print loaded instruction content")
	return cmd
}

func newDoctorCommand(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check local runtime support for agentcli",
		RunE: func(cmd *cobra.Command, _ []string) error {
			runtimeCfg, err := effectiveConfig(cmd, *opts)
			if err != nil {
				return err
			}
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
			printEnvStatus(out, "GEMINI_API_KEY")
			printEnvStatus(out, "GEMINI_MODEL")
			printEnvStatus(out, "GEMINI_BASE_URL")
			projectInstructions, err := instructions.Load(opts.cwd, runtimeCfg.MaxInstructions)
			if err != nil {
				return err
			}
			if len(projectInstructions.Files) == 0 {
				fmt.Fprintln(out, "project_instructions: none")
			} else {
				names := make([]string, 0, len(projectInstructions.Files))
				for _, file := range projectInstructions.Files {
					names = append(names, file.Path)
				}
				fmt.Fprintf(out, "project_instructions: %s (%d bytes)\n", strings.Join(names, ", "), projectInstructions.Bytes)
			}
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
	loadEnvFile(opts.cwd)
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
	if flags.Changed("base-url") {
		setProviderBaseURL(&cfg, cfg.Provider, opts.baseURL)
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
	if flags.Changed("max-instruction-bytes") {
		cfg.MaxInstructions = opts.maxInstructions
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
	if flags.Changed("mode") {
		cfg.Mode = opts.mode
	}
	if flags.Changed("context-file") {
		cfg.ContextFiles = append([]string(nil), opts.contextFiles...)
	}
	if flags.Changed("context-cockpit") {
		cfg.ContextCockpit = opts.contextCockpit
	}
	if flags.Changed("no-context-cockpit") {
		cfg.ContextCockpit = !opts.noContextCockpit
	}
	if flags.Changed("context-cockpit-inject") {
		cfg.ContextCockpitInject = opts.contextCockpitInject
	}
	if flags.Changed("no-context-cockpit-inject") {
		cfg.ContextCockpitInject = !opts.noContextCockpitInject
	}
	if flags.Changed("context-cockpit-tokens") {
		cfg.ContextCockpitTokens = opts.contextCockpitTokens
	}
	if flags.Changed("context-cockpit-files") {
		cfg.ContextCockpitMaxFiles = opts.contextCockpitMaxFiles
	}
	if flags.Changed("context-recipes") {
		cfg.ContextRecipes = opts.contextRecipes
	}
	if flags.Changed("no-context-recipes") {
		cfg.ContextRecipes = !opts.noContextRecipes
	}
	if flags.Changed("negative-context") {
		cfg.NegativeContext = opts.negativeContext
	}
	if flags.Changed("no-negative-context") {
		cfg.NegativeContext = !opts.noNegativeContext
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
	return buildProviderWithBaseURL(name, model, "")
}

func buildProviderFromConfig(cfg appconfig.Config) (llm.Provider, string, error) {
	return buildProviderWithBaseURL(cfg.Provider, cfg.Model, providerBaseURL(cfg, cfg.Provider))
}

func buildProviderWithBaseURL(name, model, baseURL string) (llm.Provider, string, error) {
	normalized := strings.ToLower(strings.TrimSpace(name))
	switch normalized {
	case "", "mock":
		if strings.TrimSpace(model) == "" {
			model = "mock-agent"
		}
		return llm.NewMockProvider(), model, nil
	case "openai", "responses":
		if strings.TrimSpace(baseURL) == "" {
			baseURL = os.Getenv("OPENAI_BASE_URL")
		}
		provider, err := llm.NewResponsesProvider(os.Getenv("OPENAI_API_KEY"), baseURL)
		if err != nil {
			return nil, "", err
		}
		if strings.TrimSpace(model) == "" || model == "mock-agent" {
			model = os.Getenv("OPENAI_MODEL")
		}
		return provider, model, nil
	case "local", "chat", "chat-completions", "openai-chat", "openai-compatible":
		if strings.TrimSpace(baseURL) == "" {
			baseURL = os.Getenv("LOCAL_BASE_URL")
		}
		if strings.TrimSpace(baseURL) == "" {
			baseURL = os.Getenv("OPENAI_BASE_URL")
		}
		if strings.TrimSpace(baseURL) == "" && normalized == "local" {
			// Sensible default for local testing if not using Ollama
			baseURL = "http://localhost:8080/v1"
		}
		provider, err := llm.NewOpenAIProvider(os.Getenv("OPENAI_API_KEY"), baseURL)
		if err != nil {
			return nil, "", err
		}
		if strings.TrimSpace(model) == "" || model == "mock-agent" {
			model = os.Getenv("OPENAI_MODEL")
			if model == "" {
				model = "local-model" // fallback so it doesn't fail
			}
		}
		return provider, model, nil
	case "ollama":
		if strings.TrimSpace(baseURL) == "" {
			baseURL = os.Getenv("OLLAMA_BASE_URL")
		}
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
		if strings.TrimSpace(baseURL) == "" {
			baseURL = os.Getenv("ANTHROPIC_BASE_URL")
		}
		provider, err := llm.NewAnthropicProvider(os.Getenv("ANTHROPIC_API_KEY"), baseURL)
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
	case "gemini", "google":
		if strings.TrimSpace(baseURL) == "" {
			baseURL = os.Getenv("GEMINI_BASE_URL")
		}
		provider, err := llm.NewGeminiProvider(os.Getenv("GEMINI_API_KEY"), baseURL)
		if err != nil {
			return nil, "", err
		}
		if strings.TrimSpace(model) == "" || model == "mock-agent" {
			model = os.Getenv("GEMINI_MODEL")
		}
		if strings.TrimSpace(model) == "" {
			return nil, "", fmt.Errorf("model is required for provider %q; pass --model or set GEMINI_MODEL", normalized)
		}
		return provider, model, nil
	default:
		return nil, "", fmt.Errorf("provider %q is not implemented yet", name)
	}
}

func providerBaseURL(cfg appconfig.Config, provider string) string {
	if cfg.BaseURLs == nil {
		return ""
	}
	return cfg.BaseURLs[strings.ToLower(strings.TrimSpace(provider))]
}

func setProviderBaseURL(cfg *appconfig.Config, provider, baseURL string) {
	provider = strings.ToLower(strings.TrimSpace(provider))
	if provider == "" {
		provider = "openai"
	}
	if cfg.BaseURLs == nil {
		cfg.BaseURLs = map[string]string{}
	}
	cfg.BaseURLs[provider] = strings.TrimSpace(baseURL)
}

func loadEnvFile(dir string) {
	if dir == "" {
		dir = "."
	}
	path := filepath.Join(dir, ".env")
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)
		if (strings.HasPrefix(val, "\"") && strings.HasSuffix(val, "\"")) ||
			(strings.HasPrefix(val, "'") && strings.HasSuffix(val, "'")) {
			if len(val) >= 2 {
				val = val[1 : len(val)-1]
			}
		}
		if key != "" && os.Getenv(key) == "" {
			_ = os.Setenv(key, val)
		}
	}
}
