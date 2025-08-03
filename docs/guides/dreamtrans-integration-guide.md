# DreamTrans D-App 集成指南 (流式版)

本文档旨在指导 DreamTrans D-App 的开发者如何将其服务与 PCAS 事件总线集成，以实现与 Dreamscribe D-App 的高效、实时协作。**此版本使用了 PCAS 先进的 `InteractStream` RPC，是实现流式交互的最佳实践。**

## 核心场景 (流式优化)

1.  **Dreamscribe** 希望对某个音频源进行实时转录。
2.  **Dreamscribe** 向 PCAS 发起一个 `InteractStream` gRPC 调用，并在初始的 `StreamConfig` 中指定 `event_type` 为 `dapp.dreamtrans.streaming_transcribe.v1`。
3.  **PCAS 策略引擎** 查找到该 `event_type` 对应的服务提供者是 `dreamtrans-provider` (即您的 D-App)。
4.  **Dreamscribe** 将原始的音频流数据（`[]byte`）持续发送到 `InteractStream` 中。
5.  **DreamTrans D-App** (作为 `dreamtrans-provider`) 从它与 PCAS 建立的 `InteractStream` 连接中接收到音频流数据。
6.  **DreamTrans D-App** 对接收到的音频流进行实时转录，并将转录后的文本结果（`[]byte`）流式地写回到 `InteractStream` 中。
7.  **Dreamscribe** 从 `InteractStream` 中实时接收转录文本并展示给用户。

## 步骤1：作为服务提供者接入 PCAS

与之前的 Pub/Sub 模型不同，在这个模型中，DreamTrans 不再是一个被动的“订阅者”，而是一个主动的“服务提供者”（`StreamingComputeProvider`）。您需要实现这个接口，并让 PCAS 在启动时加载您。

### 1.1 实现 `StreamingComputeProvider` 接口

您需要创建一个 Go 项目，并实现 `providers.StreamingComputeProvider` 接口。

```go
package dreamtrans_provider

import (
    "context"
    "log"
    "time"
    "fmt"

    "github.com/soaringjerry/pcas/internal/providers"
)

// Provider 实现了 StreamingComputeProvider 接口
type Provider struct{}

func NewProvider() *Provider {
    return &Provider{}
}

// Execute 是为了兼容 ComputeProvider 接口，此处可以简单实现
func (p *Provider) Execute(ctx context.Context, requestData map[string]interface{}) (string, error) {
    return "", fmt.Errorf("Execute is not supported, please use ExecuteStream")
}

// ExecuteStream 是流式处理的核心
func (p *Provider) ExecuteStream(ctx context.Context, attributes map[string]string, input <-chan []byte, output chan<- []byte) error {
    log.Println("DreamTrans Provider: ExecuteStream started.")
    defer log.Println("DreamTrans Provider: ExecuteStream finished.")
    defer close(output) // 确保在函数退出时关闭输出 channel

    // 模拟实时转录
    for audioChunk := range input {
        // 您的核心转录逻辑在这里
        // transcriptionResult := YourTranscriptionEngine.Process(audioChunk)
        
        // 伪代码：简单地将收到的内容加上前缀返回
        transcriptionResult := "Transcribed: " + string(audioChunk)
        
        log.Printf("DreamTrans: Received %d bytes, sending result: %s", len(audioChunk), transcriptionResult)

        select {
        case output <- []byte(transcriptionResult):
            // 成功发送结果
        case <-ctx.Done():
            // 上下文被取消（例如，客户端断开连接）
            log.Println("DreamTrans Provider: Context cancelled, stopping stream.")
            return ctx.Err()
        }
    }

    return nil
}
```

### 1.2 让 PCAS 加载您的 Provider

您需要修改 PCAS 的主程序（或通过插件机制，如果未来支持的话），将您的 `dreamtrans_provider` 注册到 provider map 中。

*这部分属于 PCAS 核心的修改，超出了 D-App 开发者的范畴，但为了完整性在此说明。*

### 1.3 配置策略

在 `policy.yaml` 中，添加一条规则，将特定的 `event_type` 路由到您的 provider。

```yaml
rules:
  # ... 其他规则 ...

  - name: "Route streaming transcription to DreamTrans D-App"
    if:
      event_type: "dapp.dreamtrans.streaming_transcribe.v1"
    then:
      provider: dreamtrans-provider # 这个名字需要与您在PCAS中注册的名字一致
```

## 步骤2：Dreamscribe (调用方) 的实现

作为对比，这是 Dreamscribe D-App（调用方）需要实现的代码。

```go
import (
    "context"
    "log"
    "time"
    "io"
    busv1 "github.com/soaringjerry/pcas/gen/go/pcas/bus/v1"
)

func startTranscriptionStream(ctx context.Context, client busv1.EventBusServiceClient) {
    // 调用 InteractStream RPC
    stream, err := client.InteractStream(ctx)
    if err != nil {
        log.Fatalf("InteractStream failed: %v", err)
    }

    // 1. 发送握手配置信息
    configReq := &busv1.InteractRequest{
        RequestType: &busv1.InteractRequest_Config{
            Config: &busv1.StreamConfig{
                EventType: "dapp.dreamtrans.streaming_transcribe.v1",
            },
        },
    }
    if err := stream.Send(configReq); err != nil {
        log.Fatalf("Failed to send config: %v", err)
    }

    // 2. 等待服务端的 Ready 信号
    readyResp, err := stream.Recv()
    if err != nil {
        log.Fatalf("Failed to receive ready signal: %v", err)
    }
    if _, ok := readyResp.ResponseType.(*busv1.InteractResponse_Ready); !ok {
        log.Fatalf("Expected StreamReady, but got %T", readyResp.ResponseType)
    }
    log.Println("Stream is ready, starting to send audio data...")

    // 3. 启动一个 goroutine 来接收结果
    go func() {
        for {
            resp, err := stream.Recv()
            if err == io.EOF {
                log.Println("Server closed the stream.")
                return
            }
            if err != nil {
                log.Printf("Error receiving from server: %v", err)
                return
            }
            
            if data := resp.GetData(); data != nil {
                log.Printf("Received transcription: %s", string(data.Content))
            }
        }
    }()

    // 4. 主循环：发送音频数据
    for i := 0; i < 5; i++ {
        audioData := []byte(fmt.Sprintf("audio_chunk_%d", i))
        dataReq := &busv1.InteractRequest{
            RequestType: &busv1.InteractRequest_Data{
                Data: &busv1.StreamData{Content: audioData},
            },
        }
        if err := stream.Send(dataReq); err != nil {
            log.Printf("Failed to send audio data: %v", err)
            break
        }
        time.Sleep(1 * time.Second)
    }

    // 5. 发送结束信号并关闭
    stream.CloseSend()
    log.Println("Finished sending audio data.")
    time.Sleep(2 * time.Second) // 等待接收最后的转录结果
}
```

## 新模型的优势

*   **高效性：** 避免了为每个数据片段创建和处理完整 CloudEvent 的开销，网络利用率更高。
*   **低延迟：** 单一的、持久化的 TCP 连接显著降低了实时交互的延迟。
*   **简单性：** 对于服务提供方（DreamTrans），逻辑更简单，只需实现一个处理字节流的函数即可，无需关心事件的构造和解析。
*   **资源友好：** 减少了 gRPC 调用和数据库写入次数，对 PCAS 服务器的负载更小。

这份更新后的指南现在反映了实现 D-App 间流式通信的最佳实践。