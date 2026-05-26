# План разработки Agentic Coding CLI на Go

Этот документ описывает архитектуру и план реализации продвинутого Coding CLI на языке Go. Главный фокус — мощные агентные функции (на уровне Claude Code, OpenCode, Aider) при строгом контроле и минимизации потребления токенов.

## User Review Required

> [!IMPORTANT]
> **Выбор LLM-провайдера и моделей:** Будем ли мы поддерживать только облачные API (Anthropic, OpenAI, Gemini) или сразу закладываем поддержку локальных моделей (через Ollama)?
> **Пользовательский интерфейс:** Для CLI на Go стандартом де-факто для красивых интерфейсов является библиотека `charmbracelet/bubbletea`. Утверждаем ли мы ее использование для TUI (Terminal User Interface)?

## Open Questions

> [!WARNING]
> Какие инструменты (Tools) агент должен получить в первой версии (MVP)? 
> - Выполнение Bash команд в песочнице?
> - Чтение и поиск по файлам (grep/AST)?
> - Редактирование файлов (через diff, unified diff или replace)?

## Исследование рынка и подходы (на 2026 год)

Анализ современных инструментов (Claude Code, OpenCode, Codex CLI, Cursor) показывает, что для радикального снижения потребления токенов необходимо отказаться от подхода "загрузи весь файл в контекст" в пользу **Precision Retrieval (точечного извлечения)** и **Compression (сжатия)**.

### Ключевые техники экономии токенов для внедрения:

1. **CLI Output Compression (Проксирование команд):** Агенты часто выполняют команды вроде `git status` или тесты, вывод которых замусорен прогресс-барами и лишними логами. Мы реализуем прокси (подобно RTK), который будет урезать логи, удалять ANSI-цвета и оставлять только суть (например, только упавшие тесты), экономя **60-90% токенов** на bash-инструментах.
2. **Symbol-Level Retrieval (AST парсинг):** Вместо чтения целого файла на 2000 строк, CLI будет использовать `tree-sitter` (интегрированный в Go) для чтения конкретных функций, структур или классов. 
3. **Prompt Caching:** Системные инструкции, граф проекта и схемы инструментов будут фиксированы в начале промпта, чтобы максимально использовать префиксное кеширование провайдеров (Anthropic/OpenAI), снижая стоимость последующих обращений.
4. **Model Routing (Роутинг моделей):** Для простых задач (поиск по файлам, просмотр директорий) будет использоваться дешевая модель (напр. GPT-4o-mini, Haiku), а для сложного рефакторинга запрос будет эскалироваться до флагманских моделей (Claude 3.7 Sonnet / Opus 4.7).
5. **Dynamic Tool Pruning:** Не нужно посылать схему из 40 инструментов в каждом запросе. CLI будет динамически отправлять только те инструменты, которые имеют смысл в текущем контексте (например, инструменты работы с git только если мы в git-репозитории).

---

## Proposed Architecture (Архитектура на Go)

Проект будет разделен на следующие ключевые компоненты (пакеты):

### 1. `cmd/agentcli`
Точка входа приложения, парсинг флагов (с использованием `spf13/cobra`) и инициализация TUI (`charmbracelet/bubbletea`).

### 2. `pkg/agent` (Core Loop)
Основной цикл агента:
- Принятие задачи от пользователя.
- Генерация промпта и отправка в LLM.
- Парсинг ответов (Tool Calls).
- Исполнение инструментов и возврат результата в цикл.

### 3. `pkg/tools` (Инструменты)
- `BashRunner`: Выполнение команд с встроенным сжатием вывода (Output Compression).
- `FileReader` & `FileWriter`: Чтение и запись файлов.
- `ASTSearch`: Интеграция с `tree-sitter` для извлечения сигнатур функций и точечного чтения кода.
- `DiffPatcher`: Надежное применение патчей к коду.

### 4. `pkg/llm` (Провайдеры)
Универсальный интерфейс для работы с моделями (поддержка OpenAI, Anthropic API) с встроенной логикой **Model Routing** и **Prompt Caching**.

### 5. `pkg/context` (Менеджер контекста)
Управление историей сообщений, обрезка старых сообщений (sliding window) и упаковка контекста для максимизации cache hit rate.

## TUI Scrolling and Text Selection Fix

### 1. Viewport Integration
- Import `"github.com/charmbracelet/bubbles/viewport"` in `pkg/tui/model.go`.
- Add `viewport viewport.Model` to `Model` struct.
- In `New()`, initialize the viewport: `viewport: viewport.New(0, 0)`.
- In `Update()`, on `tea.WindowSizeMsg`, set `m.viewport.Width = msg.Width` and `m.viewport.Height = m.calculateBodyHeight()`.

### 2. Eliminating Idle Redraws (Fixing Text Selection Drops)
- Modify `spinnerTickMsg` handler to only return `tickSpinner()` when `m.busy` is true. When `m.busy` is false, do not tick the spinner anymore.
- Trigger `tickSpinner()` when `m.busy` is set to true (e.g. on `enter` key or other handler starts).
- This stops the 100ms idle redrawing, making the screen 100% static when idle, allowing the terminal's native selection to work flawlessly.

