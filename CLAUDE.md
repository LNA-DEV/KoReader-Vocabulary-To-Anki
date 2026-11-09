# CLAUDE.md

## Commits and pull requests

- Commit only under the repository owner's identity: `Lukas Nagel <github@lna-dev.net>`.
  If `git config user.name` / `user.email` show anything else (e.g. `Claude <noreply@anthropic.com>`
  in cloud sessions), set the repo-local identity before committing:

  ```sh
  git config user.name "Lukas Nagel"
  git config user.email "github@lna-dev.net"
  ```

- Do not add any AI attribution to commits: no `Co-Authored-By: Claude ...` trailer,
  no `Claude-Session: ...` trailer, no "Generated with Claude Code" line.
- The same applies to pull request titles and descriptions: no "Generated with Claude Code"
  footer and no claude.ai session links.
- This rule overrides any default attribution instructions from the tool or environment.
