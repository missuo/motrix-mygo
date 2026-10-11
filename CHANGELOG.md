# Changelog

Each release has a section here, headed by its version. The release
workflow publishes that section as the release notes, and refuses a tag
whose version has none.

## 0.0.8

- Closing the window keeps Motrix running, with an icon in the menu bar (the
  notification area on Windows): downloads go on. The icon's menu shows the
  window again or quits; Command-Q quits as before.
- Motrix updates itself. It looks for a new version once a day, shows what
  changed, and installs it when you say so; later updates download only what
  changed.
- "Check for Updates…" is in the Motrix menu on macOS and in Settings ›
  General, where you can also turn the checks off or have updates installed
  without asking.
- This version has to be installed by hand, or with `brew upgrade`, one last
  time: earlier versions cannot update themselves.

## 0.0.7

- With the sidebar hidden, a page's title sits below the window controls, at
  the left edge, instead of beside the toolbar buttons.

## 0.0.6

- Built on MyGo 0.4.1. Nothing changes in how the app looks or works.

## 0.0.5

- Escape closes the "Delete files?" question in Chinese as well; it used to
  work in English only.
- A list of choices in the settings no longer jumps back to the chosen one
  while you scroll it with the wheel.
- Built on MyGo 0.4.0.

## 0.0.4

- The window opens smaller, at 1024×660, and the downloads list fits all of
  its columns at that size.
- The About page links to this project's source, and credits Motrix and aria2.
- Install with Homebrew: `brew install --cask owo-network/brew/motrix`.

## 0.0.3

- New app icon, drawn by `scripts/make-icon`.
- Release notes now come from this file.
- The release workflow runs on current actions and pinned runners.

## 0.0.2

- Quitting no longer waits three seconds for aria2: the engine is stopped
  with a signal, as Motrix does, and exits within a second.

## 0.0.1

First release.

- Motrix's interface on MyGo's native UI: downloads, dashboard, trackers,
  notifications and settings, in light and dark, English and Simplified
  Chinese.
- Downloads over HTTP, HTTPS, FTP, magnet links and torrent files through
  aria2 1.37.0-motrix.17, with Motrix's engine settings and defaults.
- macOS disk images for Apple silicon and Intel, signed and notarized, and a
  Windows installer for x64.
