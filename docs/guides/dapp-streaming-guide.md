# D-App 流式能力集成指南 (最终版)

本文档旨在为 D-App 开发者提供一个关于如何在 PCAS 生态中**提供和消费“流式能力”**的权威指南。这套模式确保了 D-App 之间的**完全解耦**，是构建可复用、可替换服务的最佳实践。

我们将以一个通用的“实时转录”能力为例进行说明。

## 核心设计哲学: 能力、解耦与智能端点

1.  **能力 (Capability):** D-App 不直接互相调用，而是通过 PCAS 提供或消费抽象的“能力”。一个能力由一个唯一的事件类型字符串标识，例如 `capability.streaming.transcribe.v1`。

2.  **完全解耦:**
    *   **能力提供方** (例如 `DreamTrans` D-App) 向 PCAS 注册自己能提供某种能力。它不知道、也不关心谁会来消费这个能力。
    *   **能力消费方** (例如 `Dreamscribe` D-App) 向 PCAS 请求使用某种能力。它不知道、也不关心是哪个 D-App 在背后提供此能力。

3.  **智能端点与哑管道 (Smart Endpoints and Dumb Pipes):**
    *   **PCAS `InteractStream` 作为“哑管道”:** 提供高效、低延迟的字节流通道，是能力交互的载体。
    *   **能力提供方作为“工具”:** 高效地完成其核心任务（例如：音频流 -> 文本流）。
    *   **能力消费方作为“智能端点”和“知识提炼器”:** 消费方拥有完整的业务上下文，因此它负责将无结构的实时流，异步地提炼、整合成有结构、有意义的“知识事件”，再通过 `Publish` RPC 提交给 PCAS 进行沉淀。

---

## 步骤1: 如何成为一个“能力提供方” (以 `DreamTrans` 为例)

作为能力提供方，您的 D-App 需要作为一个后台服务运行，并实现 `StreamingComputeProvider` 接口。

### 1.1 实现 `StreamingComputeProvider`

```go
package capability_provider

import (
    "context"
    "log"
    "fmt"
    "github.com/soaringjerry/pcas/internal/providers"
)

// Provider 实现了流式能力
type Provider struct{}

func NewProvider() *Provider { return &Provider{} }

// ExecuteStream 是流式处理的核心。它接收输入字节流，返回输出字节流。
func (p *Provider) ExecuteStream(ctx context.Context, attributes map[string]string, input <-chan []byte, output chan<- []byte) error {
    log.Printf("能力提供方: 流式处理已启动。属性: %v", attributes)
    defer log.Println("能力提供方: 流式处理已结束。")
    defer close(output) // 必须在退出时关闭输出 channel

    // 您的核心逻辑，例如转录、翻译等
    for dataChunk := range input {
        // result := YourEngine.Process(dataChunk)
        result := []byte(fmt.Sprintf("Processed: %s", string(dataChunk)))
        
        select {
        case output <- result:
        case <-ctx.Done():
            return ctx.Err()
        }
    }
    return nil
}

// Execute 是为了兼容接口，对于纯流式服务可以返回不支持
func (p *Provider) Execute(ctx context.Context, r map[string]interface{}) (string, error) {
    return "", fmt.Errorf("非流式调用不受支持")
}
```

### 1.2 在 PCAS 中注册能力

您需要让 PCAS 核心知道您的存在。这通常在 PCAS 的主程序启动时完成。

```go
// 在 PCAS 主程序中...
providerMap["dreamtrans-provider"] = capability_provider.NewProvider()
```

然后在 `policy.yaml` 中，将一个“能力”路由到您的 Provider：

```yaml
# policy.yaml
rules:
  - name: "Route transcription capability to DreamTrans provider"
    if:
      event_type: "capability.streaming.transcribe.v1" # 定义能力
    then:
      provider: "dreamtrans-provider" # 路由到您的实现
```

---

## 步骤2: 如何成为一个“能力消费方” (以 `Dreamscribe` 为例)

作为能力消费方，您的 D-App 通过 `InteractStream` RPC 来使用一个抽象的能力。

