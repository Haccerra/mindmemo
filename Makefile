BINARY := mindmemo
VERSION ?= dev
DIST_DIR := dist

.PHONY: build test release install qa-manual clean

build:
	go build -o $(BINARY) ./cmd/mindmemo

test:
	go test ./..

release:
	@mkdir -p $(DIST_DIR)
	GOOS=darwin  GOARCH=arm64 go build -o $(DIST_DIR)/$(BINARY)-$(VERSION)-darwin-arm64      ./cmd/mindmemo
	GOOS=darwin  GOARCH=amd64 go build -o $(DIST_DIR)/$(BINARY)-$(VERSION)-darwin-amd64      ./cmd/mindmemo
	GOOS=linux   GOARCH=amd64 go build -o $(DIST_DIR)/$(BINARY)-$(VERSION)-linux-amd64       ./cmd/mindmemo
	GOOS=windows GOARCH=amd64 go build -o $(DIST_DIR)/$(BINARY)-$(VERSION)-windows-amd64.exe ./cmd/mindmemo
	cd $(DIST_DIR) && shasum -a 256 * > checksums.txt

install:
	go build -o /usr/local/bin/$(BINARY) ./cmd/mindmemo

qa-manual:
	./scripts/qa_terminal.sh

clean:
	rm -rf $(BINARY) $(DIST_DIR)

