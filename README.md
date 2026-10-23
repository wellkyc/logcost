# logcost

Find out which log lines eat your logging bill.

Most of a log bill usually comes from a handful of lines: health checks, debug output someone forgot to turn off, a retry loop. They're hard to spot because every line is slightly different, with its own IDs, timestamps and durations. logcost groups lines into templates and shows how much each one weighs.

```
$ logcost -price 1.70 app.log
1.0 GB in 9.6M lines, 11 templates, covering 7d
about $7.29/month at $1.70 per GB (set your price with -price)

by level: TRACE 2.8%, DEBUG 23.8%, INFO 63.3%, WARN 5.6%, ERROR 4.5%

 SHARE      SIZE    LINES   $/MONTH  LEVEL  TEMPLATE
 23.7%    237 MB     1.3M     $1.73  INFO   request finished
 22.7%    227 MB     2.9M     $1.66  INFO   INFO [http] GET /healthz <*> in <*> from <*>
 13.7%    137 MB     1.7M     $1.00  DEBUG  DEBUG [cache] hit key=<*> ttl=<*> size=<*>
 10.1%    101 MB   861.5K     $0.74  DEBUG  DEBUG [db] SELECT * FROM orders WHERE user_id = <*> AND status = 'open' LIMIT <*> took <*>
  8.0%   80.4 MB   765.7K     $0.59  INFO   INFO [kafka] committed offset <*> for partition <*> of topic orders.events
  7.2%   72.4 MB   573.5K     $0.53  INFO   INFO [auth] user <*> logged in, session_id=<*> token=<*>
  5.6%   56.3 MB   477.1K     $0.41  WARN   WARN [payments] retrying charge <*>, attempt <*> of <*>: upstream timeout after <*>
  3.2%   31.9 MB   288.3K     $0.23  ERROR  ERROR [orders] failed to reserve stock for order <*>: item <*> out of stock
  5.7%  ... 3 more templates
```

That's a generated week of logs from a made-up shop. In this case health checks and cache hits are a third of everything, and nobody reads them.

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
| `-similarity` | `0.4` | lower merges more lines into one template, higher splits them |
| `-no-redact` | off | show secrets and emails as they are, see below |

The monthly cost is `size / period * 30 days * price`. The period comes from the first and last timestamp it finds. If your logs have no timestamps, or you piped in a slice of a bigger stream, set `-period` yourself. Prices vary a lot between vendors and plans, so put in your own number.

It understands plain text logs (ISO timestamps, syslog, nginx/apache access logs) and JSON logs. For JSON it takes the template from `msg`/`message`, the level from `level`/`severity` (pino numbers work too) and the time from `time`/`ts`/`@timestamp`.

## How it works

Each line gets its variable parts replaced with `<*>` first: numbers, durations, IPs, UUIDs, hashes, dates, emails, IDs inside URL paths. Then the lines go through [Drain](https://jiemingzhu.github.io/pub/pjhe_icws2017.pdf), a log parsing algorithm that puts lines with the same length and first word into a tree and merges them when enough words match. Lines that turn out identical after masking skip the tree through a cache, which is why repetitive logs are fast.

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
