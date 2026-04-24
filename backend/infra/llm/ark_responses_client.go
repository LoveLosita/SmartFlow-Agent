package llm

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/volcengine/volcengine-go-sdk/service/arkruntime"
	"github.com/volcengine/volcengine-go-sdk/service/arkruntime/model/responses"
)

// ArkResponsesMessage 描述一次 Responses 输入消息。
//
// 职责边界：
// 1. 负责表达角色与多模态内容（文本/图片）；
// 2. 不负责业务 prompt 生成；
// 3. 不负责输出 JSON 的字段校验。
type ArkResponsesMessage struct {
	Role        string
	Text        string
	ImageURL    string
	ImageDetail string
}

// ArkResponsesOptions 描述 Responses 生成选项。
type ArkResponsesOptions struct {
	Model           string
	Temperature     float64
	MaxOutputTokens int
	Thinking        ThinkingMode
	TextFormat      string
}

// ArkResponsesUsage 统一透传 token 使用量。
type ArkResponsesUsage struct {
	InputTokens  int64
	OutputTokens int64
	TotalTokens  int64
}

// ArkResponsesResult 是 Ark Responses 的统一输出结构。
type ArkResponsesResult struct {
	Text             string
	Status           string
	IncompleteReason string
	ErrorCode        string
	ErrorMessage     string
	Usage            *ArkResponsesUsage
}

// ArkResponsesClient 是 Ark SDK Responses 的统一模型出口。
type ArkResponsesClient struct {
	model  string
	client *arkruntime.Client
}

// NewArkResponsesClient 创建 Ark SDK Responses 客户端。
//
// 说明：
// 1. model 为空时返回 nil，表示当前能力未启用；
// 2. baseURL 为空时使用 SDK 默认地址；
// 3. 仅负责客户端创建，不做连通性探测。
func NewArkResponsesClient(apiKey string, baseURL string, model string) *ArkResponsesClient {
	model = strings.TrimSpace(model)
	if model == "" {
		return nil
	}

	options := make([]arkruntime.ConfigOption, 0, 1)
	if strings.TrimSpace(baseURL) != "" {
		options = append(options, arkruntime.WithBaseUrl(strings.TrimSpace(baseURL)))
	}

	return &ArkResponsesClient{
		model:  model,
		client: arkruntime.NewClientWithApiKey(strings.TrimSpace(apiKey), options...),
	}
}

// GenerateText 执行一次非流式 Responses 调用并提取文本。
func (c *ArkResponsesClient) GenerateText(ctx context.Context, messages []ArkResponsesMessage, options ArkResponsesOptions) (*ArkResponsesResult, error) {
	req, err := c.buildRequest(messages, options)
	if err != nil {
		return nil, err
	}

	resp, err := c.client.CreateResponses(ctx, req)
	if err != nil {
		return nil, err
	}

	result := buildArkResponsesResult(resp)
	if result.Status == "failed" {
		if result.ErrorMessage != "" {
			return result, fmt.Errorf("ark responses failed: %s", result.ErrorMessage)
		}
		return result, errors.New("ark responses failed")
	}

	if strings.TrimSpace(result.Text) == "" {
		return result, FormatEmptyResponseError("ark_responses")
	}
	return result, nil
}

// GenerateArkResponsesJSON 先调用 Responses，再解析为 JSON 结构体。
func GenerateArkResponsesJSON[T any](ctx context.Context, client *ArkResponsesClient, messages []ArkResponsesMessage, options ArkResponsesOptions) (*T, *ArkResponsesResult, error) {
	if client == nil {
		return nil, nil, errors.New("ark responses client is not ready")
	}

	result, err := client.GenerateText(ctx, messages, options)
	if err != nil {
		return nil, result, err
	}

	parsed, err := ParseJSONObject[T](result.Text)
	if err != nil {
		return nil, result, err
	}
	return parsed, result, nil
}

func (c *ArkResponsesClient) buildRequest(messages []ArkResponsesMessage, options ArkResponsesOptions) (*responses.ResponsesRequest, error) {
	if c == nil || c.client == nil {
		return nil, errors.New("ark responses client is not ready")
	}
	if len(messages) == 0 {
		return nil, errors.New("ark responses messages is empty")
	}

	modelName := strings.TrimSpace(options.Model)
	if modelName == "" {
		modelName = c.model
	}
	if modelName == "" {
		return nil, errors.New("ark responses model is empty")
	}

	inputItems := make([]*responses.InputItem, 0, len(messages))
	for idx := range messages {
		item, err := buildInputItem(messages[idx])
		if err != nil {
			return nil, fmt.Errorf("build ark responses message[%d] failed: %w", idx, err)
		}
		inputItems = append(inputItems, item)
	}

	request := &responses.ResponsesRequest{
		Model: modelName,
		Input: &responses.ResponsesInput{
			Union: &responses.ResponsesInput_ListValue{
				ListValue: &responses.InputItemList{ListValue: inputItems},
			},
		},
	}

	if options.Temperature > 0 {
		request.Temperature = float64Ptr(options.Temperature)
	}
	if options.MaxOutputTokens > 0 {
		request.MaxOutputTokens = int64Ptr(int64(options.MaxOutputTokens))
	}

	switch options.Thinking {
	case ThinkingModeEnabled:
		thinkingType := responses.ThinkingType_enabled
		request.Thinking = &responses.ResponsesThinking{Type: &thinkingType}
	case ThinkingModeDisabled:
		thinkingType := responses.ThinkingType_disabled
		request.Thinking = &responses.ResponsesThinking{Type: &thinkingType}
	}

	if textType, ok := parseTextType(options.TextFormat); ok {
		request.Text = &responses.ResponsesText{
			Format: &responses.TextFormat{
				Type: textType,
			},
		}
	}

	return request, nil
}

