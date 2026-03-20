# protoc-gen-go-skeleton

`protoc-gen-go-skeleton` 是一个自定义 `protoc` 插件。

它会扫描 `.proto` 里的 `service` / `rpc` 定义，并生成：

- 每个 `service` 对应一个 `Application` 结构体
- 每个 `service` 对应一个构造函数
- unary rpc 的空实现

默认生成文件命名：

- `<service>.go`（例如 `welcome.go`）
- 输出目录由 `--go-skeleton_out` 决定

## 1）安装

编译并安装到你的 `GOBIN`：

```bash
go install ./cmd/protoc-gen-go-skeleton
```

## 2）proto 示例

```proto
syntax = "proto3";

package demo.user.v1;

option go_package = "example.com/demo/user/v1;userv1";

service UserService {
  rpc GetUser(GetUserRequest) returns (GetUserResponse);
  rpc ListUsers(ListUsersRequest) returns (stream ListUsersResponse);
}

message GetUserRequest {
  string id = 1;
}

message GetUserResponse {
  string id = 1;
  string name = 2;
}

message ListUsersRequest {}

message ListUsersResponse {
  string id = 1;
}
```

## 3）生成代码

```bash
# 简单使用
protoc --proto_path=. \           # 导入依赖的pb文件
       --go_out=. \               # protoc-gen-go插件生成go代码的输出路径
       --go-skeleton_out=paths=source_relative:{{输出目录}} \   # protoc-gen-go-skeleton插件生成application代码输出路径
       path/to/your.proto         # 目标pb文件
```

执行后你会得到：

- `path/to/your.pb.go`（由 `protoc-gen-go` 生成）
- `{{输出目录}}/{{service}}.go`（由 `protoc-gen-go-skeleton` 生成）


## 4）生成结果风格

针对每个 `service`，插件会生成：

- 结构体 `<ServiceBaseName>Application`
- 构造函数 `New<ServiceBaseName>Application()`
- 方法：
  - `func (app *XxxApplication) Hello(ctx context.Context, req *welcomePB.HelloReq) (*welcomePB.HelloReply, error)`

暂仅支持 Unary rpc，不支持流式 rpc。
