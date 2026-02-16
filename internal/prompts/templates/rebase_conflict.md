You are resolving git rebase conflicts for a feature branch. Your goal is to resolve all conflicts while preserving the intent of the feature and incorporating the incoming base branch changes.

## PRD Description

{{.PRDDescription}}

## User Stories

{{.Stories}}

## Progress Log

```
{{.Progress}}```

## Feature Branch Changes (your work)

The following diff shows what the feature branch changed relative to the common ancestor:

```diff
{{.FeatureDiff}}```

## Incoming Base Branch Changes

The following diff shows what the base branch changed since the common ancestor:

```diff
{{.BaseDiff}}```

## Conflicted Files

The following files have conflict markers that need to be resolved:

```
{{.ConflictFiles}}```

## Instructions

1. Open each conflicted file and resolve the conflict markers (`<<<<<<<`, `=======`, `>>>>>>>`).
2. **Preserve the intent of the feature branch** — the feature's behavior and logic should remain intact.
3. **Incorporate base branch changes** — adopt renamed variables, new imports, structural refactors, or API changes from the base branch.
4. If the base branch moved or renamed code that the feature branch also modified, apply the feature's changes on top of the base branch's new structure.
5. After resolving each file, run `git add <file>` to mark it as resolved.
{{- if .QualityChecks}}
6. After resolving all conflicts and before completing the rebase, run quality checks:
{{range .QualityChecks}}   - `ralph check {{.}}`
{{end}}   > **Note:** `ralph check` wraps each command with compact pass/fail output. Full output is saved to the log file path shown in the output. If the truncated output is insufficient for debugging, you can grep or read the full log file.
7. Once all files are resolved, staged, and quality checks pass, run `git rebase --continue` to proceed.
8. Do NOT run `git rebase --abort` unless the conflicts are truly unresolvable.
{{- else}}
6. Once all files are resolved and staged, run `git rebase --continue` to proceed.
7. Do NOT run `git rebase --abort` unless the conflicts are truly unresolvable.
{{- end}}