func buildInputItem(message ArkResponsesMessage) (*responses.InputItem, error) {
	role, ok := parseMessageRole(message.Role)
	if !ok {
		return nil, fmt.Errorf("unsupported message role: %s", strings.TrimSpace(message.Role))
	}

	content := make([]*responses.ContentItem, 0, 2)
	if text := strings.TrimSpace(message.Text); text != "" {
		content = append(content, &responses.ContentItem{
			Union: &responses.ContentItem_Text{
				Text: &responses.ContentItemText{
					Type: responses.ContentItemType_input_text,
					Text: text,
				},
			},
		})
	}

	if imageURL := strings.TrimSpace(message.ImageURL); imageURL != "" {
		image := &responses.ContentItemImage{
			Type:     responses.ContentItemType_input_image,
			ImageUrl: stringPtr(imageURL),
		}
		if detail, ok := parseImageDetail(message.ImageDetail); ok {
			image.Detail = &detail
		}

		content = append(content, &responses.ContentItem{
			Union: &responses.ContentItem_Image{
				Image: image,
			},
		})
	}

	if len(content) == 0 {
		return nil, errors.New("message content is empty")
	}

	return &responses.InputItem{
		Union: &responses.InputItem_InputMessage{
			InputMessage: &responses.ItemInputMessage{
				Role:    role,
				Content: content,
			},
		},
	}, nil
}

func buildArkResponsesResult(resp *responses.ResponseObject) *ArkResponsesResult {
	if resp == nil {
		return &ArkResponsesResult{}
	}

	result := &ArkResponsesResult{
		Text:   extractArkResponsesText(resp),
		Status: strings.TrimSpace(resp.GetStatus().String()),
	}

	if details := resp.GetIncompleteDetails(); details != nil {
		result.IncompleteReason = strings.TrimSpace(details.GetReason())
	}

	if responseErr := resp.GetError(); responseErr != nil {
		result.ErrorCode = strings.TrimSpace(responseErr.GetCode())
		result.ErrorMessage = strings.TrimSpace(responseErr.GetMessage())
	}

	if usage := resp.GetUsage(); usage != nil {
		result.Usage = &ArkResponsesUsage{
			InputTokens:  usage.GetInputTokens(),
			OutputTokens: usage.GetOutputTokens(),
			TotalTokens:  usage.GetTotalTokens(),
		}
	}
	return result
}

func extractArkResponsesText(resp *responses.ResponseObject) string {
	if resp == nil {
		return ""
	}

	textParts := make([]string, 0, 2)
	for _, outputItem := range resp.GetOutput() {
		outputMessage := outputItem.GetOutputMessage()
		if outputMessage == nil {
			continue
		}

		for _, contentItem := range outputMessage.GetContent() {
			text := strings.TrimSpace(contentItem.GetText().GetText())
			if text == "" {
				continue
			}
			textParts = append(textParts, text)
		}
	}
	return strings.TrimSpace(strings.Join(textParts, "\n"))
}

func parseMessageRole(raw string) (responses.MessageRole_Enum, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "user":
		return responses.MessageRole_user, true
	case "system":
		return responses.MessageRole_system, true
	case "developer":
		return responses.MessageRole_developer, true
	case "assistant":
		return responses.MessageRole_assistant, true
	default:
		return responses.MessageRole_unspecified, false
	}
}

func parseImageDetail(raw string) (responses.ContentItemImageDetail_Enum, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "high":
		return responses.ContentItemImageDetail_high, true
	case "low":
		return responses.ContentItemImageDetail_low, true
	case "auto":
		return responses.ContentItemImageDetail_auto, true
	default:
		return responses.ContentItemImageDetail_auto, false
	}
}

func parseTextType(raw string) (responses.TextType_Enum, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "":
		return responses.TextType_unspecified, false
	case "text":
		return responses.TextType_text, true
	case "json_object":
		return responses.TextType_json_object, true
	default:
		return responses.TextType_unspecified, false
	}
}

func stringPtr(value string) *string {
	return &value
}

func float64Ptr(value float64) *float64 {
	return &value
}

func int64Ptr(value int64) *int64 {
	return &value
}
