# 官方符合性测试记录 — 2026-10-15

## 结论

**原版官方测试尚未全部通过，不能宣称完整 CITE 通过或获得 OGC 认证。** 继续修复后，Processes 原版达到50通过、0失败，且输入验证负例实际执行；WMTS 小写 all 对应测试通过。六个正式套件剩余2个直接失败，均为 WMTS I/J 异常码断言；Maps/Coverages 后续补齐功能并消除原有14个失败，原版开发定义分别达到15/0/9、5/0/0；仍不将跳过或人工视觉断言算作通过。

执行时基线为 `8a8ff6146747b928a93accab61d1c2c3c250f6ee` 加本地改动；后续提交与推送状态以 Git 历史为准，未进行版本发布。执行环境为 macOS ARM64、官方 Linux/amd64 镜像及固定 Maven/Java 17。最新完整结果、逐项状态和指纹保存在 `.artifacts/cite-20261015-maps/results.json`；前一批全部协议证据在 `.artifacts/cite-20261015-all/results.json`；前两轮原始记录保留在 `.artifacts/cite-20261014/`、`.artifacts/cite-20261015/`。

## 最终原始结果

| 协议 / 套件 | 通过 | 直接失败 | 跳过 | 范围与附加状态 |
| --- | ---: | ---: | ---: | --- |
| Features ETS 1.9 | 100 | 0 | 25 | Part 1 Core/GeoJSON；其余为未声明 Part 2 CRS |
| Tiles ETS 1.2 | 15 | 0 | 1 | 集合瓦片；未声明 dataset-tilesets |
| WMS 1.3.0 ETS 1.34 | 163 | 0 | 0 | Basic、Queryable 必需组 |
| WMS 1.1.1 ETS 1.23 | 99 | 0 | 0 | 通用、Basic、Queryable、严格 bbox 组均进入 |
| Processes ETS 1.3 | 50 | 0 | 4 | 同步/异步、JSON/纯文本双输出、Jobs、运行中与非法格式输入；其余输入/输出类型未声明 |
| WMTS ETS 1.11 | 91 | 2 | 13 | 原始 CTL 全组；另有3继承失败、9 CONTINUE |
| Maps 1.10-SNAPSHOT | 15 | 0 | 9 | 未修改官方开发源码；数据集/集合地图、集合选择、CRS 发现、PNG/JPEG/TIFF、CORS；非认证发布套件 |
| Coverages 0.1-SNAPSHOT | 5 | 0 | 0 | 未修改官方旧草案开发源码；全部5项元数据定义通过，另做官方 CIS schema 校验 |

TestNG 采用报告原始计数。CTL 计数包含汇总节点，不能等同于独立规范条款数；继承失败、跳过及 CONTINUE 均不算通过。WMS 本轮单独验证必需类，先前5项推荐元数据失败仍保留在旧批次，不能宣称推荐项全部满足。没有以关闭已声明能力、减少集合或修改官方断言消除失败。

## 实现缺陷及修复

- WMS：VERSION 优先于 WMTVER；1.1.1 历史操作别名；1.3 GetCapabilities 忽略不支持的 FORMAT；未知查询图层返回 LayerNotDefined；根图层公共 CRS/地理范围及命名图层 BoundingBox。新增的 `Layer.BoundingBoxes` 使用 XY 坐标，编码时转换轴序，不隐式重投影。
- WMTS：将小写 Sections=all 规范化为 All，不放宽其他节名；KVP GetFeatureInfo 必需且合法的 FORMAT、未知 REQUEST 异常、不可查询图层、GetCapabilities 异常省略不允许的 locator、局部能力响应省略未请求的 ServiceMetadataURL，以及 按实际返回媒体参数处理 REST Accept/406/Vary。net/http 和 Fiber 示例均传递 Accept。
- Tiles：OpenAPI 使用集合数据类型及 getTile/getTileSet/getTileSetsList 操作后缀。混合数据类型路径仍不自动宣称完整 OAS 类。
- Processes：补上过程列表 limit/cursor 分页、默认数量限制和 OpenAPI 参数。原始多输出按规范要求29返回 multipart/related，输出格式由执行文档选择；document 和单输出仍校验 Accept。异常响应按 Accept 支持 application/json 与 application/problem+json，并设置 Vary: Accept；OpenAPI 同步描述两种表示。部署符合性通过 `Catalog.ConformsTo` 显式补充，由宿主承担后端履约责任，不从路由存在推断完整标准。
- 每个修复有请求、元数据或异常回归；保留原有 WMTS 测试，更新并检查 WMS 黄金 XML。

