.PHONY: build run run-once

build:
	go build -o scout7 ./cmd/scout7

run: build
	./scout7 --config scout7.yaml

run-once: build
	./scout7 --config scout7.yaml --once
