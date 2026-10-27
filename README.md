# logcost

Find out which log lines eat your logging bill.

Most of a log bill usually comes from a handful of lines: health checks, debug output someone forgot to turn off, a retry loop. They're hard to spot because every line is slightly different, with its own IDs, timestamps and durations. logcost groups lines into templates and shows how much each one weighs.

```
$ logcost -price 1.70 app.log
1.0 GB in 10.3M lines (9.1M events), 13 templates, covering 7d
about $7.29/month at $1.70 per GB (set your price with -price)

by level: TRACE 2.6%, DEBUG 22.1%, INFO 58.8%, WARN 5.2%, ERROR 11.2%

 SHARE      SIZE   EVENTS   $/MONTH  LEVEL  TEMPLATE
 22.0%    220 MB     1.2M     $1.60  INFO   request finished
 21.1%    211 MB     2.7M     $1.54  INFO   INFO [http] GET /healthz <*> in <*> from <*>
 12.7%    127 MB     1.6M     $0.93  DEBUG  DEBUG [cache] hit key=<*> ttl=<*> size=<*>
  9.4%   94.2 MB   799.9K     $0.69  DEBUG  DEBUG [db] SELECT * FROM orders WHERE user_id = <*> AND status = 'open' LIMIT <*> took <*>
  7.5%   74.8 MB   711.6K     $0.54  INFO   INFO [kafka] committed offset <*> for partition <*> of topic orders.events
  6.7%   67.3 MB   533.3K     $0.49  INFO   INFO [auth] user <*> logged in, session_id=<*> token=<*>
  5.2%   52.4 MB   444.3K     $0.38  WARN   WARN [payments] retrying charge <*>, attempt <*> of <*>: upstream timeout after <*>
  4.8%   48.2 MB    89.1K     $0.35  ERROR  ERROR [inventory] sync failed for warehouse <*>
 10.5%  ... 5 more templates
```

That's a generated week of logs from a made-up shop. Health checks and cache hits are a third of everything, and nobody reads them. The `ERROR [inventory]` row is 89K events but 48 MB, because each of them carries a Java stack trace, which logcost counts as part of the same event.

## Cutting the bill

Add `-suggest` and logcost points at what can go:

```
Suggestions: about $4.79/month, 65.7% of the volume
ACTION                 SAVES   $/MONTH  WHY           TEMPLATE
drop                   21.1%     $1.54  health check  INFO [http] GET /healthz <*> in <*> from <*>
sample, keep 1 in 10   19.8%     $1.44  high volume   request finished
drop                   12.7%     $0.93  debug level   DEBUG [cache] hit key=<*> ttl=<*> size=<*>
drop                    9.4%     $0.69  debug level   DEBUG [db] SELECT * FROM orders WHERE user_id = <*> AND status = 'open' LIMIT <*> took <*>
drop                    2.6%     $0.19  trace level   TRACE [scheduler] tick worker=<*> queue_depth=<*>
```

It picks debug and trace output, health checks and probes, and suggests keeping 1 in 10 of any info template bigger than 8%. Warnings and errors are never on the list. These are guesses from the log alone, so read them before you act: maybe that debug line is the one you need during incidents.

Then `-filter` turns the list into a config for your log shipper:

```bash
logcost -filter vector app.log      # filter + sample transforms for Vector
logcost -filter fluentbit app.log   # grep filters for Fluent Bit
logcost -filter otel app.log        # filter processor for the OpenTelemetry Collector
```

```yaml
transforms:
  logcost_drop:
    type: filter
    inputs: [your_source]
    condition: |-
      msg = to_string(.message) ?? ""
      !(match(msg, r'INFO\s+\[http\]\s+GET\s+/healthz\s+\S+\s+in\s+\S+\s+from\s+\S+\s*$') || ...)
  logcost_sample:
    type: sample
    inputs: [logcost_drop]
    rate: 10
    ...
```

Fluent Bit and the filter processor can't sample by pattern, so for them only the drop rules are written. Secrets are hidden in the patterns too, so a constant token in a log line won't end up in your config repo. Every push runs the generated configs through real Vector, Fluent Bit and OpenTelemetry Collector containers and checks that they drop exactly the suggested lines and keep every warning and error ([scripts/check-configs.sh](scripts/check-configs.sh)).

The rules match the first line of an event. If your shipper splits stack traces into separate records, the frames of a dropped event will still go through.

## What changed since last week

`-diff` compares two inputs, for example logs from before and after a release:

```
$ logcost -diff -price 1.70 last-week.log after-release.log
before: 300 MB over 7d (42.9 MB/day)    after: 130 MB over 3d (43.4 MB/day)    +1.1%
about +$0.02/month at $1.70 per GB

CHANGE   BEFORE/DAY   AFTER/DAY   $/MONTH  LEVEL  TEMPLATE
+255%        2.2 MB      7.9 MB    +$0.29  WARN   WARN [payments] retrying charge <*>, attempt <*> of <*>: upstream timeout after <*>
-71%         5.5 MB      1.6 MB    -$0.20  DEBUG  DEBUG [cache] hit key=<*> ttl=<*> size=<*>
new               -      3.2 MB    +$0.16  INFO   INFO [search] query q=<*> results=<*> took <*> cache=miss
gone         631 KB           -    -$0.03  INFO   INFO [mailer] sent receipt to <*> in <*>
10 more templates changed less or are under 0.5% of the volume
```

