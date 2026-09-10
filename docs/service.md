# Background services

`recall service` turns two commands into background jobs, so the dashboard
is always up at one address and every session is archived once a day even
when you never think about it.

```sh
recall service install
```

That installs both. Each step prints its plan and asks y/N. Pass `--yes` to
skip the questions, `--serve` to install only the dashboard, `--archive` to
install only the daily run.

macOS only. On macOS the jobs are launchd user agents. On Linux `recall
service` refuses and prints the exact command to put into a systemd user
unit instead.

## The two jobs

| Job | Label | Runs | When |
| --- | --- | --- | --- |
| Dashboard | `dev.recall.serve` | `recall serve --no-open --addr 127.0.0.1:4747` | At login, and again after any unclean exit |
| Daily archive | `dev.recall.archive` | `recall archive --all --quiet` | Once a day at 09:00 local time |

The dashboard job is the one behind `recall service url`. It listens on the
same loopback address every time, and the token lives in
`~/.recall/serve.token`, so the URL never changes. Bookmark it, or link it
from another dashboard.

The archive job is the safety net under the SessionEnd hook. The hook only
fires when a session ends cleanly. A session you leave open for three weeks
never ends at all, and a crash or a reboot skips the hook. The daily run
does not care: it walks every transcript on disk and archives whatever is
not archived yet.

The daily run is idempotent. A session whose archive already matches the
transcript on disk is skipped, so nothing is copied twice and a repeat run
costs almost nothing. With `--quiet` the job prints one summary line, and
nothing at all when nothing changed, so the log stays empty on a quiet day.

## Where things live

| Thing | Path |
| --- | --- |
| Dashboard property list | `/Users/me/Library/LaunchAgents/dev.recall.serve.plist` |
| Archive property list | `/Users/me/Library/LaunchAgents/dev.recall.archive.plist` |
| Dashboard logs | `/Users/me/.recall/logs/serve.out.log`, `serve.err.log` |
| Archive logs | `/Users/me/.recall/logs/archive.out.log`, `archive.err.log` |
| Dashboard token | `/Users/me/.recall/serve.token` |

Both property lists are written atomically with mode 0644, through a
temporary file and a rename, so launchd can never pick up half a file.

If you installed with `--claude-dir` or `--recall-dir`, those paths are
written into the job as well, so the service keeps pointing at the same
directories the install did. The log directory follows `--recall-dir`.

## Commands

```sh
recall service install                 # both jobs, y/N per step
recall service install --serve         # the dashboard only
recall service install --archive       # the daily archive only
recall service install --at 02:30      # run the archive at 02:30 instead
recall service install --addr 127.0.0.1:5000
recall service install --yes           # no questions
recall service status                  # installed, loaded, pid, plist path
recall service url                     # the bookmarkable URL, one line
recall service uninstall               # stop both and remove both plists
recall service uninstall --archive     # leave the dashboard alone
```

`--at` takes `HH:MM` in 24-hour local time. `--addr` must be a loopback
address; `recall serve` refuses anything else, so the job cannot be pointed
at the network by accident.

To change the time or the address, run `install` again with the new value.
The job is booted out and replaced.

`recall service status` prints one row per job:

```text
SERVICE  INSTALLED  LOADED  PID    PLIST
serve    yes        yes     41207  /Users/me/Library/LaunchAgents/dev.recall.serve.plist
archive  yes        yes     -      /Users/me/Library/LaunchAgents/dev.recall.archive.plist

dashboard http://127.0.0.1:4747/?t=4fa2e085621d33cfacb1c15723a51072
```

The archive job has no pid between runs. That is normal. It starts, archives
what is new, and exits.

## What recall runs on your behalf

These are the only `launchctl` calls recall makes. `gui/501` is your own
user domain; the number is your uid.

| When | Call |
| --- | --- |
| Install, first | `launchctl bootout gui/501/dev.recall.serve` |
| Install, then | `launchctl bootstrap gui/501 /Users/me/Library/LaunchAgents/dev.recall.serve.plist` |
| Install, fallback | `launchctl load -w /Users/me/Library/LaunchAgents/dev.recall.serve.plist` |
| Uninstall | `launchctl bootout gui/501/dev.recall.serve` |
| Uninstall, fallback | `launchctl unload -w /Users/me/Library/LaunchAgents/dev.recall.serve.plist` |
| Status | `launchctl print gui/501/dev.recall.serve`, else `launchctl list dev.recall.serve` |

The bootout before a bootstrap is deliberate. launchd rejects a bootstrap of
a job it already knows, and an absent job is exactly the state we want, so
the result of that first call is ignored.

`bootstrap` is the modern call. `load -w` is tried only when bootstrap is
missing or refuses, which is what old macOS releases do. Uninstall works the
same way in reverse, and `unload` runs before the file is removed, because
the old call takes the file rather than the label.

Every call is capped at 10 seconds, so a wedged service database cannot hang
your terminal.

