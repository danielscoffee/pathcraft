test:
	@go test ./... -v -cover

build:
	@go build -o ./bin/pathcraft ./cmd/pathcraft

clean:
	@rm -f ./bin/pathcraft

.PHONY: test build clean