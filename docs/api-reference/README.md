# Protocol Documentation
<a name="top"></a>

## Table of Contents

- [pcas/events/v1/event.proto](#pcas_events_v1_event-proto)
    - [Event](#pcas-events-v1-Event)
    - [Event.AttributesEntry](#pcas-events-v1-Event-AttributesEntry)
    - [EventVectorizedV1](#pcas-events-v1-EventVectorizedV1)
  
- [Scalar Value Types](#scalar-value-types)



<a name="pcas_events_v1_event-proto"></a>
<p align="right"><a href="#top">Top</a></p>

## pcas/events/v1/event.proto
指定我们使用 proto3 语法。


<a name="pcas-events-v1-Event"></a>

### Event
PCAS 的核心事件信封 —— 与 CloudEvents v1.0 兼容。
这是流经整个PCAS事件总线的所有事件的统一结构。

---- CloudEvents 核心属性 (Core Attributes) ----


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| id | [string](#string) |  | ID: 事件的唯一标识符。 在运行时必须非空 (MUST be non-empty)。 |
| source | [string](#string) |  | Source: 事件发生的上下文标识。 通常是一个URI，例如 &#34;/d-app/com.wechat.connector&#34;。 |
| specversion | [string](#string) |  | SpecVersion: 事件所遵循的CloudEvents规范版本。 对于此版本，恒为 &#34;1.0&#34;。 |
| type | [string](#string) |  | Type: 描述与源事件相关的事件类型。 采用反向域名表示法，例如 &#34;pcas.dapp.message.received.v1&#34;。 |
| datacontenttype | [string](#string) |  | DataContentType: &#34;data&#34;属性的内容类型。 可选字段。例如 &#34;application/json&#34;, &#34;application/protobuf&#34;。 |
| dataschema | [string](#string) |  | DataSchema: &#34;data&#34;属性所遵循的schema的URI。 可选字段。 |
| subject | [string](#string) |  | Subject: 在事件生产者上下文中，事件主体的描述。 可选字段。 |
| time | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  | Time: 事件生成的时间戳。 强烈建议设置 (SHOULD be set)；如果缺失，事件总线将自动填充为当前时间。 |
| trace_id | [string](#string) |  | TraceID: 用于在整个因果链中追踪事件的相关性ID。 |
| user_id | [string](#string) |  | UserID: 事件的用户上下文。对未来的多用户系统至关重要。 |
| session_id | [string](#string) |  | SessionID: 用于将一系列相关事件分组的逻辑会话ID。 可选字段。 |
| correlation_id | [string](#string) |  | CorrelationID: 标识直接的&#34;请求-响应&#34;关系。 例如，响应事件的 correlation_id 应该等于触发它的原始事件的 id。 |
| attributes | [Event.AttributesEntry](#pcas-events-v1-Event-AttributesEntry) | repeated | Attributes: dApp 附加的自定义上下文键值对。 记忆和搜索模块可以使用这些属性进行精确检索。 Custom context key-value pairs attached by dApps. Memory and search modules can use these attributes for precise retrieval. |
| data | [google.protobuf.Any](#google-protobuf-Any) |  | Data: 事件的载荷。 `Any` 类型允许我们嵌入任何其他Protobuf消息， 这使得事件信封具有高度的可扩展性和类型安全性。 使用100号是为了给未来的核心扩展属性留出充足空间。 |






<a name="pcas-events-v1-Event-AttributesEntry"></a>

### Event.AttributesEntry



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| key | [string](#string) |  |  |
| value | [string](#string) |  |  |






<a name="pcas-events-v1-EventVectorizedV1"></a>

### EventVectorizedV1
EventVectorizedV1 表示一个向量化事件的数据载荷。
当为原始事件生成嵌入向量后，会创建一个新的事件来存储这个向量信息。


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| original_event_id | [string](#string) |  | 原始事件的ID |
| embedding | [float](#float) | repeated | 事件的嵌入向量 |





 

 

 

 



## Scalar Value Types

| .proto Type | Notes | C++ | Java | Python | Go | C# | PHP | Ruby |
| ----------- | ----- | --- | ---- | ------ | -- | -- | --- | ---- |
| <a name="double" /> double |  | double | double | float | float64 | double | float | Float |
| <a name="float" /> float |  | float | float | float | float32 | float | float | Float |
| <a name="int32" /> int32 | Uses variable-length encoding. Inefficient for encoding negative numbers – if your field is likely to have negative values, use sint32 instead. | int32 | int | int | int32 | int | integer | Bignum or Fixnum (as required) |
| <a name="int64" /> int64 | Uses variable-length encoding. Inefficient for encoding negative numbers – if your field is likely to have negative values, use sint64 instead. | int64 | long | int/long | int64 | long | integer/string | Bignum |
| <a name="uint32" /> uint32 | Uses variable-length encoding. | uint32 | int | int/long | uint32 | uint | integer | Bignum or Fixnum (as required) |
| <a name="uint64" /> uint64 | Uses variable-length encoding. | uint64 | long | int/long | uint64 | ulong | integer/string | Bignum or Fixnum (as required) |
| <a name="sint32" /> sint32 | Uses variable-length encoding. Signed int value. These more efficiently encode negative numbers than regular int32s. | int32 | int | int | int32 | int | integer | Bignum or Fixnum (as required) |
| <a name="sint64" /> sint64 | Uses variable-length encoding. Signed int value. These more efficiently encode negative numbers than regular int64s. | int64 | long | int/long | int64 | long | integer/string | Bignum |
| <a name="fixed32" /> fixed32 | Always four bytes. More efficient than uint32 if values are often greater than 2^28. | uint32 | int | int | uint32 | uint | integer | Bignum or Fixnum (as required) |
| <a name="fixed64" /> fixed64 | Always eight bytes. More efficient than uint64 if values are often greater than 2^56. | uint64 | long | int/long | uint64 | ulong | integer/string | Bignum |
| <a name="sfixed32" /> sfixed32 | Always four bytes. | int32 | int | int | int32 | int | integer | Bignum or Fixnum (as required) |
| <a name="sfixed64" /> sfixed64 | Always eight bytes. | int64 | long | int/long | int64 | long | integer/string | Bignum |
| <a name="bool" /> bool |  | bool | boolean | boolean | bool | bool | boolean | TrueClass/FalseClass |
| <a name="string" /> string | A string must always contain UTF-8 encoded or 7-bit ASCII text. | string | String | str/unicode | string | string | string | String (UTF-8) |
| <a name="bytes" /> bytes | May contain any arbitrary sequence of bytes. | string | ByteString | str | []byte | ByteString | string | String (ASCII-8BIT) |

