<img width="1440" height="856" alt="image" src="https://github.com/user-attachments/assets/10f65a45-1867-42dc-bb02-d0016f9c21e9" />



# Klyra

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

Ollama vision-capable models can receive images from the TUI via `/attach path/to/image.png`, using the OpenAI-compatible `image_url` message format.

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

## Gemini provider

```sh
export GEMINI_API_KEY="..."
export GEMINI_MODEL="gemini-2.5-flash"
go run . --provider gemini run "inspect this project"
```

Optional:

```sh
export GEMINI_BASE_URL="https://generativelanguage.googleapis.com/v1beta"
```

## Support commands

```sh
go run . config init
go run . config show --profile coding
go run . doctor
go run . tools
go run . instructions
go run . instructions --content
go run . skills --all
go run . skills --query "frontend css" --content
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

The Klyra Bubble Tea TUI is intended as the primary work surface. It supports `/help`, `/status`, `/settings`, `/provider`, `/model`, `/reasoning`, `/limits`, `/approval`, `/sandbox`, `/attach`, `/attachments`, `/instructions`, `/skills`, `/compact`, `/clear`, and `/exit`.

Press `F2` or `Ctrl+S` to open the settings panel. Use `Tab` to move between fields, left/right arrows to choose provider/reasoning/approval/sandbox values, type directly into text fields such as model or endpoint, then press `Enter` to apply the runtime settings.

Example TUI flow:

```text
/provider ollama
/model llama3.2-vision
/endpoint http://localhost:11434/v1
/reasoning low
/limits context 16000
/attach screenshots/error.png
explain this screenshot and inspect the relevant code
```

When approval mode is `ask`, risky tool calls appear as an in-app approval prompt. Press `y`/`Enter` to approve or `n`/`Esc` to reject. Use `always` to allow tool calls without prompts.

Image attachments are sent only with the next model request and are not stored as base64 in session history, keeping future turns cheap.

## Modes and context cart

Klyra has real mode constraints, not just labels:

- `plan`: read-only exploration and web retrieval with an optional structured `update_plan`; shell, writes, patches, and external MCP tools are hidden/blocked.
- `inspect`: retrieval only; write tools and shell are hidden/blocked.
- `edit`: write tools require files in the context cart.
- `repair`: keeps the agent focused on failing output, relevant code, and current diff.
- `refactor`: exposes preview/search paths and requires explicit context cart before broad patches.

Use CLI flags or TUI commands:

```sh
go run . --mode plan run "plan the auth refactor"
go run . --mode inspect run "map the auth flow"
go run . --mode edit --context-file pkg/auth/middleware.go run "fix the auth bug"
```

```text
/mode edit
/cart add pkg/auth/middleware.go pkg/auth/login_test.go
```

After a turn, the context debugger reports the mode, context cart, tools the model could see, and risks such as “edit mode has no cart”. This is meant to make weak-model answers safer without automatically stuffing more files into context.

## Vision

Vision attachments are supported for OpenAI Responses, Anthropic, Gemini, and OpenAI-compatible chat providers such as Ollama. Attachments are encoded as provider-native image parts and stripped from saved history after the turn.


## Project instructions

Agent CLI automatically loads common repository instruction files into the system prompt, capped by `--max-instruction-bytes`:

- `AGENTS.md`
- `CLAUDE.md`
- `GEMINI.md`
- `.agentcli/instructions.md`
- `.agentcli/rules.md`
- `.cursorrules`
- `.github/copilot-instructions.md`
- `.cursor/rules/*.md`

Use `go run . instructions --content` to inspect exactly what the agent will see.

## Skills

Skills are small task-specific markdown playbooks. Klyra auto-matches them by task text and context-cart paths, then injects only matched skills into the system prompt.

Supported locations:

- `.klyra/skills/*.md`
- `.klyra/skills/*/SKILL.md`
- `.agentcli/skills/*.md`
- `.agentcli/skills/*/SKILL.md`
- `skills/*.md`
- `skills/*/SKILL.md`

Optional metadata:

```md
name: Frontend Cleanup
description: CSS and UI cleanup rules
triggers: frontend, css, style

Use focused edits and avoid glassmorphism.
```

Use `go run . skills --all` to list skills, or `go run . skills --query "migration sql" --content` to inspect matches. Disable injection with `--no-skills` or `skills=off` in TUI settings.

## Approval policy

Risky tools (`bash`, `write_file`, `diff_patch`, focused write tools, and checkpoint restore) support approval modes:

```sh
go run . --approval ask run "fix the failing tests"
go run . --approval always run "apply the known local fix"
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
  --max-instruction-bytes 12000 \
  --fast-model "$FAST_MODEL" \
  --edit-model "$CODING_MODEL" \
  --deep-model "$REASONING_MODEL" \
  run "inspect the project and propose next steps"
```

Routing follows the explicit agent mode rather than guessing intent from task keywords: `inspect` uses the fast route, `edit`/`repair` use the edit route, and `plan`/`refactor` use the deep route.

`--stream` uses Responses API SSE when available and falls back to normal completion for providers that do not support streaming.

## Context compaction

Agent CLI estimates prompt tokens locally and packs context before provider calls. It preserves the system prompt, keeps recent turns, drops orphan tool outputs, and inserts a compact summary when older history would exceed `--max-context-tokens`.

The context cockpit also builds a small retrieval cart before each task. It ranks file chunks with BM25, AST repo-map hints, and local hash embeddings over words, identifier subtokens, and character n-grams, so semantic matches such as `token validation` -> `ValidateToken` can be found without a network embedding service.

## Implemented tools

- `discover_tools`: unlocks compact capability groups (`workspace`, `edit`, `git`, `shell`, `web`, `plan`, `external`) for the current run.
- `guide`: returns compact task-specific workflow guidance on demand, so unfamiliar work does not require injecting every playbook into the prompt.
- `project_map`: token-budgeted repo map for low-token discovery; includes important files and AST symbols.
- `list_files`: lists workspace files while skipping common generated directories.
- `read_file`: reads files with line slicing.
- `file_outline`: returns compact imports/symbols for one file.
- `read_symbol`: reads one AST symbol instead of a whole file.
- `read_go_symbol`: reads a Go declaration by symbol name without loading the whole file.
- `create_file`: creates new files only.
- `replace_symbol`, `replace_lines`, `insert_lines`: focused write tools for existing files.
- `write_file`: legacy full-file writer; hidden from normal edit prompts and blocked from overwriting existing files in edit/refactor/repair mode.
- `search`: searches with `rg`.
- `web_search`, `fetch_url`: searches public web and fetches pages; `fetch_url` can use a focus query to return only relevant page chunks via local retrieval.
- `update_plan`: records a short structured plan for plan mode or explicitly multi-step work.
- `bash`: runs shell commands with timeout and output compression.
- `diff_patch`: applies unified diffs via `git apply`.

The agent discloses tool schemas progressively without guessing task intent from keyword lists. A fresh run starts with compact `discover_tools`; the model requests only the capability groups needed for the task. Concrete paths, URLs, context-cart entries, and explicit agent modes act as structural shortcuts. Heavy patch/checkpoint tools still require a context cart.
The agent can also call `policy_check` before risky shell commands; destructive shell patterns are blocked in `--approval auto`.

## Verification

```sh
go test ./...
go build ./...
```
