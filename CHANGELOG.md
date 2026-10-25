# Changelog

## 0.2.0

- `-suggest` lists templates worth dropping or sampling (debug and trace output, health checks, the biggest info templates) with the money it saves; warnings and errors are never suggested
- `-filter vector`, `-filter fluentbit` and `-filter otel` print a ready config that drops those lines; CI runs every config through the real tool to check it drops exactly what it should
- stack traces (Java, Python) and indented continuation lines are counted as one event with the line that started them, so they no longer show up as hundreds of separate templates; turn it off with `-multiline=false`
- the table shows events instead of lines, JSON output has both

## 0.1.0

First release.

- groups log lines into templates with Drain and shows size, line count, level and monthly cost for each
- plain text and JSON logs, gzip input, stdin
- monthly cost from timestamps or `-period`
- table, JSON and Markdown output
- hides secrets and emails in the output, escapes terminal control characters
- prebuilt binaries for Windows, Linux and macOS with checksums and build attestations
