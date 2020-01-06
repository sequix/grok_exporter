#!/bin/sh
nohup go run grok_exporter.go -loglevel debug -config config.yml &>log &
