# Features Part 2 多坐标系请求

依据 [OGC API Features Part 2 1.0.1](https://docs.ogc.org/is/18-058r1/18-058r1.html)。本库解释URL、类型化坐标系、验证能力并组织响应；**不执行重投影、垂直基准转换、几何筛选或SQL**。实际坐标操作由业务Reader完成。

## 按集合显式启用

`features.Options.CRS == nil` 保留既有Part1参数和结果契约：不会自动接收 `crs` / `bbox-crs`，也不从Maps/WMS/Coverages推断支持。

```go
package application

import (
	"github.com/mapseekai/ogc-api/common"
	"github.com/mapseekai/ogc-api/features"
)

func CoordinateOptions() features.Options {
	wgs84, _ := common.CRSByURI(common.CRS84)
	latitudeFirst, _ := common.CRSByURI("http://www.opengis.net/def/crs/EPSG/0/4326")
	mercator, _ := common.CRSByURI("http://www.opengis.net/def/crs/EPSG/0/3857")
	return features.Options{CRS: &features.CRSOptions{
		Supported:  []common.CRS{mercator, wgs84, latitudeFirst},
		DefaultCRS: common.CRS84, // 不是列表首项，也不是storageCrs
	}}
}
```

`common.CRSByURI` 仅提供CRS84、CRS84h、EPSG4326、EPSG4979、EPSG3857的精确标准URI轴信息，不启用集合能力。未知标识返回false，宿主可从权威定义显式提供 `common.CRS{URI, AxisOrder, Dimensions, Geographic}`。不解析SRID数字，不自动将短名、URN、HTTP/HTTPS别名互换。URI必须是安全的绝对ASCII URI，不含凭据、fragment、非法转义或控制字符。

支持二维/三维、水平XY或YX轴序，第三轴保留自身高程/深度语义。`Geographic` 表示度制经纬度；其他坐标只检查有限数值、维度和有序范围，**不按经纬度范围验证投影值，不做Mercator clamp**。自定义投影适用区域和操作有效性由后端判断。已知URI不能配置错误轴序/维度。

## 三种CRS互不替代

| 参数/字段 | 作用 |
| --- | --- |
| bbox-crs → Query.BBoxCRS | 解释输入BBox的原生轴序/维度；不重排原值，不改变输出 |
| crs → Query.OutputCRS | 选择所有输出geometry-valued properties的坐标系，适用于单要素与列表 |
| filter-crs → Filter.CRSDefinition、几何AST.CRSDefinition | 解释CQL2几何字面值，不改变bbox或输出 |

默认输出只能是CRS84或CRS84h；省略DefaultCRS时为CRS84，不能将任意投影设成隐式GeoJSON默认。存储CRS不改变默认输出。

省略bbox-crs：4数按CRS84、6数按CRS84h，与输出无关。保留Part1隐式6数解析不等于承诺3D输出/垂直转换；Reader必须正确处理高度约束，不能将无Z假设为Z=0。显式bbox-crs必须来自集合Supported列表，且维度匹配；无bbox的单独bbox-crs被拒绝，避免静默忽略。投影轴必须有序；只有地理经度轴允许跨反经线。

EPSG4326使用纬度、经度：世界框是 `-90,-180,90,180`；`-180,-90,180,90` 是CRS84顺序，不能直接用于EPSG4326。EPSG4979是纬度、经度、椭球高；CRS84h是经度、纬度、椭球高。

Part3额外filter CRS使用独立 `FilterOptions.AdditionalCRS` 白名单，且必须同样存在于集合Part2 Supported。**输出/bbox支持不会自动承诺CQL2求值支持。** 省略filter-crs仍逐字面值用CRS84/h，不继承bbox-crs或crs。纯 `cql2.Options` 的非默认CRS须同时提供一致的CRS与CRSDefinition；精确坐标数字不经float64、换轴或重投影。

未知、空、非法、重复或未支持参数返回400；URL解码一次。单要素仅接受f/已启用的crs，不接受bbox、bbox-crs、filter或分页条件。

## 后端结果、GeoJSON与所有权

- Reader完成转换后，列表须在 `Page.CRS` 回报实际输出定义（空页也必须）；Iterator可省略每个Feature.CRS，编码器采用Page声明，显式冲突则终止流。
- 单要素须在 `Feature.CRS` 回报实际输出定义。API交付前检查与请求一致；缺失/不一致是后端契约错误，不能把200正文误贴为请求CRS。错误页保留关闭/取消责任。
- Feature.CRS的 `json:"-"` 不生成GeoJSON crs成员。Part2模式校验顶层geometry结构、有限数值、轴序范围、维度及bbox，不校验拓扑/转换数值。其他geometry-valued properties的转换由Reader保证，不能只转换顶层geometry。
- `Content-Crs: <URI>` 在启用Part2的成功items/item/default/HEAD中始终输出。流后续错误由Plan.Finish记录，不改写已提交状态。
- [Part2 §6.3.3.2](https://docs.ogc.org/is/18-058r1/18-058r1.html#_output_format_considerations) 允许GeoJSON prior arrangement：显式crs建立非默认约定，不一概禁止投影GeoJSON，也不添加自造CRS字段。接收方须按Content-Crs原生轴序处理；跨域宿主应暴露此头。
- self/next链接保留crs、bbox-crs、filter-crs及筛选/格式参数，元数据collection链接不错误继承要素参数。请求/AST不使用公共对象池，可长期保留；快照复制Supported/AdditionalCRS数组与epoch指针。

## 发现与符合性边界

快照将Supported写为每Collection完整crs列表；宿主另填Metadata.CRS时必须与能力一致。列表嵌套Collection与单Collection相同。使用完整本地列表，不广告可选全局 `#/crs` 继承。

`common.Collection.StorageCRS` 由宿主在所有要素使用同一存储CRS时如实提供，必须属于支持列表；混合存储/未知可省略。不从输出顺序或Map默认值猜测。动态CRS可填 `StorageCRSCoordinateEpoch *float64`：有限的公历小数年，要求有StorageCRS，不是请求转换epoch指令。

`features.CRSConformance` 是Part2 server class URI。配置解析器不自动声明符合性；部署验证Reader后可加入Catalog.ConformsTo，且所有Features集合都须显式配置CRS策略。**不是OGC认证或任意后端符合性保证。**

## 需求与验证矩阵

标准路径以 `/req/crs/` 为前缀。

| 要求 | 实现/契约 | 验证位置 |
| --- | --- | --- |
| 1 crs-uri；推荐1 URI模型 | 绝对URI、安全编码；显式轴信息，不抓取注册表 | common/crs_test.go |
| 2 fc-md-crs-list A/B | 集合列表完整且含标准输出默认 | features/crs_test.go；api/catalog_test.go |
| 3 fc-md-storageCrs；推荐2 epoch | 存储由宿主如实声明；支持epoch，不推断 | api/catalog_test.go；测试后端 |
| 4 fc-md-storageCrs-valid-value | storage属于集合支持列表 | api/catalog_test.go |
| 5 fc-md-crs-list-global | 条件不适用：不发布全局列表/引用 | api/catalog_test.go；本文 |
| 6 fc-bbox-crs-definition | 仅列表bbox-crs及OpenAPI | features/crs_test.go；api/openapi_test.go |
| 7 fc-bbox-crs-valid-value | per-collection白名单；空/未知/重复400，不调用Reader | api/features_test.go |
| 8 fc-bbox-crs-valid-default-value | 4/6数按CRS84/h，不继承output/storage/filter | features/crs_test.go |
| 9 fc-bbox-crs-action | 原生bbox+typed CRS交Reader；测试后端真正转换点再筛选 | test/citefeatures/main_test.go |
| 10 fc-crs-definition | items/item参数与OpenAPI，item严格参数集合 | features/crs_test.go；api/openapi_test.go |
| 11 fc-crs-valid-value | 集合白名单，输入框与输出独立 | api/features_test.go |
| 12 fc-crs-default-value | 默认CRS84/h，非首项/storage | features/crs_test.go；真实HTTP |
| 13 fc-crs-action | 后端转换所有几何值；返回定义须匹配请求 | api/features_test.go；真实HTTP已知数值/第二几何属性 |
| 14 geojson | 显式非默认请求prior arrangement，无自造crs成员 | features/geometry_test.go；api/features_test.go |
| 15 ogc-crs-header | items/item/default/HEAD Content-Crs | api/features_test.go；真实HTTP |
| 16 ogc-crs-header-value | 尖括号URI，不输出非法原始输入 | api/features_test.go；common/crs_test.go |
| Part3交互 | 独立filter能力，精确AST原生坐标，保持AND契约 | cql2/geometry_test.go；features/crs_test.go；api/features_test.go |
| 生命周期/回归 | retained request、并行快照、错误关闭；各协议原请求不变 | api/features_test.go；features/stream_test.go；全项目gates |

## 真实HTTP与官方ETS

`go run ./test/citefeatures` 默认只监听127.0.0.1:18090。根API提供全球2D点与真实3857存储区域数据；`/three-dimensional/` 提供真实4979存储和不同高度点。两者如实声明Part2能力，不声明Part3/CQL2服务符合性。投影公式仅在测试命令，不进入库依赖。自动整合入口使用literal loopback的动态端口，发布本次nonce/PID/源码revision就绪身份并检查child活性；手工默认地址仍为18090，不依赖“先检查空闲再绑定”。

独立测试包含[官方PROJ示例](https://proj.org/en/stable/operations/projections/webmerc.html)数值、4326轴序、3857米坐标/逆转换、第二几何属性、bbox命中排除、反经线、3D高度、不伪造缺失Z和分页保留；不是官方ETS替代品。

```sh
go test -race ./common ./features ./cql2 ./api ./test/citefeatures
./scripts/check.sh --consumers
go test -run='^$' -fuzz='^FuzzCRSQuery$' -fuzztime=5s -parallel=2 ./features
go test -run='^$' -bench=BenchmarkCRS -benchmem ./features
```

官方ETS1.9 tag commit为 `e7c8a96deff936f7ae9c4cbbafb900ce65371d2c`，从Maven Central下载AIO JAR。在同一shell启动fixture并等候就绪后，分别运行两套全部公开集合（部分云执行器不跨调用共享localhost）：

```sh
python3 test/citefeatures/run_ets.py --jar PATH_TO_OFFICIAL_1.9_AIO \
  --iut http://127.0.0.1:18090/ --output .artifacts/cite-features-2d
python3 test/citefeatures/run_ets.py --jar PATH_TO_OFFICIAL_1.9_AIO \
  --iut http://127.0.0.1:18090/three-dimensional/ --output .artifacts/cite-features-3d
```

runner核对JAR的SHA256，保留XML/HTML/EARL、命令、全部失败/跳过，断言失败非零退出。不要因shell set-e在首个非零时漏跑另一API，应独立记录后汇总。

原版ETS1.9 AIO有资源打包冲突，造成4326世界bbox不换轴和4979条目缺失。官方普通classpath/TEAM Engine的项目JAR优先启动能解除这些资源错误，但仍有六数extent硬限制、4979轴序/Z与坐标相等函数未assert等执行器缺口；不能把资源异常消失后的PASS当作完整三维数值证明。

原版不利结果必须保留，不得为全绿削弱轴序/维度、删除数据、伪造extent，或将修改过的ETS冒称官方原版。另一个独立ETS仓库提供明确标注非官方的本地修正版与测试编排；执行器不进入本库依赖。完整双仓库锁定、最终源码fixture构建和两API/两套件复现见[CITE接入指南](cite.md#features-part1part2与本地整合验证)。本地修正结果与官方结果分列，任何失败/跳过都不算通过；正式认证另走OGC流程。