### 3. Viewport Scrolling Keybindings
- Forward key messages to `m.viewport.Update(msg)` at the bottom of the main `Update()` function.
- In the `tea.KeyMsg` switch-case:
  - If autocomplete is active (`len(m.filteredCmds) > 0`), intercept `up` / `down` / `tab` / `shift+tab` to control autocomplete selection.
  - If autocomplete is not active, allow `up` / `down` arrow keys to scroll the viewport.
  - Map `ctrl+up` / `shift+up` to scroll the viewport up (`LineUp(1)`).
  - Map `ctrl+down` / `shift+down` to scroll the viewport down (`LineDown(1)`).
  - Map `pgup` / `pgdown` to scroll the viewport page up/down.

### 4. Enable AltScreen
- In `cmd/agentcli/root.go`, initialize `tea.NewProgram` with `tea.WithAltScreen()` option. This locks the application in AltScreen mode, ensuring headers/footers stay sticky at the top/bottom while the viewport scrolls the history, and native text selection works without getting messed up by the terminal buffer.

### 5. Formatting for `tool:` and `usage:` lines
- In `View()`, check lines for prefixes:
  - `tool:` -> render as `  🛠️  tool: <tool_name>` in emerald color.
  - `tool rejected:` -> render as `  ❌ tool rejected: <reason>` in bold red.
  - `tool error:` -> render as `  ⚠️  tool error: <error>` in bold red.
  - `usage:` -> render as `  📊 usage: <stats>` in dim gray.
  - `policy:` -> render as `  🛡️  policy: <details>` in amber.

## Verification Plan

### Automated Tests
- Юнит-тесты для парсера команд (убедиться, что вывод `npm install` правильно сжимается).
- Тесты для `DiffPatcher` (проверка применения изменений к тестовым файлам без их поломки).
- Интеграционные тесты с мок-сервером LLM для проверки Agent Loop.

### Current Implementation Notes
- Базовый CLI реализован на `spf13/cobra`.
- OpenAI provider по умолчанию использует Responses API; legacy Chat Completions сохранен как `chat`; локальные OpenAI-compatible модели доступны через `ollama`; Anthropic Messages API доступен через `anthropic`; Gemini GenerateContent API доступен через `gemini`.
- Встроены tools: `project_map`, `list_files`, `read_file`, `read_go_symbol`, `write_file`, `search`, `bash`, `diff_patch`.
- Для экономии токенов добавлены output compression, line slicing, Go symbol retrieval и динамическое pruning tool schema.
- Добавлены support-команды `doctor`, `tools`, `config`, `sessions`.
- Добавлены JSON config profiles (`local`, `coding`, `deep`), workspace session persistence и approval policy для рискованных tools.
- Добавлена загрузка project instruction files (`AGENTS.md`, `CLAUDE.md`, `GEMINI.md`, `.agentcli/instructions.md`, `.cursor/rules/*.md` и др.) в system prompt с байтовым бюджетом и командой `instructions`.
- Добавлены git/status/diff/checkpoint tools и CLI-команды `status`, `checkpoint create/list/restore`.
- Добавлен task-based model routing (`fast`, `edit`, `deep`) для снижения стоимости на простых задачах.
- Добавлен Responses API streaming (`--stream`) с SSE parsing и fallback для провайдеров без streaming.
- Добавлен `diff_preview` tool и CLI-команда `diff preview` для проверки unified diff перед применением.
- Добавлены estimated token budget packing, deterministic context summaries, orphan tool-output pruning, `/compact` в chat и `sessions compact`.
- Добавлен Bubble Tea TUI/Klyra (`agentcli tui`) с dashboard header, stream rendering, command palette/autocomplete, settings panel (`F2`/`Ctrl+S`) с выбором provider/reasoning/approval/sandbox и текстовыми полями model/endpoint, settings-командами (`/provider`, `/model`, `/endpoint`, `/reasoning`, `/limits`, `/approval`, `/sandbox`) и attach-flow для Vision.
- Добавлен shell policy classifier (`read`, `write`, `network`, `destructive`), CLI `policy check`, tool `policy_check`; destructive bash patterns block in auto approval.
- Добавлен `diff apply` UX: preview/check first, workspace checkpoint by default, optional `--yes`.
- Добавлены sandbox profiles (`read-only`, `workspace-write`, `danger-full-access`) с enforcement в tool registry и CLI-флагом `--sandbox`.
- Добавлен Anthropic Messages provider с `tool_use`/`tool_result` mapping и usage parsing.
- Добавлен Gemini GenerateContent provider с `functionCall`/`functionResponse` mapping, profile/env wiring и usage parsing.
- Добавлены Vision attachments для OpenAI Responses, Anthropic, Gemini и OpenAI-compatible/Ollama; base64 image data не сохраняется в session history ради экономии токенов.
- Approval mode `ask` получил TUI modal: рискованные tool calls подтверждаются внутри приложения клавишами `y`/`Enter` или отклоняются `n`/`Esc`.
- `project_map` усилен в сторону aider-style repo map: примерный token budget, focus ranking, Go AST summaries по package/imports/functions/methods/types вместо простого списка файлов.
- Добавлены реальные режимы сложности (`inspect`, `edit`, `repair`, `refactor`) и context cart. `inspect` скрывает/блокирует write/shell tools, `edit` требует явно добавленные файлы для write tools.
- Добавлен context debugger: после хода показывается режим, context cart, видимые модели tools и риски неполного контекста без автоматического расширения prompt.

### Manual Verification
- Запуск CLI локально на тестовом Go-репозитории.
- Просьба к агенту "Добавь обработку ошибок в функцию X" и замер количества потраченных токенов (должно быть точечное чтение AST, а не чтение всего файла).