### 2.1 使用 `InteractStream` 消费能力

```go
// 在 Dreamscribe D-App 的代码中...
import (
    "context"
    "log"
    "io"
    busv1 "github.com/soaringjerry/pcas/gen/go/pcas/bus/v1"
    eventsv1 "github.com/soaringjerry/pcas/gen/go/pcas/events/v1"
    "github.com/google/uuid"
    "google.golang.org/protobuf/types/known/timestamppb"
)

func consumeTranscriptionCapability(ctx context.Context, client busv1.EventBusServiceClient, userID string) {
    // 1. 请求使用一个能力
    stream, err := client.InteractStream(ctx)
    if err != nil { log.Fatalf("InteractStream failed: %v", err) }

    configReq := &busv1.InteractRequest{
        RequestType: &busv1.InteractRequest_Config{
            Config: &busv1.StreamConfig{
                EventType: "capability.streaming.transcribe.v1", // 指定需要的能力
            },
        },
    }
    if err := stream.Send(configReq); err != nil { log.Fatalf("Send config failed: %v", err) }

    // 2. 等待 PCAS 准备好管道
    readyResp, err := stream.Recv()
    if err != nil || readyResp.GetReady() == nil { log.Fatalf("Handshake failed: %v", err) }
    log.Printf("管道已就绪 (Stream ID: %s)，开始发送数据...", readyResp.GetReady().StreamId)

    // 3. 异步接收结果
    go receiveResults(stream, client, userID)

    // 4. 发送数据流
    // for audioChunk := range yourAudioSource {
    //     stream.Send(&busv1.InteractRequest{...})
    // }
    // stream.CloseSend()
}
```

### 2.2 异步提炼知识 (智能端点的核心)

`Dreamscribe` 在自己的进程中，异步地将收到的结果流提炼成结构化的“记忆”事件。

```go
// 在 Dreamscribe D-App 的代码中...

// receiveResults 运行在一个独立的 goroutine 中
func receiveResults(stream busv1.EventBusService_InteractStreamClient, busClient busv1.EventBusServiceClient, userID string) {
    var completeSentence string
    for {
        resp, err := stream.Recv()
        if err != nil {
            if err != io.EOF {
                log.Printf("接收结果时出错: %v", err)
            }
            break
        }

        if data := resp.GetData(); data != nil {
            // 实时显示逻辑 (示例)
            // ui.UpdateRealtimeText(string(data.Content))
            
            // 知识提炼逻辑
            completeSentence += string(data.Content)
            if strings.HasSuffix(completeSentence, "。") { // 假设以句号为一个知识单元
                log.Printf("提炼出一个知识单元: %s", completeSentence)
                
                // 将其包装成一个高质量的记忆事件
                memoryEvent := createMemoryEvent(completeSentence, userID)
                
                // 通过 Publish RPC 提交给 PCAS 进行沉淀
                if _, err := busClient.Publish(context.Background(), memoryEvent); err != nil {
                    log.Printf("发布记忆事件失败: %v", err)
                } else {
                    log.Printf("成功发布记忆事件 ID: %s", memoryEvent.Id)
                }
                
                completeSentence = "" // 重置缓冲区
            }
        }
    }
}

// createMemoryEvent 构造一个结构化的 pcas.memory.create.v1 事件
func createMemoryEvent(text string, userID string) *eventsv1.Event {
    eventID := uuid.New().String()
    return &eventsv1.Event{
        Id:          eventID,
        Specversion: "1.0",
        Type:        "pcas.memory.create.v1",
        Source:      "/d-app/dreamscribe", // 标识事件来源 D-App
        Subject:     text,                // 核心知识内容
        Time:        timestamppb.Now(),
        Userid:      userID,              // 关键的用户上下文
        // Data 字段留空，因为核心信息已在 Subject 中。
        // 如果未来需要更复杂的结构，可以定义专门的 Protobuf 消息并用 Any 包装。
    }
}
```

这份最终的指南清晰地划分了各方职责，并阐明了在 PCAS 生态中构建可复用、解耦的流式服务的最佳实践。