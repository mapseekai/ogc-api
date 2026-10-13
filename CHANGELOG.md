# Changelog

## 开发版本

### 协议能力

- WMS 1.0.0、1.1.0、1.1.1、1.3.0 的请求模型、目录校验、Capabilities 和异常编码。
- WMTS 1.0.0 KVP / REST 的 GetCapabilities、GetTile、GetFeatureInfo 与矩阵约束。
- 原始参数、坐标轴顺序、空样式项、字符串矩阵标识、图层 Limits、维度和可信公开地址处理。
- 字节与流内容、缓存元信息、可配置 HEAD，以及请求阶段观察事件。

### 接入与复用

- 标准 HTTP、Fiber v3 和 Gin 示例共用同一协议核心。
- Fiber 接入与测试位于 `examples/fiber3`，使用示例内 `ogcHandler`。
- 标准 HTTP 与 Gin 共享 `examples/internal/nethttp` 的发送实现和生命周期测试。
- 公共核心、框架示例与开发工具分别维护各自的配置。

### 可靠性与资源控制

- 错误 XML 使用继承父请求取消的独立 1 秒读取窗口。
- 引擎与接入层按 Payload / Plan 所有权执行关闭、Context 取消与异常清理。
- REST 资源宣告共用目录中的矩阵、图层 Limits、行列及像素规则。
- 目录与 REST 编译共用维度保留字校验。
- WMS 列表在展开前检查数量，REST 在完整候选路径命中后创建字段 Map。

### API 更新

`wmts.Router.Resources` 当前签名为 `Resources(layerID, catalog, baseURL, featureInfo)`，第二个参数采用完整 `wmts.Catalog`。初始调用中传入的 `Layer` 应调整为已验证 Catalog，以提供矩阵和 Limits 信息。

HTTP 接入代码统一位于示例目录。应用可以采用示例中的请求提取和响应写出方式，连接 `server.Engine` 与 `response.Plan`。

### 工程验证

- 开发工具由 Makefile 与 Python 脚本组织，Go 源码、Python 脚本和文档内 Go 示例沿用各自语言的格式工具。
- 文档文本采用 `.editorconfig` 约定，能力表由统一清单生成和校验。
- 公共 API GoDoc、goimports、Staticcheck 与 go vet 进入统一检查。
- 单元测试、真实 TCP 契约、race、四组 fuzz smoke 与内存分配基准。
- 14 份版本化 XML 夹具和固定官方 Schema/DTD 的本地校验。
- 三个 Go 模块构建、外部核心消费者与示例 internal 规则验证。
- 中文能力说明、快速开始、API 接入、架构与验证文档。
