package llm

import (
	"context"
	"fmt"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/bedrock"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
)

// bedrockClient は Amazon Bedrock 経由で Claude Messages API を叩く（record / live）。
//
// 接続には anthropic-sdk-go の Bedrock backend を使う。SigV4 署名と AWS 認証情報
// チェーン（環境変数 / 共有 config / EKS Pod Identity / IRSA）は SDK に任せる。
// このファイルと factory.go の bedrock 分岐が Bedrock 固有依存のすべてで、
// 剥がすときはここを消して config の既定を anthropic へ戻すだけでよい。
type bedrockClient struct {
	cli  anthropic.Client
	mode string
}

// newBedrock は指定リージョンで Bedrock 向けクライアントを組み立てる。
// region が空なら AWS SDK の既定解決（AWS_REGION 等）に委ねる。
func newBedrock(ctx context.Context, region, mode string) (*bedrockClient, error) {
	loadOpts := []func(*awsconfig.LoadOptions) error{}
	if region != "" {
		loadOpts = append(loadOpts, awsconfig.WithRegion(region))
	}
	cli := anthropic.NewClient(bedrock.WithLoadDefaultConfig(ctx, loadOpts...))
	return &bedrockClient{cli: cli, mode: mode}, nil
}

func (c *bedrockClient) Mode() string { return c.mode }

func (c *bedrockClient) Generate(ctx context.Context, req Request) (Response, error) {
	maxTokens := req.MaxTokens
	if maxTokens == 0 {
		maxTokens = 2048
	}

	params := anthropic.MessageNewParams{
		Model:       anthropic.Model(req.Model),
		MaxTokens:   int64(maxTokens),
		Temperature: anthropic.Float(req.Temperature),
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(req.User)),
		},
	}
	if req.System != "" {
		params.System = []anthropic.TextBlockParam{{Text: req.System}}
	}

	msg, err := c.cli.Messages.New(ctx, params)
	if err != nil {
		return Response{}, fmt.Errorf("Bedrock 呼び出しに失敗: %w", err)
	}

	var text string
	for _, block := range msg.Content {
		if b, ok := block.AsAny().(anthropic.TextBlock); ok {
			text += b.Text
		}
	}
	return Response{
		Text:    text,
		ModelID: string(msg.Model),
		Usage: Usage{
			InputTokens:  int(msg.Usage.InputTokens),
			OutputTokens: int(msg.Usage.OutputTokens),
		},
	}, nil
}