## 宿主配置和测试数据

专用宿主复用现有引擎与标准库，没有新增运行依赖：

- Blue Lake 官方 PNG/worldfile 与 Polygon/MultiPolygon 保持实际范围、透明合成和要素身份；两版 WMS 分别挂载，图像限额与 UpdateSequence 比较一致。
- Features 七个合成点覆盖赤道、极区和日期线两侧，真实执行 bbox、datetime、分页和单要素查询。原来因空结果跳过的5项 bbox 内容断言现已通过，95/0/30 提升至100/0/25。
- WMTS 增加三个受限矩阵、Time 默认值/current 和不可查询图层。每种表示提供唯一 REST 地址，避免套件选择不包含维度的另一条合法模板；新增组确实执行。
- Processes 的 message 输入产生 message（JSON）和 copy（text/plain）两个真实输出；可选 text 输入可覆盖纯文本副本，并提供格式化输入负例。同步/异步共用有界内存 Jobs，0—5秒 pause 验证运行中与结果未就绪。作业每次读取重建独立结果；支持 qualified value 和宿主自有引用，拒绝任意 URL 抓取，不提供生产持久化或调度。
- WMS 1.1.1 包装器会把 profile=queryable 转换成 queryable_profile，原始日志确认 Basic/Queryable 已进入；两种写法都得到99通过，不能把参数转换误判为新增修复。
- Maps 开发代码对 noofcollections=-1 使用 Math.min，导致循环不执行。本轮改用100000遍历全部发布集合；因此比旧报告暴露更多断言，而非回退。

## 套件问题：原版失败保持可审计

