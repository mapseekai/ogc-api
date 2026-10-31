# Features Part 3 过滤请求 profile

依据 [OGC API Features Part 3 1.0](https://docs.ogc.org/is/19-079r2/19-079r2.html) 和 [CQL2 1.0](https://docs.ogc.org/is/21-065r2/21-065r2.html)。这是**协议解析与发现能力**，不是数据库查询引擎或完整服务器符合性认证。

## 数据流与所有权

```text
URL 原始 query
  → common.ParseQuery（一次 URL 解码、重复参数/UTF-8/大小检查）
  → features.ParseQuery（参数策略、collection queryables 类型检查）
  → features.Request.Query.Filter.Expression（同一个 cql2.Expression 模型）
  → 外部 Reader / Iterator（执行实际筛选）
```

CQL2 Text 与 CQL2 JSON 使用同一 AST，属性引用、属性声明类型、字面值类型、运算符和操作数是不同字段。`IN` 的集合归一为 `Args[1:]`，`IS NOT NULL`、`NOT LIKE/IN/BETWEEN` 归一为 `Not` 包装；括号只影响树形结构。语言与显式 CRS 保留在 `features.Filter` 元数据中，不要求后端再次解析表达式。`Property` 名称不是 SQL 标识符，不得直接拼入 SQL。

返回的 request、AST、字面值和坐标数组由请求独占，既不暴露也不使用公共对象池。Reader 或其 Iterator 可以保留它们到 `Items`、`Handle`、`Plan.Finish` 之后。共享配置须在调用期间保持只读；`Options.Defaults` 复制可变 map/slice，`api.NewSnapshot` 冻结配置和发现文档。取消和迭代器关闭继续遵循现有 API 生命周期契约。

`Query.Filter`、`BBox`、`Datetime`、`Properties` **全部按 AND 联合**。它们不是候选条件或优先级替代关系。`BBox` 保留 Part 1 对空几何、三维范围、反经线等语义，不能简单改写成 `S_INTERSECTS`。后端按 CQL2 三值逻辑执行，只有结果为 TRUE 的要素进入集合；FALSE 和 NULL 均排除。

## 按集合启用

默认 `features.Options.Filter == nil`，不会自动接受 CQL2 或公布 queryables。

```go
package application

import (
	"github.com/mapseekai/ogc-api/cql2"
	"github.com/mapseekai/ogc-api/features"
)

func FeatureOptions() features.Options {
	return features.Options{
		FilterProperties: []string{"name"}, // 现有字符串属性参数保持兼容
		Filter: &features.FilterOptions{
			Queryables: map[string]features.Queryable{
				"name":       {Type: cql2.String, Title: "Road name"},
				"population": {Type: cql2.Integer},
				"observed":   {Type: cql2.Timestamp},
				"geom":       {Type: cql2.GeometryType, Format: "geometry-linestring", Role: "primary-geometry"},
			},
			// 只声明这个 Reader 真正能够执行的扩展运算符。
			Operators: []cql2.Operator{cql2.SIntersects, cql2.TIntersects},
			Limits:    cql2.Limits{MaxBytes: 32768, MaxDepth: 32, MaxNodes: 2048},
		},
	}
}
```

Basic CQL2 逻辑、标准比较和空值测试默认支持。`IS NULL` / `IS NOT NULL` 在两个编码中都接受受支持的属性、字面值和布尔表达式 operand（不采用仅允许属性的可选限制）；这不会绕过内部谓词的类型、operator 或 property-comparison 策略。空值测试作为完整 predicate/value 的后缀处理，不要求额外外括号，允许连续后缀；它在 AND/OR 前结合，NOT 仍按 boolean factor 作用于后续完整谓词。每层 IS NULL 及 IS NOT NULL 的 Not 包装分别计入节点与深度限额。`Operators` 是扩展白名单，未声明的扩展直接拒绝，不交给 Reader 猜测。`PropertyComparisons` 允许标量 property/property、literal/property 和 literal/literal 比较；默认只接受 property/literal 比较。空间、时间运算符的两个操作数可以是相应类型的属性或字面值。无需把数据库字段暴露为 queryable，可以提供有意设计的虚拟字段。

启用后，原 `FilterProperties` 必须也声明为字符串 queryable；其 URL 参数仍按原样放入 `Query.Properties`。新增 queryable 不会自动成为同名 query 参数，数值字段也不会偷换到旧的 `map[string]string`。本 profile 不宣称 `queryables-query-parameters` 类。

## 参数与错误

- `filter`：非空、有效且在预算内的 CQL2 谓词。
- `filter-lang`：省略使用 `cql2-text`；默认策略允许 `cql2-text` 和 `cql2-json`。可按集合限制为 Text；JSON-only 策略不支持。拼写大小写敏感，不接受 `cql`、SQL 或其他语言。
- `filter-crs`：省略时，各二维/三维几何分别获得 CRS84/CRS84h。默认显式接受这两个标准 URI；启用 Part 2 后，可通过 FilterOptions.AdditionalCRS 独立声明同集合支持的其他过滤坐标系。维度须对应，每个几何节点保留 CRSDefinition 的原生轴序，数值不重排。不会从输出、bbox、Maps、WMS 或 Coverages 自动推断过滤支持，也不执行重投影；详见[多坐标系契约](features-crs.md)。
- 该 profile 将单独出现而无 `filter` 的 `filter-lang` / `filter-crs` 视为无效组合；这是明确的严格输入策略。
- 未知参数、未知字段、错误类型、未启用的操作符/语言/CRS、空值和重复参数返回 400。参数在 URL 层只解码一次，CQL2 解析器不再次 URL 解码。`filter-crs` 不再可被声明成普通旧属性过滤参数。
- 查询/路径的整体字节超限仍由既有 API 逻辑返回 414；过滤器自身的预算、语法和能力错误返回安全的 `InvalidParameterValue`，不回显表达式。内部原因保留在 `common.Error.Cause`。

## Queryables、OpenAPI 与符合性

每个启用 collection 发布 `/collections/{id}/queryables`，支持 GET/HEAD、`application/schema+json` 和 JSON、ETag/304、既有缓存策略；Collection、FeatureCollection 和 HTTP `Link` 提供标准 queryables 关系。JSON Schema 使用 2020-12，`$id` 是无查询串资源 URI，根为 object，`additionalProperties:false`。空间字段按标准省略 `type` / `$ref`，使用 `geometry-*` format；时间字段为带 `date` / `date-time` format 的 string。

非标准 `x-ogc-filter-*` / `x-ogc-property-comparisons` 注解说明语言、实际支持的运算符和有限预算，不是符合性声明。OpenAPI 只在启用相关 collection 时发布 queryables 与过滤参数；参数语言是已发布集合的并集，逐集合能力以它们各自的 queryables 为准。

**不会仅因存在解析器就自动将 Features Part 3 或 CQL2 类添加到 `/conformance`。** 宿主必须验证 Reader 的筛选语义、NULL/时间/空间行为及部署的标准依赖，之后通过现有 `Catalog.ConformsTo` 明确声明；该字段是部署断言，不由库认证。不能声称未实现的函数、算术、数组、额外 CRS 或 queryable 参数映射类。示例 Reader 默认不启用此能力，以免忽略过滤后返回未筛选内容。

## 需求与符合性矩阵

下列状态区分协议支持与依赖外部执行的服务器行为；不等同于通过 OGC CITE。

| 标准要求 / 类 | 当前处理 | 证据与边界 |
| --- | --- | --- |
| Part 3 `/req/queryables/queryables-link` | 已实现，按集合启用 | Collection/FeatureCollection/HTTP Link；`api/catalog_test.go`、`api/features_test.go` |
| `/req/queryables/get-queryables-op` | 已实现 | queryables 路由、Schema JSON 协商、GET/HEAD、错误、缓存 |
| `/req/queryables/get-queryables-response` | 已实现闭合字段 profile | 2020-12、正确 `$id`、scalar/temporal/spatial schemas；`features/filter_test.go` |
| `/req/queryables-query-parameters/parameters` | 不宣称 | 仅保留显式字符串 `FilterProperties`，不将全部 queryables 变成参数 |
| `/req/filter/filter-param` | 已实现协议层 | URL decode → typed AST；默认不启用 |
| `/req/filter/filter-lang-param` | 已实现双编码 profile | 默认 Text、集合语言白名单、错误 400 |
| `/req/filter/filter-crs-wgs84` | 已实现解析/归一 | 二维 CRS84、三维 CRS84h；后端负责相应空间语义 |
| `/req/filter/filter-crs-param` | 标准地理 CRS；可显式配置 Part 2 额外 CRS | `FilterOptions.AdditionalCRS` 独立声明过滤能力；AST 保留原生轴序/维度，不做重投影 |
| `/req/filter/mixing-expressions` | 统一请求 AND 契约 | 原 BBox/Datetime/Properties 保持独立；Reader 负责执行 |
| `/req/filter/get-functions-operation`、`get-functions-response-json` | 不适用 | 不支持 Functions 类，不发布 `/functions` |
| Part 3 §8.7 过滤语言 | 有限 CQL2 parser profile | 下表列出扩展和明确限制；非完整任意表达式语言 |
| `/req/filter/response` | 外部 Reader 责任 | 三值逻辑与结果筛选不在协议库中执行 |
| `/req/features-filter/get-queryables-op`、`queryables-link` | 已实现，启用 collection 内 | 不是自动启用所有已有 Feature 服务 |
| `/req/features-filter/filter-param`、`filter-lang-param` | 已实现协议绑定 | 仅集合 items，单要素 endpoint 拒绝过滤参数 |
| `/req/features-filter/filter-crs-param` | 已实现有限 Part 2/3 协议绑定 | 标准默认 CRS 及显式 `AdditionalCRS`；过滤能力独立于 bbox/输出 CRS，由 Reader 求值与转换 |
| `/req/features-filter/response` | 解析/400 已实现；执行外置 | Reader 必须联合全部谓词，只有 TRUE 纳入结果 |
| CQL2 Basic / Text / JSON | 解析受限 profile | AND/OR/NOT、`= <> < <= > >=`、IS [NOT] NULL；同一 AST；不自动宣称服务器类 |
| CQL2 Advanced comparison | 可逐操作符启用 | LIKE、数值 BETWEEN、非空 IN；支持 NOT 包装；不支持 CASEI/ACCENTI |
| CQL2 Basic spatial / spatial-plus / spatial | 可逐操作符启用 | 8 个标准空间运算符；Point/LineString/Polygon/Multi*/GeometryCollection/BBox；校验结构、不计算拓扑 |
| CQL2 Temporal | 可逐操作符启用 | 15 个标准时间运算符，按 §7.8.2 Table 5/6 区分签名；其中 10 个只允许两侧均为 interval，不把 instant 当作 interval |
| CQL2 Property-property | 可启用标量比较 | 属性类型来自声明，不能将任意表达式当 SQL |
| CQL2 Case/accent、Array、Functions、Arithmetic | 未实现，拒绝 | 不静默透传不认识的运算符或函数 |

## 明确的语法 / 资源边界

- 硬上限：过滤输入 65,536 bytes、嵌套深度 64、节点/JSON 值/坐标容器 4,096。可以降低，不能提高或设置负值以取消上限；有限解析栈也计入深度，因此不是保证接受恰好 64 层业务括号。
- 数字令牌最多 1,024 字符，十进制指数绝对值不超过 1,024；归一为精确十进制/科学计数 `json.Number`，不会经 `float64`；大 scale 保持紧凑形式以避免指数展开放大内存。数值 queryable 可比较整数或小数边界，类型检查不进行字符串自动转换。
- 日期与 timestamp 先做完整 ASCII 词法检查，再做日历/范围检查；timestamp 必须 `YYYY-MM-DDTHH:MM:SS[.fraction]Z`、小时固定两位、小数秒最多 9 位，拒绝逗号小数、短小时及超精度值，不依赖 Go `time.Parse` 的宽松接受或截断行为。interval 仅支持字面值端点、至少一个非开放端点、相同粒度、起点不晚于终点；不支持属性/函数端点。
- 几何支持二维/三维已声明坐标系；默认仅地理CRS84/h，其他CRS须显式提供轴序/维度。地理坐标按原生轴校验经纬范围，投影值不套度范围；校验线段最小点数、polygon ring 闭合、bbox轴/高程顺序，仅地理经度轴允许跨反经线。拒绝 EMPTY、非标准 GeoJSON `crs`、未知成员和混合维度；不验证自相交、环方向或真实相交关系。GeometryCollection 要求至少两个同维度的简单几何成员，不接受嵌套 collection/BBox；MultiPoint 使用带括号的 position。
- JSON 使用精确数字，拒绝重复/未知结构成员、尾随值、无效 UTF-8、孤立 UTF-16 surrogate。Text 支持 Unicode identifier、双引号属性、双单引号、反斜杠单引号、反斜杠自身以及 `\a\b\t\n\v\f\r` 控制转义；只解码一次，未知 escape 返回 400，不会把字面值变成属性引用。
- Table 6 的 T_CONTAINS、T_DURING、T_FINISHEDBY、T_FINISHES、T_MEETS、T_METBY、T_OVERLAPPEDBY、T_OVERLAPS、T_STARTEDBY、T_STARTS 必须接收两个 interval；其他五个函数允许 instant/interval 混合。独立 `cql2.Options.Properties` 可声明 `IntervalType`；Features queryables 目前只支持时间 instant 字段，配置 interval-valued 字段会明确报错，不会伪装成 timestamp。两个 interval 字面值可正常解析，实际运算仍由 Reader 负责。
- CQL2 `NULL` 通过 IS NULL 表达，不接受 `= NULL`、JSON null 或裸 NULL 作为谓词。未实现的语法立即拒绝，不生成部分树。

## 验证命令

```sh
make tools
make fmt
./scripts/check.sh --consumers
go test -race -count=1 ./cql2 ./features ./api
go test -run='^$' -fuzz='^FuzzParse$' -fuzztime=10s -parallel=2 ./cql2
go test -run='^$' -fuzz='^FuzzParseQuery$' -fuzztime=5s -parallel=2 ./features
go test -run='^$' -bench=BenchmarkParse -benchmem ./cql2
```

测试按现有规范与主要源文件同名，集成行为位于 `api/features_test.go` / `api/catalog_test.go` / `api/openapi_test.go`；包含延迟 Iterator 保留 AST、并行请求、取消、元数据发布后变更、旧协议回归与模糊输入。完整日志由仓库检查脚本写入 `.artifacts/check/`。
