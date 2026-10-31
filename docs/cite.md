# CITE 测试接入指南

本页说明如何将由 `ogc-api`、业务 Provider 和宿主接入层组成的服务接入 OGC 测试套件。测试环境使用与目录声明一致的数据、地图内容和瓦片内容。

2026-10-14 至15已实际执行正式与开发套件，尚未全部通过。具体版本、失败、跳过及原始证据见 [官方测试记录](cite-results-2026-10-15.md)，发布前以最终候选源码复验。

Features后续的Part2多坐标系与独立本地ETS整合见[本页对应章节](#features-part1part2与本地整合验证)；不得把早期记录、本地修正版或纯解析能力当作官方认证。

## 官方套件

| 套件         | 入口                                                       | 服务准备                                    |
| ------------ | ---------------------------------------------------------- | ------------------------------------------- |
| WMS 1.3.0    | [ets-wms13](https://opengeospatial.github.io/ets-wms13/)   | 按测试选项配置图层、CRS、图像格式和查询能力 |
| WMTS 1.0.0   | [ets-wmts10](https://opengeospatial.github.io/ets-wmts10/) | 配置能力文档和套件规定的测试瓦片            |
| OGC 测试平台 | [CITE](https://cite.opengeospatial.org/)                   | 提供测试执行器可访问的服务地址              |

WMS 套件提供 Blue Lake 数据集及对应图层说明。WMTS 套件提供 `noDataSquare.png`，并规定 `TransparentNodata` 图层、`Default_style` 样式、同名矩阵集、矩阵 `0` 和行列 `0/0` 的测试响应。数据配置按各套件的说明固定。

## 服务配置

通过 `server.NewSnapshot` 注册测试目录，使用 `server.Handlers` 连接实际地图与瓦片 Provider。`WMSURL`、`WMTSURL` 和 `RESTBaseURL` 设置为测试执行器可以访问的受控地址。

使用 `GetCapabilities` 核对公开的图层、样式、格式、坐标系、矩阵集、维度和操作。业务 Provider 按相同配置提供地图、瓦片或查询结果。

## 本地预检

服务在 `127.0.0.1:18080` 运行时，可执行以下能力文档检查：

```bash
curl --fail \
  'http://127.0.0.1:18080/wms?SERVICE=WMS&VERSION=1.3.0&REQUEST=GetCapabilities' \
  -o /tmp/ogc-wms-capabilities.xml

curl --fail \
  'http://127.0.0.1:18080/wmts?SERVICE=WMTS&REQUEST=GetCapabilities' \
  -o /tmp/ogc-wmts-capabilities.xml

xmllint --nonet --noout /tmp/ogc-wms-capabilities.xml
xmllint --nonet --noout /tmp/ogc-wmts-capabilities.xml
```

仓库自身的协议、流和固定 XML 夹具可通过 `./scripts/check.sh --consumers` 一并验证。

## 执行与记录

在测试平台选择与服务一致的套件版本，填写该执行环境可访问的 GetCapabilities URL，并按服务声明选择测试选项。固定 Provider 数据集、源码版本和部署配置，保存测试会话和原始输出。

| 记录项 | 内容                               |
| ------ | ---------------------------------- |
| 套件   | 名称、发行版或 commit、测试选项    |
| 服务   | 源码版本、访问地址、宿主配置       |
| 数据   | 数据集版本、图层、样式、矩阵及维度 |
| 执行   | 时间、会话标识、执行器环境         |
| 结果   | 各测试项原始状态、断言和报告       |

协议解析、目录与 XML 的检查定位到核心包；图像尺寸、地理范围、样式和像素内容的检查定位到 Provider；网络访问与响应写出定位到宿主。按这个分工记录和复验结果。

## 专用官方数据宿主

`examples/cite` 使用标准库读取官方 PNG/worldfile、Polygon/MultiPolygon JSON，并复用当前协议引擎与示例 HTTP helper。准备阶段需要 `unzip` 和 GDAL 的 `ogr2ogr`；协议模块和运行宿主没有新增第三方依赖。

使用官方 `ets-wms13` 仓库 `9a45b91e8e1dc600f574002ac89342b20bac3106` 中的 `src/site/resources/png-worldfiles-wms-1.3.0.zip`，提取 PNG/PGW 到数据目录。对 BasicPolygons、Buildings、Forests、Lakes、NamedPlaces、Ponds 逐个转换原始 Shapefile，例如：

```bash
ogr2ogr -f GeoJSON data/Lakes.json \
  ets-wms13/src/main/resources/data/shapefile/Lakes.shp -t_srs OGC:CRS84
```

从官方 `ets-wmts10` 仓库 `b259ee6eb551b171597a33d901cf6d45d156931e` 的 `src/main/resources/nodataSquare.png` 复制同名文件到数据目录。启动时提供该目录和执行器可访问的公开 origin：

```bash
OGC_CITE_DATA="$PWD/data" \
OGC_ADDR=0.0.0.0:18088 \
OGC_PUBLIC_URL=http://host.docker.internal:18088 \
go run ./examples/cite
```

宿主发布 `/wms`（1.3.0）、`/wms11`（1.1.1）、`/wmts`（KVP）、`/wmts/1.0.0/WMTSCapabilities.xml`（REST）、`/tiles/`、`/info/`、`/samples/`、`/sampleinfo/`、`/tilesapi/` 和 `/processapi/`。

Features/Tiles 使用 `/tilesapi`：七个合成点覆盖赤道、极区和日期线两侧，支持真实 bbox、datetime、limit/cursor 查询；瓦片使用注册的 WebMercatorQuad。WMTS 另有三个受限矩阵级别、Time 默认值/current 和不可查询图层。每种表示提供唯一 REST 地址，以便套件能替换相应维度。

Processes 使用 `/processapi`：`message` 输入产生 JSON `message` 和纯文本 `copy` 两个输出，可选 `text` 输入覆盖文本副本并验证非法格式输入；同步/异步执行共用有界内存 Jobs，pause 可选输入限制为0—5秒，以验证运行中状态和结果未就绪响应；支持原始字符串、qualified value 和宿主 `/processapi/input` 引用。引用只允许该宿主资源，不抓取任意 URL。此夹具不提供持久化、调度或完整 job-list 过滤类。

WMS 1.3 参数为 `basic=basic&queryable=queryable`；WMS 1.1.1 REST 包装器使用 `profile=queryable` 并内部转换为 `queryable_profile`。推荐元数据与必需 Basic/Queryable 分开记录。Maps/Coverages 使用组合示例 `/ogc`：数据集与集合地图显示真实 Origin 要素，支持 PNG/JPEG/TIFF 和集合选择；覆盖为原点单元20°C，提供 CIS JSON、CoverageJSON 及一致的 domainset/rangetype。示例公开 API 启用 CORS，生产宿主自行决定访问策略。Maps 开发套件的 `noofcollections` 须使用正数上限（本次100000），其部分实现不能把-1解释为全部集合。

## 完整 WMTS 执行入口

官方 ETS 1.11 的 REST API `wmts-auto.xml` 固定关闭 REST 和 GetFeatureInfo。完整测试使用官方 TEAM Engine 6.0.0-RC2 控制台 `test.sh`、原始 `WMTS_ETS.xml` 和官方表单：

```bash
TE_BASE=/root/te_base sh test.sh \
  -source=/root/te_base/scripts/wmts/1.0.0/ctl \
  -suite=wmts:server.suite.base \
  -form=/root/wmts-full-form.xml \
  -form=/root/wmts-full-all-tests-form.xml
```

第一个 `<values>` 表单填写 `capabilities-url`，将 `fail-early`、`include-CommonOperations-tests`、`include-GetCapabilities-tests`、`include-GetTile-tests`、`include-GetFeatureInfo-tests`、`include-kvp-get-encoding`、`include-restful-encoding` 设置为 `on`。第二个表单把官方 `wmtsFunctions.xml` 声明的 `whichServer...tests/Tests` 选择字段设为 `all`；本次完整表单已保存于批次产物目录。

运行 `viewlog.sh -session=<实际会话> -html` 生成 EARL 和 HTML，保留完整原始会话。导出器不能完整表示 CONTINUE 时，原始日志仍是判定依据。失败前提导致的后续组未执行和没有正式套件的标准范围，需要在发布结论中明确列出。

2026-10-15 继续修复的 Maps/Coverages 原版开发定义结果为15/0/9和5/0/0；图像还经 GDAL 独立解码，CIS 元数据及完整覆盖使用官方 JSON Schema 校验。9项跳过和未启用的人工视觉断言不算通过，也不等同正式认证。最新证据见 `.artifacts/cite-20261015-maps/`。

## Features Part1/Part2与本地整合验证

`test/citefeatures`是测试专用Reader宿主，根API提供2D集合，`/three-dimensional/`提供真实高度数据；两套必须完整报告，不能删数据换绿。Part3/CQL2仍是解析/发现能力，fixture不声明其服务符合性。官方Features ETS1.9范围是Part1及可选Part2，不是Part3/CQL2认证套件。

`test/citefeatures/run_ets.py`仅接受固定SHA256的官方原版1.9 AIO，不接受本地修改包。官方AIO、官方普通classpath/TEAM Engine、原样PR267候选和本地r3的原始结果分别保留；原AIO的资源错误与官方Java的六值/数值断言缺口不能混为一谈，详情见[多坐标系契约](features-crs.md#真实http与官方ets)。

本地修正属于**另一个ETS仓库**，从官方1.9固定base整合至`1.9-local-crs3d-integrated-r4`；其`local-verification/`包含最终双仓库编排，不进入本Go库依赖。外部`integration-lock.json`固定两个最终HEAD与实际JAR哈希，避免脚本引用旧archive或陈旧绝对路径。两个仓库冻结后生成该锁，不将自身commit哈希写入其自身树。

在本产品仓库根目录，使用明确的本地路径运行：

```sh
GO_BINARY=/path/to/go/bin/go python3 "$LOCAL_ETS_ROOT/local-verification/run_all.py" \
  --product-root "$PWD" --lock "$INTEGRATION_LOCK" \
  --local-jar "$LOCAL_ETS_JAR" --official-jar "$OFFICIAL_ETS_JAR" \
  --output "$NEW_RESULT_DIRECTORY"
```

入口先拒绝HEAD/dirty/工件哈希不匹配，构建当前冻结产品而非旧二进制，同一shell网络环境内原子取得loopback动态端口并启动fixture，通过本次nonce/PID/源码revision的就绪文件与HTTP身份验证，逐组检查child活性，运行两API全部集合的官方原版与明确非官方本地修正版，再运行20个已知坐标/高度/链接控制与15个固定bbox控制。`--validate-only`只检查来源，不运行测试。工具链准备、Maven完整verify、独立脚本单测和结果布局见ETS仓库的`local-verification/README.md`。

任何原官方FAIL都使汇总入口非零退出，即使本地修正版没有FAIL；这不是执行器setup失败的自动豁免。保留各变体TestNG/configuration结果，SKIP仍为未覆盖，不能用空结果证明正相交。源码HEAD、fixture构建VCS、JAR与锁哈希随日志保存。没有自动下载、远程部署、上游提交或认证申请。


叶runner按实际TestNG方法/配置结果核对必需计数，零测试、缺报告、错误XML或计数矛盾均非零退出；顶层再次独立读取原XML，并核对控制报告完整性/显式成功，不能仅信进程exit0。新叶runner单测加入完整check门禁（原34项另加该项）。旧原版/本地反例与报告均保留。
