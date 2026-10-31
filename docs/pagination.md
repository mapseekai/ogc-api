# Features 与集合目录分页扩展

这是默认关闭的协议扩展，不是新的 OGC 符合性类或数据库分页引擎。核心只解析、校验、路由并生成链接；Reader / Provider 执行实际读取、授权与排序。

## Features offset

在每个实际支持 offset 的集合上设置 `features.Options.OffsetPagination = true`。原有 `features.Reader.Items/Item` 方法不变：

- `Query.Offset == nil`：沿用现有默认/cursor 模式。`Page.NextCursor` 是不透明后端续页令牌；不能把 offset 悄悄转换为 keyset 或 SQL。
- 非 nil（包括零）：Reader **先授权，再将 BBox AND Datetime AND Properties AND Filter 全部求值**，最后跳过 `*Query.Offset` 个匹配要素，最多返回 `Query.Limit` 个。稳定排序由 Reader 提供；协议不承诺跨请求数据库快照。
- URL `offset` 必须是非空十进制 uint64（0..18446744073709551615）；负值、符号、浮点、溢出及重复参数返回400。`offset` 与任何 `cursor` 出现互斥，包括空 cursor；未开启选项时 offset 返回400，不会被忽略。单要素路由不接受它。
- 原有 limit 行为不变：正整数超上限按 MaxLimit 截断，包含超 uint64 的正十进制文本；默认值也保持不变。
- Reader 以 `Page.NextOffset` 明确表示下一页起始位置，必须大于本次 offset；nil 表示终点，**包括刚好满页的末页**。不知道总数可以省略 `NumberMatched`；不得用“返回数量等于 limit”猜下一页。
- offset 页不能同时返回 NextCursor；默认/cursor 页不能返回 NextOffset。已知 NumberMatched 时下一页位置必须小于总数。错误元数据返回安全500并关闭 Iterator。
- self/next/prev 保留全部已接受的 bbox、datetime、普通属性、filter/filter-lang/filter-crs、bbox-crs、crs、f、limit 参数。next 使用显式位置；prev 为 `max(0, offset-limit)`，只保证向前翻页的协议位置，不承诺变动数据集的反向一致性。所有参数只 URL 编码一次；未知参数仍拒绝。

Query/AST 可在 Reader 返回后继续持有；没有池化回收。Page 返回后移交 Iterator 和元数据指针所有权，Reader 不再修改它们。宿主仍须在成功、HEAD、304、取消、发送失败等路径调用 Plan.Finish。

## `/collections` 静态分页

设置 `api.Catalog.CollectionPaging = &api.CollectionPaging{DefaultLimit: 10, MaxLimit: 1000}`。两个零值分别选择10/1000，DefaultLimit 大于 MaxLimit 在发布时拒绝。仅接受 f、limit、offset；目录 limit 必须在1..MaxLimit，超限**拒绝**，这与 Features 既有 clamp 有意不同。

静态目录按已发布的排序索引切页，不执行后端读取。返回 collections、links、numberReturned 和可选 numberMatched；空页编码为 `[]`。静态 next 只在确有后续资源时生成，整满尾页也没有 next；超尾页为空且不生成 next。self 规范化显式 limit/offset；next/prev 保留 f/limit。未设置 CollectionPaging 时，旧的完整预编码目录、参数策略和表示不变。

## 惰性富元数据：为什么 List 之外还需要 Get 和 Snapshot

大目录的“每页全量装载富描述，再重新建 Snapshot”不是分页。可通过 `CollectionPaging.Provider` 注入 `CollectionMetadataProvider`，它只有两个读取方法：

- `Collections(ctx, scope, CollectionQuery)` 一次读取所请求的有界页。
- `Collection(ctx, scope, id)` 只读取一个描述，供 `/collections/{id}` 使用；不是先 List 全目录再搜索。

