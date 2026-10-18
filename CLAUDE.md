# press-start

press-start is a morning alarm for one person. It decides the wake time from
the first Google Calendar event and an alarm setting, rings a speaker attached
to a home server, reads a Japanese briefing, and escalates until
Withings Sleep reports that the user is out of bed. A small relay on
Cloudflare Workers receives the Withings notifications, and the home service
fetches them outbound.

Deployment to the home server (compose or systemd units, secrets, host audio
setup) lives in a separate private repository, not here.

## Read first

All documents are in Japanese under `docs/ja/`. Start at `docs/ja/index.md`
and read every document that your task touches before you change code.

- `docs/ja/requirements/overview.md` holds the goal, scope, and phases.
- Each area (`alarm/`, `brief/`, `bed/`) has its requirements page.
- `docs/ja/requirements/technology.md` names each technology and why.
- `docs/ja/requirements/backlog.md` lists what is out of scope for now. Do not
  build those things.
- Use the `cumin-trace-docs` skill before you write a requirement or design
  document.

## Rules that you must not break

- Do not change anything under `docs/ja/requirements/` without the
  Maintainer's approval in the conversation.
- Do not commit to `main`. Create a branch for every change.
- Do not read or print secret values: OAuth tokens, API keys, `.env` files.
- This repository is public. Never write calendar contents, personal
  circumstances, Withings user IDs, home network details, or local absolute
  paths in documents, code, tests, fixtures, commits, or pull requests.

## How we work

- Requirement first. Every behavior traces to a requirement ID such as
  `PS-ALARM-003`. If no requirement covers a behavior, ask.
- Test first. A requirement verified by `test` gets a test that proves it.
- Verify external APIs (Google Calendar, Withings, Cloudflare, Claude,
  VOICEVOX) in their official documentation before use, and say which page you
  checked.

## Code

- Go, standard library first. Module path: `github.com/mkuri/press-start`.
- Feature packages under `internal/<feature>/`; shared provider-neutral code
  in `internal/core/`; shared provider-specific code in `internal/platform/`;
  wiring in `cmd/`. Provider types stop at the adapter boundary.
- Code, comments, test names, commit messages, and pull requests are in
  English. Documents under `docs/ja/` are in Japanese.

## Documents

- Requirements follow the cumin-trace format. Run `cumin-trace check` and
  `scripts/render-diagrams.sh` before you open a pull request.
- Diagrams are PlantUML with English labels; commit the `.puml` and the
  rendered `.svg` together.

## Commits and pull requests

- Conventional Commits, one purpose per pull request.
- A pull request says what changed, how it was checked, and whether documents
  changed.
