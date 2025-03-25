#!/bin/sh
redo-ifchange ./*.go
go build -o "$3"
