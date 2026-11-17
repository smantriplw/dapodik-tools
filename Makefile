.PHONY: build run test fmt vet generate clean

BINARY := dapodik-tool

build: generate
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o $(BINARY) ./cmd/dapodik-tool

run: build
	./$(BINARY) serve

test:
	go test ./...

fmt:
	gofmt -w $$(find . -name '*.go' -not -path './vendor/*')

vet:
	go vet ./...

generate:
	go generate ./...

clean:
	rm -f $(BINARY)