Two environment variables change what happens:

- `RECALL_DRY_RUN=1` prints the plan, the full property list and the
  launchctl calls, and writes nothing.
- `RECALL_SERVICE_NO_LAUNCHCTL=1` writes and removes the property list but
  never calls launchctl. Every test in this repository sets it.

## Verify by hand

```sh
ls -l ~/Library/LaunchAgents/dev.recall.*.plist
launchctl print "gui/$(id -u)/dev.recall.serve" | head -20
launchctl print "gui/$(id -u)/dev.recall.archive" | grep -i 'next\|last exit'
curl -sI "$(recall service url)" | head -1
tail -n 20 ~/.recall/logs/archive.out.log
```

`launchctl print` shows the pid while the dashboard runs and the last exit
code after any run. For the archive job it also shows the next scheduled
fire date. An empty `archive.out.log` means the last runs found nothing new,
which is the normal state.

To run the daily job now instead of waiting:

```sh
launchctl kickstart -k "gui/$(id -u)/dev.recall.archive"
```

## Uninstall

```sh
recall service uninstall
```

That boots both jobs out and removes both property lists. Running it twice
is safe: a property list that is not there is not an error.

Nothing else is touched. The archives under `~/.recall/archive`, the token,
the logs and your labels and pins all stay. `recall service uninstall` is
not `recall uninstall`; the latter is the one that reverses `recall setup`.

## The property lists

This is exactly what recall writes. The binary path is the absolute path of
the `recall` you installed from, and the two directory arguments appear only
when you installed with `--claude-dir` or `--recall-dir`.

`dev.recall.serve.plist`:

```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>dev.recall.serve</string>
	<key>ProgramArguments</key>
	<array>
		<string>/opt/homebrew/bin/recall</string>
		<string>serve</string>
		<string>--no-open</string>
		<string>--addr</string>
		<string>127.0.0.1:4747</string>
	</array>
	<key>RunAtLoad</key>
	<true/>
	<key>KeepAlive</key>
	<dict>
		<key>SuccessfulExit</key>
		<false/>
	</dict>
	<key>StandardOutPath</key>
	<string>/Users/me/.recall/logs/serve.out.log</string>
	<key>StandardErrorPath</key>
	<string>/Users/me/.recall/logs/serve.err.log</string>
	<key>ProcessType</key>
	<string>Background</string>
</dict>
</plist>
```

`KeepAlive` is a dict rather than `<true/>` on purpose. launchd restarts the
dashboard after a crash and leaves it alone after a clean stop, so you can
stop it yourself without launchd fighting you.

`dev.recall.archive.plist`:

```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>dev.recall.archive</string>
	<key>ProgramArguments</key>
	<array>
		<string>/opt/homebrew/bin/recall</string>
		<string>archive</string>
		<string>--all</string>
		<string>--quiet</string>
	</array>
	<key>RunAtLoad</key>
	<false/>
	<key>StartCalendarInterval</key>
	<dict>
		<key>Hour</key>
		<integer>9</integer>
		<key>Minute</key>
		<integer>0</integer>
	</dict>
	<key>StandardOutPath</key>
	<string>/Users/me/.recall/logs/archive.out.log</string>
	<key>StandardErrorPath</key>
	<string>/Users/me/.recall/logs/archive.err.log</string>
	<key>ProcessType</key>
	<string>Background</string>
</dict>
</plist>
```

`RunAtLoad` is false here. Installing the job should not start an archive
run; the schedule should. `StartCalendarInterval` is what `--at` changes.

launchd runs a missed calendar job once after the Mac wakes or boots, so a
laptop that was closed at 09:00 still gets its archive.

To see these before anything is written:

```sh
RECALL_DRY_RUN=1 recall service install --yes
```

## Linux

There is no launchd, so `recall service install` refuses and names the
command to run instead. The same two jobs are a pair of systemd user units
you write by hand:

```ini
# ~/.config/systemd/user/recall-serve.service
[Unit]
Description=recall dashboard

[Service]
ExecStart=/usr/local/bin/recall serve --no-open --addr 127.0.0.1:4747
Restart=on-failure

[Install]
WantedBy=default.target
```

```ini
# ~/.config/systemd/user/recall-archive.service
[Unit]
Description=recall daily archive

[Service]
Type=oneshot
ExecStart=/usr/local/bin/recall archive --all --quiet
```

```ini
# ~/.config/systemd/user/recall-archive.timer
[Timer]
OnCalendar=*-*-* 09:00:00
Persistent=true

[Install]
WantedBy=timers.target
```

```sh
systemctl --user enable --now recall-serve.service
systemctl --user enable --now recall-archive.timer
```

`recall service status` and `recall service uninstall` do not know about
these. `recall service url` still works, because the URL comes from the
token file rather than from launchd.

## See also

- [docs/dashboard.md](dashboard.md): the dashboard itself, its API and its
  security model.
- [docs/design.md](design.md): where the archive sits in the architecture.
