# Tracked git hooks

These are the repository's real hooks. They live here, under version control,
instead of in `.git/hooks/`, which is untracked and therefore per-workstation.

## Why (beads-dvf)

The `bd` pre-commit hook shipped a call to `bd sync --flush-only`, a flag that
has since been removed from `bd`, so **every commit failed** with
`Failed to flush bd changes to JSONL`. It was repaired in place on one
workstation — but `.git/hooks/` is not tracked and `core.hooksPath` was unset,
so a fresh clone (or a `bd` reinstall that rewrites the hook) reintroduced a
total commit block.

Keeping the hooks here fixes both halves of that:

* a fresh clone gets the repaired hooks from git, and
* `bd` reinstalling its own hook into `.git/hooks/` no longer takes effect,
  because `core.hooksPath` points git at this directory instead.

## Enabling them

`core.hooksPath` is local git config, so git cannot set it from inside the
repository (that would let a cloned repo run code on checkout). A fresh clone
needs exactly one command:

```bash
make hooks
```

or equivalently:

```bash
git config core.hooksPath .githooks
```

Verify with `git config core.hooksPath`, which must print `.githooks`.

## What they do

| Hook | Purpose |
|---|---|
| `pre-commit` | Exports the beads issue DB to `.beads/issues.jsonl` and stages it, so a commit never races the daemon's auto-flush. |
| `post-merge` | Imports `.beads/issues.jsonl` back into the DB after a pull/merge. |

Both are no-ops when `bd` is not installed or the repo has no `.beads`
directory, so they are safe for contributors who do not use beads.

⚠️ `core.hooksPath` replaces `.git/hooks` wholesale — git runs hooks **only**
from this directory once it is set. Any new hook must be added here, not to
`.git/hooks/`, or it will silently never run.
