# AGENTS.md

Motrix's interface rewritten in Go on [MyGo](https://github.com/egoist/mygo)'s
native UI, driving the `aria2c` build Motrix ships over JSON-RPC. One Go
module, no webview, no JavaScript.

## Commands

```sh
go run ./scripts/fetch-engine   # download aria2c into resources/ (once per machine)
go tool mygo dev                # run the app with live reload
go test ./...                   # engine and view tests, against the real aria2c
go vet ./... && gofmt -l .      # must be clean before a commit
go tool mygo build              # package into build/
go run ./scripts/make-icon      # redraw resources/icon.png
```

Tests that need `aria2c` skip when `fetch-engine` has not been run: run it
first, or a green run proves little. `TestLive` downloads real files and
rewrites `docs/screenshots/`; it runs only with `MOTRIX_LIVE` set (see the
README).

Cross-check the other platforms before finishing a change that touches
`engine/` or `main.go`:

```sh
GOOS=windows go vet ./... && GOOS=linux go vet ./...
```

## Layout

- `engine/`: the aria2 process, its arguments and its RPC. It imports
  nothing of the UI and must stay that way.
- `main.go`, `app.go`: the window, the app's state, the sidebar and the
  router.
- `downloads.go`, `addtask.go`, `dashboard.go`, `trackers_view.go`,
  `notifications.go`, `settings_view.go`: one file a page.
- `settings.go`: the settings file and the speed limit rules.
- `theme.go`, `widgets.go`, `i18n.go`: colors, shared widgets, texts.
- `resources/`: the icon, Motrix's `aria2.conf`, and `aria2c` per platform
  (ignored by git).

## Rules

- **Follow Motrix.** Engine settings keep Motrix's JSON keys, defaults and
  ranges (`engine/settings.go`), and aria2's arguments keep its layer order
  (`engine/args.go`): the user's `aria2.conf`, engine bindings, settings,
  proxy, then the invariants, last so that nothing overrides them. Check a
  change against the Motrix source before making it.
- **The view is a function of state.** `app.view` runs on the main thread for
  every frame. Never store a `ui.Element` or `*ui.Context` in `app`, and never
  touch `app` from another goroutine except inside `app.update`.
- **Engine calls return errors.** Pass them to `app.try`, which shows them as
  a toast and refreshes the tasks.
- **Engine settings are a draft.** The Downloads, BitTorrent, Network and
  Advanced pages edit `app.draft`; only Save applies it. General and
  Appearance apply at once.
- **Every user-visible text goes through `T`.** Add its Chinese to `zhCN` in
  `i18n.go`, taking Motrix's own wording from its `zh-CN.json` where it has
  the text.
- **The default window shows everything whole.** After changing a page, look
  at it at 1200×780 and at the minimum 960×620: no clipped or ellipsized
  labels, numbers or columns.
- **Windows and Linux put the window controls at the top right.** A page
  header pads its right with `trailingInset`, and its left with `chromeWidth`
  while the sidebar is hidden. Do not place controls in those corners.
- **The app's data lives in `Motrix MyGo`**, never in Motrix's own `Motrix`
  directory: the two must not share an aria2 session or database.
- **No secrets in the repository.** The signing certificate and the notary
  credentials are GitHub secrets, set by `scripts/set-release-secrets.sh`.
- Comments and documentation are in English. Match the comment style around
  the code: what a thing is for, in a sentence.

## Releasing

1. Set `version` in `mygo.json`.
2. Add a `## <version>` section to `CHANGELOG.md`, written for users: what
   changed for them, not which files. The workflow publishes it as the release
   notes and fails without it.
3. Commit, then `git tag v<version> && git push origin v<version>`.

`.github/workflows/release.yml` builds and notarizes the macOS disk images,
builds the Windows installer, and publishes the release once both are
uploaded. Watch the run to its end; a release is not done until the "Check
the signature and the notarization" step has passed.
