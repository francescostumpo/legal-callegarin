# Codex Agent Roles Design

## Objective

Configure this repository so that a Sol primary agent owns decisions and final
quality, while specialized Luna subagents perform exploration, implementation,
and task-level review.

## Configuration layout

The repository will contain:

- `.codex/config.toml`: project defaults and the custom-agent registry.
- `.codex/agents/luna-explorer.toml`: read-only exploration role.
- `.codex/agents/luna-implementer.toml`: implementation role with workspace
  write access.
- `.codex/agents/luna-task-reviewer.toml`: read-only task-review role.
- `AGENTS.md`: durable orchestration rules and hand-off contracts.

The project default model will be `gpt-5.6-sol` with reasoning effort `high`.
The three custom roles will use `gpt-5.6-luna`.

## Roles

### Sol orchestrator

Sol is the primary repository agent. It:

- establishes requirements, constraints, acceptance criteria, and the plan;
- decomposes the plan into bounded, independently verifiable tasks;
- delegates repository discovery to Luna Explorer;
- delegates approved tasks to Luna Implementer;
- requires task-level review from Luna Task Reviewer;
- performs the final review of the complete plan or set of tasks.

Sol normally does not implement product changes. It preserves decision context,
resolves disagreements between subagents, and owns the final result.

### Luna Explorer

Luna Explorer uses `gpt-5.6-luna` with reasoning effort `medium` and a
read-only sandbox. It gathers evidence, maps relevant code paths, identifies
constraints and validation commands, and reports concise findings with file and
symbol references. It does not modify files or propose an implementation unless
Sol explicitly requests options.

### Luna Implementer

Luna Implementer uses `gpt-5.6-luna` with reasoning effort `xhigh` and a
workspace-write sandbox. It receives one bounded task with explicit acceptance
criteria, implements the smallest coherent change, runs relevant validation,
and returns a concise hand-off containing changed files, commands run, results,
and remaining risks.

### Luna Task Reviewer

Luna Task Reviewer uses `gpt-5.6-luna` with reasoning effort `xhigh` and a
read-only sandbox. It reviews one completed task against its specification,
acceptance criteria, diff, tests, correctness, security, and regression risk.
Its result is either `APPROVED` or a prioritized list of actionable findings.
It does not edit the implementation.

## Workflow

1. Sol clarifies the request and obtains repository evidence from Luna Explorer
   when unfamiliar code, behavior, or risk warrants exploration.
2. Sol writes the specification and plan, including acceptance criteria and
   validation for every task.
3. Luna Implementer completes one task at a time.
4. Luna Task Reviewer independently reviews that task.
5. If findings remain, Sol routes them back to Luna Implementer and repeats the
   task-review loop until the task is approved or a real blocker is reported.
6. After all tasks are approved, Sol reviews the aggregate diff and validation
   against the original plan, checks cross-task interactions, and issues the
   final decision.

Implementation and review of the same task are sequential. Independent
exploration or preparation may run concurrently when it cannot conflict with
active writes.

## Hand-off contracts

Every delegated task must state its scope, inputs, acceptance criteria,
validation commands, and prohibited changes. Subagent results must distinguish
verified facts from assumptions and must not claim success without current
validation evidence.

Task-review findings must be ordered by severity and include concrete file or
symbol references. Style-only comments are excluded unless they expose a
maintainability or correctness risk.

## Failure handling

- A subagent reports a blocker when required information, permission, or an
  external dependency is unavailable; Sol decides whether to refine, reroute,
  or ask the user.
- Failed validation returns the task to Luna Implementer with the exact failure
  evidence.
- Conflicting recommendations are resolved by Sol against the approved
  specification and repository evidence.
- Sol does not give final approval while task-review findings or relevant test
  failures remain unresolved.

## Verification

Configuration verification will confirm that all TOML files parse, every
registered `config_file` exists, model and reasoning settings match this design,
and `AGENTS.md` expresses the same role boundaries and workflow without
contradictions.
