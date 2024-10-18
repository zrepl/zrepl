#!/usr/bin/env bash

# This script converts output that was produced by zrepl and captured by Graylog
# back to something that the scraper in this package's main can understand
# Intended for human syslog
# logging:
# - type: syslog
#   level: debug
#   format: human


csvfilter --skip 1 -f 0,2 -q '"' --out-quotechar=' ' /dev/stdin | sed -E 's/^\s*([^,]*), /\1 [LEVEL]/' | \
    go run . -v \
    --dateRE '^([^\[]+) (\[.*)' \
    --dateFormat '2006-01-02T15:04:05.999999999Z'

