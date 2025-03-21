#!/usr/bin/zsh
GOOS=windows GOARCH=amd64 go build -o epubzipper_windows_amd64.exe && \
GOOS=linux GOARCH=386 go build -o epubzipper_linux_386 && \
GOOS=linux GOARCH=arm64 go build -o epubzipper_linux_arm64 && \
GOOS=darwin GOARCH=amd64 go build -o epubzipper_darwin_amd64 && \
GOOS=darwin GOARCH=arm64 go build -o epubzipper_darwin_arm64