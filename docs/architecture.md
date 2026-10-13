# 架构说明

`ogc-api` 以协议请求、服务目录和内容响应为中心组织代码。业务应用提供数据读取与处理能力，宿主负责 HTTP 接入和连接管理，协议核心负责解释请求与组织响应。

## 执行流程

```text
宿主路由与认证中间件
        ↓
server.Input：原始 Query、转义路径、Context、服务 Scope
        ↓
server.Engine
        ├── kvp：参数读取
        ├── wms / wmts：版本与操作解析
        ├── Snapshot：目录与资源约束校验
        └── Handlers：调用业务内容接口
        ↓
response.Payload：内容与元信息
        ↓
response.Plan：状态、响应头、Body、完成回调
        ↓
宿主发送内容 → Plan.Finish → 关闭资源与结束 Context
```

## 包职责与依赖

| 包            | 职责                                   | 主要项目内依赖                            |
| ------------- | -------------------------------------- | ----------------------------------------- |
| `owscore`     | 协议标识、版本、错误、维度和部署限额   | Go 标准库                                 |
| `kvp`         | 原始 Query 读取、列表与数值解析        | `owscore`                                 |
| `wms`         | WMS 请求、轴序、目录与能力文档         | `kvp`、`owscore`、`internal/xmlutil`      |
| `wmts`        | WMTS 请求、矩阵、REST 模板与能力文档   | `kvp`、`owscore`、`internal/xmlutil`      |
| `response`    | 字节与流内容、响应头、长度及关闭       | Go 标准库                                 |
| `exception`   | 版本化异常文档与 HTTP 状态映射         | `owscore`、`response`、`internal/xmlutil` |
| `server`      | 目录获取、请求编排、业务回调与观察事件 | 上述公共协议包                            |
| `adaptertest` | 三种宿主共用的真实 TCP 契约测试        | 协议核心与示例夹具                        |

`internal/xmlutil` 集中处理 XML 转义、元素配对和输出字节限额。各协议编码器保留自己的版本结构。

## 接入方式

解析入口包括 `wms.Parse`、`wmts.Parse`、`wmts.Router.Match`，返回操作元信息和类型化参数。应用可以直接把解析结果交给自己的流程。

引擎入口为 `server.New` 和 `Engine.Handle`。引擎使用目录快照完成资源校验，并将结果交给已注册的内容回调。

Web 接入代码位于示例目录。`examples/internal/nethttp` 供标准 HTTP 与 Gin 示例共享；Fiber 的 `ogcHandler` 位于 `examples/fiber3/handler.go`。业务应用参考这些实现，将自己的路由、中间件与协议核心连接。

## 目录快照

`server.NewSnapshot` 先验证目录，再深拷贝其中的 Map 和 Slice。每个请求调用一次 `CatalogProvider`，随后的图层、样式、矩阵和维度校验使用这份快照。

服务更新通过创建新 Snapshot 并替换当前引用完成。动态目录按调用者身份和 `Scope` 提供相应资源视图；固定目录通过 `server.Static` 提供共享快照。

快照中的业务维度验证函数保留调用方的实现。共享回调使用并发安全的业务状态；类型化请求由单次调用独占。

## 内容与资源生命周期

| 阶段                       | 所有者     | 完成动作                            |
| -------------------------- | ---------- | ----------------------------------- |
| 内容回调执行期间           | Provider   | 读取内容并构造 Payload              |
| 回调返回后                 | 引擎       | 校验内容类型与响应元信息，构造 Plan |
| `Engine.Handle` 成功返回后 | 宿主接入层 | 写出状态、响应头与内容              |
| 发送完成、HEAD 或发送异常  | 宿主接入层 | 执行 `Plan.Finish`                  |

`response.Bytes` 以只读方式引用字节切片。`response.Stream` 传递 `io.ReadCloser`，通过共享关闭保护管理内容释放。`Plan.Finish` 汇总发送和关闭结果，执行完成回调；Plan 全程以指针持有。

引擎在执行外部钩子之前建立清理责任。发生异常中断时，尚由引擎持有的内容会被关闭，Context 随之结束，panic 继续交给宿主处理。已交付的 Plan 由接入层完成。

## 超时与观察事件

`Config.Timeout` 覆盖目录读取、业务回调和正常内容读取，零值沿用父请求取消。协议错误文档使用父请求下独立的 1 秒读取窗口。宿主配置连接写超时，数据源通过 Context 参与协作取消。

Observer 同步接收 `start`、`business`、`handoff`、`finish` 事件。`ProtocolError` 表示协议结果；`Error` 表示处理或传输结果；`BytesRead` 统计服务端读取并交给传输的内容字节。

## 共享规则与资源控制

KVP 与 REST 复用同一参数模型。`MatchParams` 接收已解析的 Query，目录和模板编译共用维度保留字判断。

WMTS 资源宣告使用完整 Catalog，并复用矩阵范围、图层 Limits 和像素边界校验。自定义维度验证在请求阶段执行，枚举维度可直接按目录校验。

WMS 列表先按原始分隔符检查数量，再分配结果。REST 先匹配完整候选路径，再创建字段 Map。地图与瓦片通过 Reader 逐段发送；预编码 Capabilities 在配置的字节上限内读取和验证。

实现入口：[server](../server/)、[response](../response/)、[WMTS 路由](../wmts/rest.go)。
