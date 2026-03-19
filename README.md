# protoc-gen-go-skeleton

`protoc-gen-go-skeleton` 是一个自定义 `protoc` 插件。

它会扫描 `.proto` 里的 `service` / `rpc` 定义，并生成：

- 每个 `service` 对应一个 `Application` 结构体
- 每个 `service` 对应一个构造函数
- unary rpc 的空实现（保留 `// coding here...` 并返回空响应）

默认生成文件命名：

- `<service>.go`（例如 `welcome.go`）
- 输出目录由 `--go-skeleton_out` 决定

## 1）安装

编译并安装到你的 `GOBIN`：

```bash
go install ./cmd/protoc-gen-go-skeleton
```

请确保 `$(go env GOPATH)/bin`（或你的 `GOBIN`）已加入 `PATH`。

也可以使用 `Makefile`：

```bash
make install
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
protoc \
  --proto_path=. \
  --go_out=. \
  --go-skeleton_out=. \
  path/to/your.proto
```

执行后你会得到：

- `path/to/your.pb.go`（由 `protoc-gen-go` 生成）
- `<输出目录>/<service>.go`（由 `protoc-gen-go-skeleton` 生成）

### 可选参数：`domain`

你可以通过 `--go-skeleton_out` 传递插件参数。

- 不传 `domain`：对当前 `CodeGeneratorRequest` 里的所有 proto 生成
- `domain=welcome`：只处理 `welcome/` 下的 proto，并只输出一个文件：`<输出目录>/welcome.go`

示例：

```bash
protoc \
  --proto_path=. \
  --go_out=. \
  --go-skeleton_out=paths=source_relative,domain=welcome:./internal/application \
  $(find . -name "*.proto")
```

## 4）生成结果风格

针对每个 `service`，插件会生成：

- `<ServiceBaseName>Application`
- `New<ServiceBaseName>Application()`
- 嵌入 `welcomePB.Unimplemented<ServiceName>Server`（pb 包使用 `xxxPB` 别名）
- unary 方法：
  - `func (app *XxxApplication) Hello(ctx context.Context, req *welcomePB.HelloReq) (*welcomePB.HelloReply, error)`

流式 rpc 不会生成自定义方法，保持嵌入的 `Unimplemented...Server` 默认行为。