**当前发现页不能充当授权路由表。** 宿主在独立、可缓存的授权 Snapshot 中注册全部允许的 ID 与能力绑定（富元数据可以只有 ID），并将对应的 metadata Provider 一并发布。每个请求只选择一次 Snapshot，之后 List、Get、items 和 queryables 都使用同一授权/能力视图。页外已注册授权 ID 仍可访问，其他 ID 在调用 Get 之前404；Provider 返回未注册 ID、重复 ID 或错 ID 会被拒绝。新 ID、能力或权限视图须发布新 Snapshot；Provider 不能扩展它们。

协议在每次 List/Get 后按文档预算验证并复制富元数据，基于 Snapshot 生成能力/路由链接；Provider 可补充非路由公共链接，不能伪造新 items/map/queryables 等能力。Features CRS 元数据仍须符合该 Snapshot 的显式 Reader 选项。Queryables 的字段、类型与能力声明仍由发布的 FilterOptions 决定，而不是从富描述推断。

这是按页/ID 的**富元数据读取**契约，不是动态任意 ID 注册、租户授权引擎或通用目录服务；轻量 ID/能力索引仍须注册和缓存。Provider 必须绑定同一可见域/发布代，按可信 Context/Scope 鉴权，不信任 URL 中的身份。不能在请求间改变它的授权集合；更新用 AtomicCatalog 发布新视图，旧 Provider 的退役由宿主等待在途请求结束。

## 权限过滤空页、数量与缓存

Provider 的 NextOffset 是公开位置，不是长期凭据或隐藏行总数；宿主不能将敏感扫描游标塞进 URL。对权限过滤后的空页，可以显式返回前进的 NextOffset。核心每个请求只读一页，不为填满它执行无界扫描；NextOffset 必须严格大于当前值，PreviousOffset（可选）必须小于当前值。nil next 表示扫描结束。目录位置不一定等于可见资源序号，因此不从 numberMatched 推导扫描位置；数据变动下稳定排序与续页有效性是 Provider 契约。

NumberMatched 只允许报告可见资源数量，未知即省略；不能小于本页数量或大于 Snapshot 已授权 ID 数。页面不得超过请求 limit。计数不是翻页所必需。

百分号编码的资源路径别名同样走分页/惰性读取与授权，不能回退到静态描述绕过 Provider。Provider 成功返回但元数据校验失败（如非法 extent）是安全500，并保留私有诊断 Cause，不应把它误报成客户端 bbox 参数错误。

新分页列表及惰性描述均使用 `Cache-Control: private, no-cache`，不沿用可能公开的静态元数据缓存策略。ETag 包含可信 Scope、路径、接受的参数与表示字节。**先选授权 Snapshot、调用本页/ID Provider，再判断 If-None-Match**；撤权不能靠旧 ETag 得到304，不同 scope/页/参数不能误用同一个标签。宿主必须继续保证 scope 唯一和可信、正确鉴权，并避免外部缓存覆盖 private 策略。

## 兼容性与验证

Reader/HTTP handler 方法没有变化；新增 Options/Query/Page/Catalog 字段均为可选，未启用行为保持兼容。外部使用无字段名的结构体字面量须改为具名字段（Go 公共结构新增字段的固有限制）。`offset` 新增为普通 `FilterProperties` 保留名，避免启用选项后含义冲突；依赖该属性配置的应用需迁移；CQL2 queryable 同名属性仍可通过显式表达式使用。

OpenAPI 仅在相应功能启用时提供参数。共用 items 路径若有任一集合支持 offset，会提示只适用于已启用集合；每个集合仍单独校验。该扩展不自动加入 `/conformance`，不能据此声称 CITE 认证。

`features/query_test.go` 验证精确整数、显式开启及互斥；`api/features_test.go` 验证显式续页/清理；`test/citefeatures/main_test.go` 的真实 HTTP Reader 在全部谓词后应用 offset，覆盖首中满尾空页、最大整数及过滤/CRS链接保留。`api/collections_test.go` 验证有界静态/惰性页、空权限页、跨 scope/页外路由、304撤权和在途原子替换，`api/catalog_test.go`/`openapi_test.go` 验证配置所有权与发布契约。
