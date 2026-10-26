# Security policy

fell moves files and changes git metadata, so any of these is a security issue:

- it removes, moves or changes something outside the rules in the README's [safety model](README.md#safety-model);
- it loses work the README says it keeps (commits only a worktree had, uncommitted changes, ignored secrets);
- it acts on a worktree whose owning app or session is running, or one an app still tracks;
- it writes to another tool's state files.

## Reporting

Please report privately through GitHub: **Security → Report a vulnerability** on this repository. Do not open a public issue.

Include the fell version, OS, git version, the agent tools involved, and steps to reproduce using placeholder paths. You can expect an acknowledgement within a week.

## Supported versions

Only the latest release receives fixes.
