# Changelog

## Unreleased

### 修复 Go 审查发现

- 业务超时后的错误 XML 使用父请求下独立的 1 秒读取窗口，不屏蔽父请求取消。
- Observer panic 前已接管的内容及 Context 有统一清理责任；移交完成后仍由 `Plan.Finish` 负责。
- REST 资源宣告共用目录矩阵、图层 Limits、行列和像素边界；目录和 REST 共用维度保留字规则。
- WMS 列表在展开前检查数量，REST 未命中候选不再分配字段 Map。
- 补齐公共 API GoDoc，增加注释门禁、goimports 和各模块 Staticcheck 检查。
- 新增超时、资源关闭、父取消、固定矩阵、保留字、分配与 REST fuzz 回归。

### 未发布 API 调整

- `wmts.Router.Resources` 的第二个参数由 `Layer` 改为 `Catalog`。调用方应传入已验证的完整目录；不保留无法验证固定矩阵的旧签名。
- WMTS 维度名进一步拒绝 Capabilities 操作的保留参数，避免 KVP 与 REST 配置不一致。

### 新增

- WMS 1.0.0、1.1.0、1.1.1、1.3.0 请求模型、版本化能力文档与异常。
- WMTS 1.0.0 KVP / REST、GetCapabilities、GetTile、GetFeatureInfo 与目录约束。
- 原始 KVP、轴序、样式空值、矩阵标识、Limits、维度和可信 URL 处理。
- 标准 HTTP、Fiber v3 与 Gin 接入示例，复用同一协议核心，不提供公共框架适配包。
- 明确的 Payload / Plan 所有权、HEAD、流错误、协作取消与真实 TCP 测试。
- 官方固定 Schema/DTD 离线验证、多模块检查和未发布模块独立消费测试。
- Go 与 Markdown/JSON/YAML 格式化入口、中文接入文档。

### 调整

- Fiber 适配实现与全部测试迁入 `examples/fiber3`，使用示例内未导出的 `ogcHandler`；移除原独立适配器模块及其依赖、工作区和消费测试引用。该次迁移未修改协议核心或 HTTP 响应逻辑。

- 标准 HTTP 适配实现与测试进一步迁入 `examples/internal/nethttp`，供标准 HTTP 与 Gin 示例共用；移除原公共适配路径，不保留兼容包装器。现有业务需要参考示例自建接入层，协议与响应处理接口不变。

### 明确边界

项目只处理协议，不生成地图、瓦片或查询数据。不宣称 OGC 认证；GetLegendGraphic、POST/SOAP 和额外框架便捷模块不在当前交付范围。
