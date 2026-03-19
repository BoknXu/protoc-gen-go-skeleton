SHELL := /bin/bash

APP := protoc-gen-go-skeleton
CMD_PKG := ./cmd/$(APP)

.PHONY: help install build test tidy proto

help:
	@echo "可用命令："
	@echo "  make install   # 使用 go install 安装插件到 GOBIN"
	@echo "  make build     # 本地构建二进制到 ./bin"
	@echo "  make test      # 运行 go test"
	@echo "  make tidy      # 整理 go.mod/go.sum"
	@echo "  make proto     # 调用 protoc（可通过 DOMAIN 传参）"

install:
	go install $(CMD_PKG)

build:
	@mkdir -p ./bin
	go build -o ./bin/$(APP) $(CMD_PKG)

test:
	go test ./...

tidy:
	go mod tidy

# 示例：
# 1) make proto PROTO_FILES="./welcome/*.proto"
# 2) make proto DOMAIN=welcome PROTO_FILES="./**/*.proto"
proto:
	@if [ -z "$(PROTO_FILES)" ]; then \
		echo "请传入 PROTO_FILES，例如：make proto PROTO_FILES=\"./welcome/*.proto\""; \
		exit 1; \
	fi
	@if [ -n "$(DOMAIN)" ]; then \
		protoc --proto_path=. --go_out=. --go-skeleton_out=domain=$(DOMAIN):. $(PROTO_FILES); \
	else \
		protoc --proto_path=. --go_out=. --go-skeleton_out=. $(PROTO_FILES); \
	fi
