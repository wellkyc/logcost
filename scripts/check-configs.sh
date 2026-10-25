#!/bin/sh
set -eu

VECTOR_IMAGE="timberio/vector:0.60.0-debian"
FLUENTBIT_IMAGE="fluent/fluent-bit:5.1.5"
OTEL_IMAGE="otel/opentelemetry-collector-contrib:0.163.2"

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
chmod 777 "$work"

go build -o "$work/logcost" ./cmd/logcost
go run ./tools/loggen -size 20MB -seed 7 > "$work/sample.log"
"$work/logcost" -suggest -format json "$work/sample.log" > "$work/suggest.json"
"$work/logcost" -filter vector "$work/sample.log" > "$work/vector-filter.yaml"
"$work/logcost" -filter fluentbit "$work/sample.log" > "$work/fluentbit-filter.conf"
"$work/logcost" -filter otel "$work/sample.log" > "$work/otel-filter.yaml"

{
    echo "sources:"
    echo "  in:"
    echo "    type: stdin"
    sed 's/your_source/in/' "$work/vector-filter.yaml"
    echo "sinks:"
    echo "  out:"
    echo "    type: console"
    echo "    inputs: [logcost_sample]"
    echo "    encoding:"
    echo "      codec: text"
} > "$work/vector.yaml"
docker run --rm -i -v "$work:/w" "$VECTOR_IMAGE" --quiet --config /w/vector.yaml < "$work/sample.log" > "$work/vector.out"
python3 scripts/check_filtered.py "$work/sample.log" "$work/suggest.json" text "$work/vector.out" sample

{
    printf '[SERVICE]\n    Flush 1\n    Log_Level warn\n'
    printf '[INPUT]\n    Name tail\n    Path /w/sample.log\n    Read_from_Head true\n    Exit_On_Eof true\n'
    cat "$work/fluentbit-filter.conf"
    printf '[OUTPUT]\n    Name stdout\n    Match *\n    Format json_lines\n'
} > "$work/fluentbit.conf"
docker run --rm -v "$work:/w" "$FLUENTBIT_IMAGE" /fluent-bit/bin/fluent-bit -c /w/fluentbit.conf > "$work/fluentbit.out"
python3 scripts/check_filtered.py "$work/sample.log" "$work/suggest.json" fluentbit "$work/fluentbit.out" drop

{
    printf 'receivers:\n  filelog:\n    include: [/w/sample.log]\n    start_at: beginning\n'
    sed '/^#/d' "$work/otel-filter.yaml"
    printf 'exporters:\n  file:\n    path: /w/otel.out\n'
    printf 'service:\n  pipelines:\n    logs:\n      receivers: [filelog]\n      processors: [filter/logcost]\n      exporters: [file]\n'
} > "$work/otel.yaml"
docker run --rm -v "$work:/w" "$OTEL_IMAGE" validate --config /w/otel.yaml
timeout 40 docker run --rm --name logcost-otel -v "$work:/w" "$OTEL_IMAGE" --config /w/otel.yaml > "$work/otel.log" 2>&1 &
for _ in $(seq 1 30); do
    sleep 1
    if python3 scripts/check_filtered.py "$work/sample.log" "$work/suggest.json" otel "$work/otel.out" drop >/dev/null 2>&1; then
        break
    fi
done
docker stop logcost-otel >/dev/null 2>&1 || true
wait || true
python3 scripts/check_filtered.py "$work/sample.log" "$work/suggest.json" otel "$work/otel.out" drop

echo "all collector configs work"
