#!/bin/sh
export GOOS=windows
export GOARCH=amd64
go build -o "$3"
