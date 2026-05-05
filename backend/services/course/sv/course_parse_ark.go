package sv

import (
	"context"
	"encoding/base64"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/LoveLosita/smartflow/backend/model"
	llmservice "github.com/LoveLosita/smartflow/backend/services/llm"
)

// ParseCourseTableImage 使用 Ark SDK Responses 解析课程表图片。
func (ss *CourseService) ParseCourseTableImage(ctx context.Context, req model.CourseImageParseRequest) (*model.CourseImageParseResponse, error) {
	if ss == nil || ss.courseImageResponsesClient == nil {
		modelName := ""
		if ss != nil {
			modelName = ss.courseImageModel
		}
		log.Printf(
			"[COURSE_PARSE][SERVICE] parser unavailable model_name=%q filename=%q mime=%q bytes=%d",
			modelName,
			req.Filename,
			req.MIMEType,
			len(req.ImageBytes),
		)
		return nil, ErrCourseImageParserUnavailable
	}

	normalizedReq, err := normalizeCourseImageParseRequest(req, ss.courseImageConfig)
	if err != nil {
		log.Printf(
			"[COURSE_PARSE][SERVICE] request normalization failed filename=%q mime=%q bytes=%d err=%v",
			req.Filename,
			req.MIMEType,
			len(req.ImageBytes),
			err,
		)
		return nil, err
	}

	log.Printf(
		"[COURSE_PARSE][SERVICE] normalized request model_name=%q filename=%q mime=%q bytes=%d max_bytes=%d",
		ss.courseImageModel,
		normalizedReq.Filename,
		normalizedReq.MIMEType,
		len(normalizedReq.ImageBytes),
		ss.courseImageConfig.MaxImageBytes,
	)

	messages, base64Chars, promptChars := buildCourseImageParseResponsesMessages(normalizedReq)
	startAt := time.Now()
	log.Printf(
		"[COURSE_PARSE][SERVICE] model invoke start model_name=%q filename=%q mime=%q message_count=%d base64_chars=%d prompt_chars=%d payload_chars_estimate=%d thinking=%s temperature=%.2f max_output_tokens=%d text_format=%s",
		ss.courseImageModel,
		normalizedReq.Filename,
		normalizedReq.MIMEType,
		len(messages),
		base64Chars,
		promptChars,
		base64Chars+promptChars+len(strings.TrimSpace(courseImageParseSystemPrompt)),
		llmservice.ThinkingModeDisabled,
		courseImageParseTemperature,
		ss.courseImageConfig.MaxTokens,
		"json_object",
	)

	// 1. 课程表图片识别输出体量大，显式透传 max_output_tokens，避免被默认值截断。
	// 2. text_format 固定为 json_object，降低输出混入解释文本导致解析失败的概率。
	// 3. thinking 显式关闭，优先保证课程导入链路稳定性。
	draft, rawResult, err := llmservice.GenerateArkResponsesJSON[model.CourseImageParseResponse](ctx, ss.courseImageResponsesClient, messages, llmservice.ArkResponsesOptions{
		Temperature:     courseImageParseTemperature,
		MaxOutputTokens: ss.courseImageConfig.MaxTokens,
		Thinking:        llmservice.ThinkingModeDisabled,
		TextFormat:      "json_object",
	})
	if err != nil {
		rawText := ""
		rawChars := 0
		status := ""
		incompleteReason := ""
		errorCode := ""
		errorMessage := ""
		inputTokens := int64(0)
		outputTokens := int64(0)
		totalTokens := int64(0)
		if rawResult != nil {
			rawText = strings.TrimSpace(rawResult.Text)
			rawChars = len(rawText)
			status = strings.TrimSpace(rawResult.Status)
			incompleteReason = strings.TrimSpace(rawResult.IncompleteReason)
			errorCode = strings.TrimSpace(rawResult.ErrorCode)
			errorMessage = strings.TrimSpace(rawResult.ErrorMessage)
			if rawResult.Usage != nil {
				inputTokens = rawResult.Usage.InputTokens
				outputTokens = rawResult.Usage.OutputTokens
				totalTokens = rawResult.Usage.TotalTokens
			}
		}
		log.Printf(
			"[COURSE_PARSE][SERVICE] model invoke failed model_name=%q filename=%q mime=%q cost_ms=%d err=%v status=%q incomplete_reason=%q error_code=%q error_message=%q input_tokens=%d output_tokens=%d total_tokens=%d raw_chars=%d raw_full=\n%s",
			ss.courseImageModel,
			normalizedReq.Filename,
			normalizedReq.MIMEType,
			time.Since(startAt).Milliseconds(),
			err,
			status,
			incompleteReason,
			errorCode,
			errorMessage,
			inputTokens,
			outputTokens,
			totalTokens,
			rawChars,
			rawText,
		)
		if isCourseImageOutputTruncated(rawResult) {
			return nil, fmt.Errorf(
				"课程表识别输出疑似被 max_output_tokens 截断：status=%s incomplete_reason=%s output_tokens=%d max_output_tokens=%d",
				status,
				incompleteReason,
				outputTokens,
				ss.courseImageConfig.MaxTokens,
			)
		}
		return nil, err
	}

	rawText := ""
	rawChars := 0
	status := ""
	incompleteReason := ""
	errorCode := ""
	errorMessage := ""
	inputTokens := int64(0)
	outputTokens := int64(0)
	totalTokens := int64(0)
	if rawResult != nil {
		rawText = strings.TrimSpace(rawResult.Text)
		rawChars = len(rawText)
		status = strings.TrimSpace(rawResult.Status)
		incompleteReason = strings.TrimSpace(rawResult.IncompleteReason)
		errorCode = strings.TrimSpace(rawResult.ErrorCode)
		errorMessage = strings.TrimSpace(rawResult.ErrorMessage)
		if rawResult.Usage != nil {
			inputTokens = rawResult.Usage.InputTokens
			outputTokens = rawResult.Usage.OutputTokens
			totalTokens = rawResult.Usage.TotalTokens
		}
	}
	log.Printf(
		"[COURSE_PARSE][SERVICE] model invoke success model_name=%q filename=%q mime=%q cost_ms=%d status=%q incomplete_reason=%q error_code=%q error_message=%q input_tokens=%d output_tokens=%d total_tokens=%d raw_chars=%d raw_full=\n%s",
		ss.courseImageModel,
		normalizedReq.Filename,
		normalizedReq.MIMEType,
		time.Since(startAt).Milliseconds(),
		status,
		incompleteReason,
		errorCode,
		errorMessage,
		inputTokens,
		outputTokens,
		totalTokens,
		rawChars,
		rawText,
	)

	normalizedDraft, err := normalizeCourseImageParseResponse(draft)
	if err != nil {
		log.Printf(
			"[COURSE_PARSE][SERVICE] draft normalization failed model_name=%q filename=%q err=%v draft_status=%v row_count=%d",
			ss.courseImageModel,
			normalizedReq.Filename,
			err,
			draft.DraftStatus,
			len(draft.Rows),
		)
		return nil, err
	}

	log.Printf(
		"[COURSE_PARSE][SERVICE] draft normalization success model_name=%q filename=%q draft_status=%s rows=%d warnings=%d",
		ss.courseImageModel,
		normalizedReq.Filename,
		normalizedDraft.DraftStatus,
		len(normalizedDraft.Rows),
		len(normalizedDraft.Warnings),
	)

	return normalizedDraft, nil
}

