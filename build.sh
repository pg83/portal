#!/bin/sh

export CGO_ENABLED=0
export GO_EXTLINK_ENABLED=0
export PATH="${HOME}/ix:${PATH}"

exec ix run set/dev/go -- go build
