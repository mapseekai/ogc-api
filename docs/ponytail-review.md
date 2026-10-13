# 复用设计记录

项目把协议共性集中在参数、目录和响应层，版本差异保留在对应协议实现中。下表说明当前代码中的主要复用关系。

## 共享实现

| 能力                     | 共享位置                    | 使用方                     |
| ------------------------ | --------------------------- | -------------------------- |
| 原始查询读取、列表与数值 | `kvp`                       | WMS、WMTS KVP、REST 绑定   |
| 协议类型、错误与维度     | `owscore`                   | 请求、目录、引擎与异常编码 |
| XML 转义和字节限额       | `internal/xmlutil`          | Capabilities、异常文档     |
| 矩阵、Limits 与像素范围  | `wmts/catalog.go`           | 请求校验、REST 资源宣告    |
| 内容类型、长度和关闭     | `response`                  | 业务回调、引擎、宿主发送   |
| HTTP 同步写出            | `examples/internal/nethttp` | 标准 HTTP、Gin 示例        |
| 真实 TCP 契约            | `adaptertest`               | 三种 Web 接入测试          |

## 请求路径

KVP 解析产生可复用的 Params，REST 的 `MatchParams` 接收这份参数。WMS 版本共性通过同一请求模型组织，操作名称、参数名、轴序和 XML 差异分别处理。

列表解析先检查元素数，再构造结果。REST 先匹配候选路径，再创建字段 Map。资源控制与正常路径共用同一组规则。

## 内容路径

Provider 返回 Payload，Engine 构造 Plan，宿主执行发送与 Finish。关闭、完成事件和 Context 取消依照一次所有权交付流程执行。

异常文档由协议编码器产生，图片、瓦片和查询内容由业务回调提供。HTTP 与 Fiber 示例沿用同一套引擎错误分类。

## 目录与更新

Snapshot 深拷贝目录数据，应用通过替换快照引用更新资源视图。Capabilities 与内容校验使用同一个目录；REST 资源地址与请求匹配使用同一模板。

动态授权由 CatalogProvider 选择当前调用者的资源，业务维度规则通过回调接入。

## 工程入口

格式、GoDoc、Staticcheck、测试、race、XML 和基准统一由 `scripts/check.sh` 调用。开发工具安装在 `.artifacts/tools`，构建结果写入 `.artifacts/check/build`。

验证结果见 [测试记录](test-report.md)，八项实现行为与测试的对应关系见 [行为与回归记录](go-review-fixes.md)。
