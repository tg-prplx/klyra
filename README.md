# Agent CLI

Agentic coding CLI prototype in Go.

## Run locally

```sh
go run . run "inspect this project"
```

The default provider is `mock`, so the command works without API keys and is useful for testing the agent loop and tools.

## OpenAI-compatible provider

The `openai` provider uses the modern Responses API. The legacy Chat Completions-compatible path is still available as `chat`.

```sh
export OPENAI_API_KEY="..."
export OPENAI_MODEL="..."
go run . --provider openai run "inspect this project and suggest next steps"
```

Optional:

```sh
export OPENAI_BASE_URL="https://api.openai.com/v1"
```

Useful generation controls:

```sh
go run . --provider openai --model "$OPENAI_MODEL" --reasoning low --max-output-tokens 2048 run "fix the failing tests"
```

`--store` is disabled by default so local coding sessions stay stateless unless you opt in.

## Local models with Ollama

```sh
export OLLAMA_MODEL="qwen2.5-coder:7b"
go run . --provider ollama run "inspect this project"
```

Optional:

```sh
export OLLAMA_BASE_URL="http://localhost:11434/v1"
```

## Anthropic provider

```sh
export ANTHROPIC_API_KEY="..."
export ANTHROPIC_MODEL="claude-sonnet-4-5"
go run . --provider anthropic run "inspect this project"
```

Optional:

```sh
export ANTHROPIC_BASE_URL="https://api.anthropic.com"
```

## Support commands

```sh
go run . config init
go run . config show --profile coding
go run . doctor
go run . tools
go run . status --diff
go run . checkpoint create before-refactor
go run . checkpoint list
go run . diff preview patch.diff
go run . diff apply patch.diff --yes
go run . policy check "git status --short"
go run . policy check --sandbox read-only "echo hello > file.txt"
go run . sessions
go run . sessions compact feature-work
```

## Interactive sessions

```sh
go run . chat --session feature-work
go run . tui --session feature-work
go run . --session feature-work run "continue the refactor"
```

Sessions are stored under `.agentcli/sessions` in the workspace and are ignored by git.
Inside chat, `/compact` rewrites older history into a compact deterministic summary so future turns spend fewer context tokens.

The Bubble Tea TUI supports `/help`, `/status`, `/compact`, `/clear`, and `/exit`.

## Approval policy

Risky tools (`bash`, `write_file`, `diff_patch`) support approval modes:

```sh
go run . --approval ask run "fix the failing tests"
go run . --approval never run "inspect only"
```

Checkpoint restore is explicit:

```sh
go run . checkpoint restore before-refactor
```

Diff preview validates a patch without applying it:

```sh
cat patch.diff | go run . diff preview
```

Diff apply always validates first and creates a workspace checkpoint by default:

```sh
go run . diff apply patch.diff
go run . diff apply patch.diff --yes --checkpoint=false
```

Shell policy explains how a command will be treated before an agent runs it:

```sh
go run . policy check "git reset --hard HEAD"
```

Sandbox profiles control what tools may do:

```sh
go run . --sandbox read-only run "inspect the project"
go run . --sandbox workspace-write run "fix a typo"
go run . --sandbox danger-full-access run "fetch dependencies"
```

`read-only` blocks write tools and non-read shell commands. `workspace-write` blocks network and destructive shell commands. `danger-full-access` leaves policy decisions to approval mode and the user.

## Model routing

Use cheaper models for inspection and stronger models for edits or deep reasoning:

```sh
go run . --provider openai \
  --stream \
  --max-context-tokens 32000 \
  --fast-model "$FAST_MODEL" \
  --edit-model "$CODING_MODEL" \
  --deep-model "$REASONING_MODEL" \
  run "inspect the project and propose next steps"
```

`--stream` uses Responses API SSE when available and falls back to normal completion for providers that do not support streaming.

## Context compaction

Agent CLI estimates prompt tokens locally and packs context before provider calls. It preserves the system prompt, keeps recent turns, drops orphan tool outputs, and inserts a compact summary when older history would exceed `--max-context-tokens`.

## Implemented tools

- `project_map`: compact language/file map for low-token project discovery.
- `list_files`: lists workspace files while skipping common generated directories.
- `read_file`: reads files with line slicing.
- `read_go_symbol`: reads a Go declaration by symbol name without loading the whole file.
- `write_file`: writes complete files inside the workspace.
- `search`: searches with `rg`.
- `bash`: runs shell commands with timeout and output compression.
- `diff_patch`: applies unified diffs via `git apply`.

The agent prunes tool schemas per task so simple inspection requests do not pay for edit-only tools.
The agent can also call `policy_check` before risky shell commands; destructive shell patterns are blocked in `--approval auto`.

## Verification

```sh
go test ./...
go build ./...
```
