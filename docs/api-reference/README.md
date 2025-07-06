# Protocol Documentation
<a name="top"></a>

## Table of Contents

- [pcas/bus/v1/bus.proto](#pcas_bus_v1_bus-proto)
    - [InteractRequest](#pcas-bus-v1-InteractRequest)
    - [InteractResponse](#pcas-bus-v1-InteractResponse)
    - [PublishResponse](#pcas-bus-v1-PublishResponse)
    - [SearchRequest](#pcas-bus-v1-SearchRequest)
    - [SearchRequest.AttributeFiltersEntry](#pcas-bus-v1-SearchRequest-AttributeFiltersEntry)
    - [SearchResponse](#pcas-bus-v1-SearchResponse)
    - [StreamConfig](#pcas-bus-v1-StreamConfig)
    - [StreamConfig.AttributesEntry](#pcas-bus-v1-StreamConfig-AttributesEntry)
    - [StreamData](#pcas-bus-v1-StreamData)
    - [StreamEnd](#pcas-bus-v1-StreamEnd)
    - [StreamError](#pcas-bus-v1-StreamError)
    - [StreamReady](#pcas-bus-v1-StreamReady)
    - [SubscribeRequest](#pcas-bus-v1-SubscribeRequest)
  
    - [EventBusService](#pcas-bus-v1-EventBusService)
  
- [Scalar Value Types](#scalar-value-types)



<a name="pcas_bus_v1_bus-proto"></a>
<p align="right"><a href="#top">Top</a></p>

## pcas/bus/v1/bus.proto



<a name="pcas-bus-v1-InteractRequest"></a>

### InteractRequest
InteractRequest represents a client request in the bidirectional stream.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| config | [StreamConfig](#pcas-bus-v1-StreamConfig) |  | The first message sent by the client to configure the stream. |
| data | [StreamData](#pcas-bus-v1-StreamData) |  | Subsequent messages containing data chunks. |
| client_end | [StreamEnd](#pcas-bus-v1-StreamEnd) |  | Explicit end signal from the client, indicating no more data will be sent. |






<a name="pcas-bus-v1-InteractResponse"></a>

### InteractResponse
InteractResponse represents a server response in the bidirectional stream.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| ready | [StreamReady](#pcas-bus-v1-StreamReady) |  | A message indicating the stream is ready and configured. |
| data | [StreamData](#pcas-bus-v1-StreamData) |  | Subsequent messages containing data chunks. |
| error | [StreamError](#pcas-bus-v1-StreamError) |  | An error message if something goes wrong during the stream. |
| server_end | [StreamEnd](#pcas-bus-v1-StreamEnd) |  | Explicit end signal from the server, indicating no more data will be sent. |






<a name="pcas-bus-v1-PublishResponse"></a>

### PublishResponse
PublishResponse is the response from publishing an event

Empty for now, but reserved for future use (e.g., acknowledgment ID, status)






<a name="pcas-bus-v1-SearchRequest"></a>

### SearchRequest
SearchRequest is the request for semantic search


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| query_text | [string](#string) |  | The natural language query text |
| top_k | [int32](#int32) |  | Number of top results to return (default: 5) |
| user_id | [string](#string) |  | Optional user ID to filter results by |
| attribute_filters | [SearchRequest.AttributeFiltersEntry](#pcas-bus-v1-SearchRequest-AttributeFiltersEntry) | repeated | Attribute filters for metadata pre-filtering (AND logic) 用于元数据预过滤的属性过滤器（AND逻辑） |






<a name="pcas-bus-v1-SearchRequest-AttributeFiltersEntry"></a>

### SearchRequest.AttributeFiltersEntry



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| key | [string](#string) |  |  |
| value | [string](#string) |  |  |






<a name="pcas-bus-v1-SearchResponse"></a>

### SearchResponse
SearchResponse is the response from semantic search


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| events | [pcas.events.v1.Event](#pcas-events-v1-Event) | repeated | The matching events found |
| scores | [float](#float) | repeated | Similarity scores corresponding to each event (0.0 to 1.0) |






<a name="pcas-bus-v1-StreamConfig"></a>

### StreamConfig
StreamConfig defines the initial configuration for an interaction stream.
Its primary purpose is to declare the intent of the stream via an event type,
which is used by the Policy Engine for routing.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| event_type | [string](#string) |  | The event type that defines this interaction, e.g., &#34;dapp.aipen.translate.stream.v1&#34;. This is the key for routing the entire stream to the correct provider. |
| attributes | [StreamConfig.AttributesEntry](#pcas-bus-v1-StreamConfig-AttributesEntry) | repeated | Optional, additional attributes for the stream&#39;s context. |






<a name="pcas-bus-v1-StreamConfig-AttributesEntry"></a>

### StreamConfig.AttributesEntry



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| key | [string](#string) |  |  |
| value | [string](#string) |  |  |






<a name="pcas-bus-v1-StreamData"></a>

### StreamData
StreamData carries the actual payload in the stream.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| content | [bytes](#bytes) |  | The raw data content. |






<a name="pcas-bus-v1-StreamEnd"></a>

### StreamEnd
StreamEnd is an empty message that signals the graceful end of one
direction of the stream.






<a name="pcas-bus-v1-StreamError"></a>

### StreamError
StreamError represents a terminal error that occurred during the stream.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| code | [int32](#int32) |  | A status code for the error. |
| message | [string](#string) |  | A human-readable error message. |






<a name="pcas-bus-v1-StreamReady"></a>

### StreamReady
StreamReady indicates the server has successfully configured the stream
and is ready to process data.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| stream_id | [string](#string) |  | A unique ID assigned by the server to this interaction stream. |






<a name="pcas-bus-v1-SubscribeRequest"></a>

### SubscribeRequest
SubscribeRequest is the request for subscribing to the event stream


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| client_id | [string](#string) |  | Unique identifier for the client subscribing to events |





 

 

 


<a name="pcas-bus-v1-EventBusService"></a>

### EventBusService
EventBusService provides methods for publishing events to the PCAS event bus

| Method Name | Request Type | Response Type | Description |
| ----------- | ------------ | ------------- | ------------|
| Publish | [.pcas.events.v1.Event](#pcas-events-v1-Event) | [PublishResponse](#pcas-bus-v1-PublishResponse) | Publish sends an event to the event bus |
| Subscribe | [SubscribeRequest](#pcas-bus-v1-SubscribeRequest) | [.pcas.events.v1.Event](#pcas-events-v1-Event) stream | Subscribe allows clients to receive a stream of events |
| Search | [SearchRequest](#pcas-bus-v1-SearchRequest) | [SearchResponse](#pcas-bus-v1-SearchResponse) | Search performs semantic search on stored events |
| InteractStream | [InteractRequest](#pcas-bus-v1-InteractRequest) stream | [InteractResponse](#pcas-bus-v1-InteractResponse) stream | InteractStream provides bidirectional streaming for low-latency real-time interactions The first request MUST be a StreamConfig message 提供双向流式通道，用于低延迟实时交互 首个请求必须是 StreamConfig 消息 |

 



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

