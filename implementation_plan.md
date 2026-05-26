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

## Verification Plan

### Automated Tests
- Юнит-тесты для парсера команд (убедиться, что вывод `npm install` правильно сжимается).
- Тесты для `DiffPatcher` (проверка применения изменений к тестовым файлам без их поломки).
- Интеграционные тесты с мок-сервером LLM для проверки Agent Loop.

### Current Implementation Notes
- Базовый CLI реализован на `spf13/cobra`.
- OpenAI provider по умолчанию использует Responses API; legacy Chat Completions сохранен как `chat`; локальные OpenAI-compatible модели доступны через `ollama`; Anthropic Messages API доступен через `anthropic`.
- Встроены tools: `project_map`, `list_files`, `read_file`, `read_go_symbol`, `write_file`, `search`, `bash`, `diff_patch`.
- Для экономии токенов добавлены output compression, line slicing, Go symbol retrieval и динамическое pruning tool schema.
- Добавлены support-команды `doctor`, `tools`, `config`, `sessions`.
- Добавлены JSON config profiles (`local`, `coding`, `deep`), workspace session persistence и approval policy для рискованных tools.
- Добавлены git/status/diff/checkpoint tools и CLI-команды `status`, `checkpoint create/list/restore`.
- Добавлен task-based model routing (`fast`, `edit`, `deep`) для снижения стоимости на простых задачах.
- Добавлен Responses API streaming (`--stream`) с SSE parsing и fallback для провайдеров без streaming.
- Добавлен `diff_preview` tool и CLI-команда `diff preview` для проверки unified diff перед применением.
- Добавлены estimated token budget packing, deterministic context summaries, orphan tool-output pruning, `/compact` в chat и `sessions compact`.
- Добавлен Bubble Tea TUI (`agentcli tui`) с input surface, scrollable-style transcript, status line и командами `/help`, `/status`, `/compact`, `/clear`, `/exit`.
- Добавлен shell policy classifier (`read`, `write`, `network`, `destructive`), CLI `policy check`, tool `policy_check`; destructive bash patterns block in auto approval.
- Добавлен `diff apply` UX: preview/check first, workspace checkpoint by default, optional `--yes`.
- Добавлены sandbox profiles (`read-only`, `workspace-write`, `danger-full-access`) с enforcement в tool registry и CLI-флагом `--sandbox`.
- Добавлен Anthropic Messages provider с `tool_use`/`tool_result` mapping и usage parsing.

### Manual Verification
- Запуск CLI локально на тестовом Go-репозитории.
- Просьба к агенту "Добавь обработку ошибок в функцию X" и замер количества потраченных токенов (должно быть точечное чтение AST, а не чтение всего файла).