1. **WMTS 两个像素越界断言：** 套件要求 TileOutOfRange，规范07-057r7表26和规范性 ATS A.3.6.4 要求 PointIJOutOfRange；实现返回该规范代码，I/J locator 与400正确。官方已登记且本轮核实仍开放的[问题99](https://github.com/opengeospatial/ets-wmts10/issues/99)描述同一矛盾。Maven Central 最新正式版仍为1.11；元数据、问题原文、独立 I/J 请求重放与完整原始日志均保留。不能通过改成错误代码、增加虚假异常或关闭 KVP FeatureInfo 来宣称符合性通过。REST 对应越界组通过。
2. **先前 Processes 失败现已解决，但并非仅套件问题：** 单个原始 JSON 字符串触发官方 oneOf 重叠；补充真实 JSON/文本双输出后避开该重叠，并保留单输出回归。套件的 JavaMail multipart 检查不能从 JSON 分部的 toString() 读正文，因此第二个输出按公开声明返回实际纯文本。继续补足可选格式化输入后，非法输入负例暴露引擎固定返回 problem+json 的协商问题，修复后该用例也实际通过。最终为50/0/4，没有把该负例留在跳过状态。
3. **WMTS Sections.All 已解决：** 支持小写 all 的兼容别名，原始官方用例通过；规范拼写 All 与其他节名校验保持有效。
4. **Maps 客户端二次编码已通过合法链接消除：** 原版 RestAssured 会二次编码 f=image%2Fpng。本轮地图发现链接改为统一可协商的 /map 地址，保留各表示的 type 与原 f 参数支持。示例提供真实 PNG/JPEG/TIFF，原版三个内容用例均通过，不接受错误双重编码参数。
5. **Maps CRS 比较已通过规范允许的默认表示解决：** CRS84 响应省略规范允许省略的 Content-Crs，其他 CRS 仍用带尖括号 URI；有默认/非默认 CRS 回归。没有返回错误 CRS 或修改官方比较器。

WMTS 另外保留13个跳过和9个无判定：包括未配置 Themes/Legend/其他格式、无适用可选参数，以及无法从固定资源模板构造某些负例。Sections 空列表被套件当缺失值，也是需复核的条件性跳过。没有将这些状态折算成通过。官方导出器不能完整表示 CONTINUE，原始 split logs 为准。

## 有限 profile 与发布范围

本轮增加 `Catalog.Map`，复用 maps.Service 发布数据集 /map，按调用顺序验证本地集合 ID/URL，默认按 ID 排序，拒绝空、重复、外部或超限选择；landing page 提供 extent/CRS/地图链接，集合地图补充 CRS。DomainSet/RangeType 必须成对提供非空 CIS JSON，并按预算复制、发布独立资源和 OpenAPI，提供者负责数据语义一致。

组合示例现在渲染与 Features 相同的 Origin 点，真实处理 bbox、尺寸和背景；PNG 保留 alpha，JPEG/TIFF 可被标准解码器识别。TIFF 为示例专用单 strip、未压缩8位 RGBA，不是生产 GeoTIFF 编码器。公开示例宿主启用 CORS 和预检，协议包不依赖 Web/GIS/编码 SDK，未新增运行依赖。覆盖仍是原点一个20°C单元，CIS JSON、CoverageJSON、domainset、rangetype 与子集行为保持一致；数值切片0.0、-0、0e0按数值等价处理，有请求和读取回归；官方 CIS schema 独立验证三份 JSON，GDAL 独立验证 TIFF。

原有 Maps/Coverages 14个失败全部消除；9项 Maps 跳过与未启用的人工颜色/描绘一致性问答不算通过。示例验证不替代生产 GIS 后端、投影、持久化或全部可选标准类验证。

Maps/Coverages 使用固定官方开发源码执行全部定义，但不是已发布完整认证 ETS。旧 Coverages 的5项检查即使全绿，也不覆盖读取/子集。未找到 Common、WMS 1.0.0、1.1.0 的独立正式 ETS；时间/高程与 XML POST/SOAP 不在本宿主声明范围。

## 检查与证据

最终完整检查为 `GOTOOLCHAIN=go1.27.2 ./scripts/check.sh --consumers`；最低 Go 1.24 另行运行根模块全部测试。最终机器报告同时保存源码、数据及结果 SHA-256；修改后的本地工作树不是远端 CI 通过证据。

- 本轮正式套件：features.xml、tiles.xml、processes-latest.xml、wms13.xml、wms11.xml。
- WMTS：wmts-session/（原始日志、EARL、HTML）、wmts-console.log，以及 wmts-I.xml / wmts-J.xml 独立重放。ogc-wmts99.json 和 ogc-wmts-versions.xml 固定上游状态证据。
- 最新开发套件：`.artifacts/cite-20261015-maps/maps-complete-output/`、`coverages-complete-output/`，原始15/0/9和5/0/0。Features/Tiles/Processes 也在该批次重跑，分别100/0/25、15/0/1、50/0/4；WMS/WMTS沿用前批原始结果，未在本轮重跑未改动的服务器核心。
- 最新工程检查：`.artifacts/cite-20261015-maps/project-check-report.json`、`project-check-complete.log`、`minimum-go-complete.log`。schema-verification.log、cis-schema.json、tiff-gdal.json 保存独立内容验证。最新机器清单记录精确源码与产物指纹，保留历次原始运行。

官方依据：[WMTS 07-057r7](https://docs.ogc.org/is/07-057r7/07-057r7.pdf)、[Processes 18-062r2](https://docs.ogc.org/is/18-062r2/18-062r2.html)、[Maps 20-058](https://docs.ogc.org/is/20-058/20-058.html)、[CITE 目录](https://cite.opengeospatial.org/te2/)。重现说明见 [CITE 接入指南](cite.md)，发布结论见 [发布评估](release-readiness-2026-10-15.md)。
