# The dashboard

`recall serve` shows every session in a browser instead of a terminal list.
It runs on your machine, serves one page, and opens a session in a terminal
when you click a button.

```sh
recall serve
```

It prints the address and stays in the foreground until you press Ctrl-C:

```text
recall dashboard
   http://127.0.0.1:4747/?t=4fa2e085621d33cfacb1c15723a51072

Local only. Press Ctrl-C to stop.
```

The browser opens automatically when the command runs in a terminal. Pass
`--no-open` to stop that, `--addr` to change the port, and `--print-token`
to see the token on its own line.

## The page

Each session is a card:

- A state pill: Needs you, Running, Closed or Gone.
- The title, taken from Claude's own record for that session.
- Project, branch, last activity, and the terminal it is running in.
- The last thing you typed, or the last thing Claude answered.
- Badges for open pull requests, worktrees, files changed and turns.

The buttons on a card depend on its state. A running session offers
**Focus**, which brings its existing terminal tab to the front. A closed
session offers **Open**, which resumes it in a new tab, and **Open in cmux**
when the cmux app is installed. Behind **More** are fork, pin, label, hide
and copy resume command.

Above the list are a search box and filter chips, a banner when Claude's
30-day retention is still in place, and a banner when live detection is
degraded. Cards refresh every few seconds over a server-sent event stream,
falling back to polling if the stream drops.

Keyboard: `/` focuses search, `j` and `k` move the selection, `Enter` opens
the selected session, `Escape` clears the search.

## Security

The dashboard shows your prompts and answers, so it is built to stay on the
machine it runs on.

- It binds to `127.0.0.1` only. `--addr` refuses any address that is not a
  loopback address, so it cannot be exposed to the network by accident.
- Every request carries a 32-character token. The page reads it from its own
  URL and sends it as the `X-Recall-Token` header. A request without the
  token gets 401.
- The token is generated on first run and stored in `~/.recall/serve.token`
  with owner-only permissions, so the URL keeps working across restarts.
  Pass `--token` to override it.
- Requests from another origin are refused, so a page in another tab cannot
  drive your dashboard.
- The page loads no external scripts. Its content security policy allows
  only its own styles, its own connections, and Google Fonts.
- Responses are sent with `Cache-Control: no-store` and `X-Frame-Options:
  DENY`.

Anyone with access to your user account can read the token file and open the
dashboard. It is a convenience for one person on one machine, not a
multi-user service.

## API

Every endpoint needs the `X-Recall-Token` header. Every response is JSON.

| Method | Path | What it does |
| --- | --- | --- |
| GET | `/api/sessions` | The full payload: summary counts, warnings and every session row. Query flags `ghosts=1`, `hidden=1`, `headless=1` widen the list. |
| POST | `/api/refresh` | Forces a full rescan and returns the same payload. |
| POST | `/api/open` | Opens a session. Body: `{"id": "...", "target": "terminal" \| "iterm" \| "cmux" \| "newtab", "fork": false}` |
| POST | `/api/label` | Body: `{"id": "...", "label": "api"}`. An empty label removes it. |
| POST | `/api/pin` | Body: `{"id": "...", "pinned": true}` |
| POST | `/api/hide` | Body: `{"id": "...", "hidden": true}` |
| GET | `/api/events` | A server-sent event stream that tells the page when to refetch. |

`/api/open` returns the action it took rather than a bare success:

```json
{
  "ok": true,
  "kind": "resume",
  "description": "resume 21aa07d4 in /Users/me/dev/app (new Terminal tab)",
  "command": "cd /Users/me/dev/app && claude --resume 21aa07d4-55ff-4b8d-b044-6279abebe0b0",
  "note": "",
  "dryRun": false,
  "target": "terminal",
  "tty": ""
}
```

`kind` is `focus` for a session that is still running, `attach` for a kept
tmux session, `resume` for a closed one, and `print` when recall can see the
session but cannot drive its terminal. `note` carries the losses to expect
on a resume, and the page shows it for confirmation before it opens
anything. With `RECALL_DRY_RUN=1` set, nothing is executed and `command`
shows what would have run.

A session that is already open elsewhere returns 409, a session whose
transcript Claude deleted returns 400, and an unknown id returns 404.

## Limits

- Focusing an existing tab works for Terminal.app and iTerm2. A session
  running inside Cursor, VS Code or another host cannot be focused; the
  response says where it is instead.
- Opening in cmux needs the cmux app installed. The button is hidden
  otherwise.
- On Linux there is no tab focus and no new tab. Sessions resume in place or
  inside a kept tmux session.
- The dashboard drives the same code as the terminal list, so it can never
  do something `recall open` would refuse, including resuming a session that
  is already running.
