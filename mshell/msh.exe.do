#!/bin/sh
set -e
redo-ifchange ./*.go
export GOOS=windows
export GOARCH=amd64
go build -o "$3"
