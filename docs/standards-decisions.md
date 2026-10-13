# 标准与实现决策

## 规范优先级

规范依据为 OGC WMS 1.0.0（00-028）、1.1.0（01-047r2）、1.1.1（01-068r3）、1.3.0（06-042）和 WMTS 1.0.0（07-057r7）。参考服务器的容错和业务限制不是规范本身。

来源导航：

- <https://www.ogc.org/standards/wms/>
- <https://www.ogc.org/standards/wmts/>
- <https://docs.ogc.org/is/00-028/00-028.pdf>
- <https://docs.ogc.org/is/06-042/06-042.pdf>
- <https://docs.ogc.org/is/07-057r7/07-057r7.pdf>

## 请求语义

参数名仅进行 ASCII 大小写折叠，标识值保持原样。缺失、空值、有值分别处理。已识别单值参数重复时默认拒绝，这是防参数污染策略；未知扩展字段保留在 Parameters，但不会被一律解释为维度。

图层和样式列表在原始字面逗号处切分，再解码各项，所以 `%2C` 可以属于单个标识。BBOX 是固定四个数值，不存在合法的“含逗号数字标识”，因此先解码再解析数字分隔符，同时接受浏览器 URLSearchParams 生成的转义逗号。

WMS 的参数值和操作名不会被整体转小写。WMS 1.0.0 的旧操作名与现代 GetMap 等写法分别实现。WMS 1.3.0 的非法 FEATURE_COUNT 按规范默认回到 1，而不是将所有非法数值都套用同一错误策略。

## 轴序与矩阵

WMS 1.3.0 的 EPSG:4326 使用纬度、经度的线序，CRS:84 使用经度、纬度；不能仅根据版本交换所有坐标。原始 WireBBox 与标记归一化状态的 Bounds 同时保留。

本库只做二维轴映射，不做投影、单位变化、裁剪或坐标回绕。请求范围与图层数据范围不相交不属于参数错误。

WMTS TileMatrix 是目录中的字符串键。行列范围依照 MatrixWidth、MatrixHeight 与图层 Limits；I 对应 TileWidth，J 对应 TileHeight。

## 版本与元数据

WMS Capabilities 的版本协商按数值比较，普通数据请求只接受显式支持版本。WMTS Capabilities 使用 AcceptVersions，VERSION 不被错误套用成 WMS 的协商入口。

Capabilities 不使用传入 Host/Forwarded 拼接 URL。WMTS ResourceURL 与解析路由来自同一模板，缺少图层固定样式或矩阵集的模板不对该图层宣告。

WMS 历史版本不是修改 version 字符串：1.0.0 的操作节点、格式节点、OnlineResource 与异常根元素，以及 1.1.0/1.1.1 的 SRS 元素形式分别编码。1.1.1 的 Extent 仅包含旧 DTD 允许的 default 和 nearestValue；multipleValues/current 只在 1.3.0 对应结构中声明。

## 异常

WMS 1.0.0 的 WMS_XML 异常是 WMTException，标准没有为它定义 DTD。XML 校验脚本对它做良构性验证；其余历史文档分别通过固定 DTD，现代文档通过 XSD。

WMS 1.0.0 的默认 INIMAGE 允许错误时不返回内容，本库采用空响应，不为错误内置图像生成器。现代 WMS 只声明 XML 异常；更丰富错误内容 Provider 是后续扩展，不预建空接口。

WMTS REST 的无效资源响应与 KVP 的参数异常状态不同。错误输出同时考虑协议、版本和绑定，不能全局统一返回 JSON 400。

## 验证范围

Schema/DTD 源文件保持原始字节及版权声明，由 `schemas.lock.json` 固定 SHA-256。生产路径不载入外部实体，不联网读取 Schema。结构有效与完整服务一致性是两件事，后者仍需 CITE 和真实业务数据联合测试。
