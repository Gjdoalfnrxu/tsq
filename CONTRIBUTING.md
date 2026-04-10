# Contributing to tsq

## Development Workflow

After cloning, run `make setup` to configure the git hooks path. This installs a pre-commit hook that auto-formats and auto-fixes lint issues before each commit.

All changes to tsq must go through a pull request. Direct pushes to `main` are not permitted.

### Branch Naming

`feat/phase-N-<short-description>` for planned phases.
`fix/<short-description>` for bug fixes.

### Worktree Workflow

Each parallel work stream works in its own git worktree:

```bash
git worktree add /tmp/tsq-phase-N feat/phase-N-description
cd /tmp/tsq-phase-N
# do work
git add .
git commit -m "feat: implement X"
git push origin feat/phase-N-description
gh pr create --title "Phase N: description" --body "..."
```

### PR Requirements

Every PR must:
1. Have a description explaining what changed and why
2. Pass CI (all tests green, lint clean)
3. Include a handover document (`HANDOVER-phase-N.md`) for phase completions
4. Be adversarially reviewed by a separate agent before merge

### Adversarial Review

For each PR, a separate review agent runs the standard review checklist (see implementation plans). The review must find and report any real problems before the PR merges. The implementing agent fixes issues found, then the reviewer re-checks.

### Handover Documents

When a phase completes, the implementing agent creates `HANDOVER-phase-N.md` at the repo root describing:
- What was implemented
- What was intentionally left out
- Key decisions and rationale
- Test coverage summary
- Files changed
- Dependencies for downstream phases
- First steps for the next phase