The total barely moved, but the payment retries tripled, which is worth a look. Both inputs go through the same template tree, so a line from either side always lands in the same template. When both sides have timestamps it compares volume per day, so a week and three days can be put side by side. Templates that changed by less than 25%, or stay under 0.5% of the volume, are left out.

In CI, `-max-growth` makes it fail when logs grow too much:

```bash
logcost -diff -max-growth 20% baseline.log current.log   # exit code 3 if the volume per day grew more than 20%
logcost -diff -format markdown baseline.log current.log  # table for a pull request comment
```

## Install

Windows (PowerShell):

```powershell
irm https://raw.githubusercontent.com/wellkyc/logcost/main/install.ps1 | iex
```

Linux and macOS:

```bash
curl -fsSL https://raw.githubusercontent.com/wellkyc/logcost/main/install.sh | sh
```

The scripts download the latest release, check it against `SHA256SUMS` and refuse to install if it doesn't match. Binaries are also on the [Releases](https://github.com/wellkyc/logcost/releases) page for Windows, Linux and macOS (x86_64 and arm64). Each release has a build provenance attestation, so you can check that a file was built from this repo by GitHub Actions:

```bash
gh attestation verify logcost-linux-x86_64.tar.gz --repo wellkyc/logcost
```

With Go 1.27+: `go install github.com/wellkyc/logcost/cmd/logcost@latest`

## Usage

```bash
logcost app.log                       # one file
logcost /var/log/app/*.log            # several files, counted together
logcost access.log.gz                 # gzip is detected automatically
kubectl logs deploy/api --since=24h | logcost -period 24h
journalctl -u nginx --since today -o cat | logcost
```

Flags:

| Flag | Default | |
| --- | --- | --- |
| `-price` | `0.50` | what you pay per GB of logs, in dollars |
| `-period` | from timestamps | how much time the input covers: `90m`, `24h`, `7d`, `2w` |
| `-top` | `20` | how many templates to list |
| `-examples` | off | print a real line under each template |
| `-format` | `table` | `table`, `json` or `markdown` (handy for pasting into a ticket) |
| `-suggest` | off | list templates to drop or sample, with savings |
| `-filter` | | print a config for `vector`, `fluentbit` or `otel` that drops the suggested lines |
| `-diff` | off | compare two inputs, `logcost -diff before.log after.log` |
| `-max-growth` | | with `-diff`, exit with code 3 if the volume per day grew more than this, e.g. `20%` |
| `-multiline` | on | join stack traces and indented lines into one event, `-multiline=false` to count every line |
| `-similarity` | `0.4` | lower merges more lines into one template, higher splits them |
| `-no-redact` | off | show secrets and emails as they are, see below |

The monthly cost is `size / period * 30 days * price`. The period comes from the first and last timestamp it finds. If your logs have no timestamps, or you piped in a slice of a bigger stream, set `-period` yourself. Prices vary a lot between vendors and plans, so put in your own number.

It understands plain text logs (ISO timestamps, syslog, nginx/apache access logs) and JSON logs. For JSON it takes the template from `msg`/`message`, the level from `level`/`severity` (pino numbers work too) and the time from `time`/`ts`/`@timestamp`.

## How it works

Lines that continue an event (indented lines, `Caused by:`, Java exception lines, Python tracebacks) are glued to the line that started it. The event is counted with its full size, but the template comes from the first line.

Each event then gets its variable parts replaced with `<*>`: numbers, durations, IPs, UUIDs, hashes, dates, emails, IDs inside URL paths. After that the events go through [Drain](https://jiemingzhu.github.io/pub/pjhe_icws2017.pdf), a log parsing algorithm that puts lines with the same length and first word into a tree and merges them when enough words match. Lines that turn out identical after masking skip the tree through a cache, which is why repetitive logs are fast.

Reading and parsing run on all CPU cores, the template tree is updated in one goroutine in the original line order, so you get the same result no matter how many cores you have. On a 6-core laptop it does about 160 MB/s; 1 GB took 6 seconds and 80 MB of memory.

## Your logs and your data

Logs are full of things that shouldn't end up in a ticket or a screenshot, so:

- Passwords, tokens, API keys, `Authorization` headers, JWTs, cloud keys (AWS, GitHub, Slack, Stripe, Google, GitLab, npm), credentials inside URLs, private key blocks, card numbers and emails are replaced with `<redacted>`, `<email>` or `<card>` in every output format. `-no-redact` turns this off, use it only on your own machine.
- Control characters from the logs are printed as text (`\x1b[31m`), so a log line can't clear your screen, change the terminal title or fake output. Same for Unicode direction overrides.
- Lines are cut at 64 KB and the number of templates is capped at 10,000. A huge or hostile file, including a gzip bomb, can't make it eat all your memory.
- It reads only the files you pass, writes only to stdout and never touches the network. There are no dependencies outside the Go standard library.

Every push runs `go vet`, `staticcheck`, `govulncheck`, tests with the race detector and fuzzing of the parsers. Found a security problem? See [SECURITY.md](SECURITY.md).

## Building

```bash
go build ./cmd/logcost
go test ./...
go run ./tools/loggen -size 100MB > sample.log   # fake logs to play with
```

## License

MIT
