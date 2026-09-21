# Credis upstream synchronization procedure

The Credis P0 parent repository intentionally pins a commit from an independent
fork. Upstream is an input to review, never an automatic deployment source.

## Required procedure

1. Obtain authorization to configure `upstream` as
   `https://github.com/linux-do/credit` in the independent fork. Do not
   substitute another remote or repository.
2. Begin from a clean fork checkout and create an isolated synchronization
   branch, for example `sync/upstream-YYYY-MM-DD`.
3. Fetch upstream explicitly and identify the candidate commit:

   ```bash
   git fetch upstream
   git log --oneline --decorate HEAD..upstream/master
   git diff --stat HEAD..upstream/master
   git diff HEAD..upstream/master
   ```

4. Review the complete diff before merging. Reviewers must specifically assess
   schema or migration changes, every balance/ledger write, authorization and
   security changes, concurrency/idempotency effects, and operational
   configuration changes.
5. Run the complete automated suite using the project-declared Go version and
   a reproducible frontend install. Start API, worker, and scheduler against
   fresh isolated PostgreSQL and Redis data only; verify API health/readiness,
   worker and scheduler probes, and expected task logs.
6. Run the real forum regression process in the approved non-production forum
   environment. Record the regression evidence and unresolved failures.
7. A human reviewer manually merges the reviewed synchronization branch into
   the fork. Then update the parent repository gitlink in a separate parent
   commit after verifying `git diff --submodule=log`.

## Non-negotiable constraints

- Never automatically deploy the upstream default branch.
- Never point `origin` at upstream or another person’s fork.
- Never use production database, Redis, OAuth, forum, or user data for a
  baseline or regression check.
- Treat a failed command, denied permission, or absent prerequisite as a
  failed or incomplete gate, not as a pass.
- Preserve the branch, candidate SHA, command output, review notes, and test
  evidence with every synchronization.
