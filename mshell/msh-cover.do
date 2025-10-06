#!/bin/sh
redo-ifchange ./*.go
go build -cover '-coverpkg=./...' -o "$3"
