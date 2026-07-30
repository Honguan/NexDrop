# Issue tracker: GitHub

Issues and PRDs for this repository live as GitHub Issues in `Honguan/NexDrop`.
Use the GitHub connector when available and `gh` for operations that the
connector does not support.

## Conventions

- Create one issue per independently verifiable problem or vertical slice.
- Read the complete issue body, comments, labels, and linked pull requests
  before changing its state.
- Every generated issue must include reproduction evidence, expected behavior,
  acceptance criteria, and known blocking issues.
- Apply exactly one category label (`bug` or `enhancement`) and one triage state
  label.
- Link the fixing pull request and close the issue only after required checks
  pass and the change reaches `master`.

## Pull requests as a triage surface

PRs as a request surface: no.

## Triage operations

- `needs-triage`: discovered but not yet verified.
- `needs-info`: cannot be reproduced without reporter or environment details.
- `ready-for-agent`: reproduced and specified with agent-ready acceptance
  criteria.
- `ready-for-human`: requires manual judgment, credentials, hardware, or other
  human-only work.
- `wontfix`: rejected or already implemented; close with evidence.

## When a skill says "publish to the issue tracker"

Create a GitHub Issue in `Honguan/NexDrop`.

## When a skill says "fetch the relevant ticket"

Fetch the issue body, comments, labels, assignees, and linked pull requests.

## Wayfinding operations

Use a `wayfinder:map` issue as the map and GitHub sub-issues as tickets when the
API is available. Otherwise, link child issues from a task list in the map and
put `Part of #<map>` at the start of every child.

Use native issue dependencies when available. Otherwise, include
`Blocked by: #<number>` in the issue body. A ticket is ready only when every
blocker is closed.
