import json
import re
import sys


def read_output(kind, path):
    lines = []
    with open(path, encoding="utf-8", errors="replace") as f:
        for raw in f:
            raw = raw.rstrip("\n")
            if not raw:
                continue
            if kind == "text":
                lines.append(raw)
            elif kind == "fluentbit":
                lines.append(json.loads(raw)["log"])
            elif kind == "otel":
                for resource in json.loads(raw).get("resourceLogs", []):
                    for scope in resource.get("scopeLogs", []):
                        for record in scope.get("logRecords", []):
                            lines.append(record.get("body", {}).get("stringValue", ""))
    return lines


def main():
    source, suggestions, kind, output, mode = sys.argv[1:6]
    with open(source, encoding="utf-8") as f:
        original = [line.rstrip("\n") for line in f]
    rules = json.load(open(suggestions, encoding="utf-8")).get("suggestions", [])
    drops = [re.compile(r["pattern"]) for r in rules if r["action"] == "drop"]
    samples = [re.compile(r["pattern"]) for r in rules if r["action"] == "sample"]
    if not drops:
        sys.exit("no drop rules to check")

    out = read_output(kind, output)
    dropped_in = sum(1 for line in original if any(p.search(line) for p in drops))
    dropped_out = sum(1 for line in out if any(p.search(line) for p in drops))
    important = lambda line: " ERROR " in line or " WARN " in line or line.lstrip().startswith("at ")
    important_in = sum(1 for line in original if important(line))
    important_out = sum(1 for line in out if important(line))
    expected = len(original) - dropped_in

    problems = []
    if dropped_in == 0:
        problems.append("drop rules match nothing in the input")
    if dropped_out:
        problems.append(f"{dropped_out} lines that should be dropped got through")
    if important_out != important_in:
        problems.append(f"kept {important_out} of {important_in} warning/error lines")
    if mode == "sample" and samples:
        sampled_in = sum(1 for line in original if any(p.search(line) for p in samples))
        sampled_out = sum(1 for line in out if any(p.search(line) for p in samples))
        ratio = sampled_out / sampled_in if sampled_in else 0
        if not 0.05 <= ratio <= 0.15:
            problems.append(f"sampling kept {ratio:.1%} instead of about 10%")
        expected -= sampled_in - sampled_out
    if len(out) != expected:
        problems.append(f"got {len(out)} lines, expected {expected}")

    print(f"{kind}: {len(original)} lines in, {len(out)} out, {dropped_in} dropped by {len(drops)} rules")
    if problems:
        sys.exit(f"{kind}: " + "; ".join(problems))


main()
