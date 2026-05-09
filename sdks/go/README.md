# gateway-llm Go SDK

```go
import gateway "github.com/gateway-llm/gateway-llm/sdks/go"

c := gateway.New(os.Getenv("GATEWAY_KEY"), "https://gateway.yourdomain.com")

out, err := c.Chat(ctx, gateway.ChatRequest{
    Model: "gpt-4o",
    Messages: []gateway.ChatMessage{{Role: "user", Content: "hello"}},
}, gateway.Hints{
    Tags:        []string{"go-sdk", "demo"},
    Quality:     "cheap",
    Speculative: true,
    MaxCostUSD:  0.002,
})

fmt.Println(out.Choices[0].Message.Content)
fmt.Println("savings:", out.GatewayLLM.EstimatedSavingsUSD)
fmt.Println("receipt:", out.GatewayLLM.ReceiptID)

if out.GatewayLLM.ReceiptID != "" {
    rcpt, err := c.VerifyReceipt(ctx, out.GatewayLLM.ReceiptID)
    fmt.Println("verified:", err == nil, rcpt.CostUSD)
}
```
