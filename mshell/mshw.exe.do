#!/bin/sh
set -e
redo-ifchange ./*.go
export GOOS=windows
export GOARCH=amd64
# This is to build a console app that won't bring up a console window,
# useful for running things within something like 'Task Scheduler'
go build -ldflags "-H=windowsgui" -o "$3"