func buildCourseImageParseResponsesMessages(req *model.CourseImageParseRequest) ([]llmservice.ArkResponsesMessage, int, int) {
	userPrompt := fmt.Sprintf(courseImageParseUserPromptTemplate, req.Filename, req.MIMEType)
	base64Data := base64.StdEncoding.EncodeToString(req.ImageBytes)
	imageDataURL := fmt.Sprintf("data:%s;base64,%s", req.MIMEType, base64Data)

	messages := []llmservice.ArkResponsesMessage{
		{
			Role: "system",
			Text: strings.TrimSpace(courseImageParseSystemPrompt),
		},
		{
			Role:        "user",
			Text:        strings.TrimSpace(userPrompt),
			ImageURL:    imageDataURL,
			ImageDetail: "high",
		},
	}
	return messages, len(base64Data), len(strings.TrimSpace(userPrompt))
}

func isCourseImageOutputTruncated(rawResult *llmservice.ArkResponsesResult) bool {
	if rawResult == nil {
		return false
	}

	reason := strings.ToLower(strings.TrimSpace(rawResult.IncompleteReason))
	if strings.Contains(reason, "max_output_tokens") ||
		strings.Contains(reason, "max_tokens") ||
		strings.Contains(reason, "length") {
		return true
	}

	return strings.EqualFold(strings.TrimSpace(rawResult.Status), "incomplete") && reason == ""
}
