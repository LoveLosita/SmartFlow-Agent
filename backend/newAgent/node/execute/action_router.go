package newagentexecute

import (
	"context"
	"fmt"
	newagentshared "github.com/LoveLosita/smartflow/backend/newAgent/shared"
	"io"
	"log"
	"strings"

	newagentmodel "github.com/LoveLosita/smartflow/backend/newAgent/model"
	newagentrouter "github.com/LoveLosita/smartflow/backend/newAgent/router"
	newagentstream "github.com/LoveLosita/smartflow/backend/newAgent/stream"
	newagenttools "github.com/LoveLosita/smartflow/backend/newAgent/tools"
	llmservice "github.com/LoveLosita/smartflow/backend/services/llm"
	"github.com/cloudwego/eino/schema"
	"github.com/google/uuid"
)

type executeDecisionStreamOutput struct {
	decision         *newagentmodel.ExecuteDecision
	rawText          string
	parsedBeforeText string
	parsedAfterText  string
	streamedSpeak    string
	speakStreamed    bool
	firstChunk       bool
}

func collectExecuteDecisionFromLLM(
	ctx context.Context,
	input ExecuteNodeInput,
	flowState *newagentmodel.CommonState,
	conversationContext *newagentmodel.ConversationContext,
	emitter *newagentstream.ChunkEmitter,
	messages []*schema.Message,
) (*executeDecisionStreamOutput, error) {
	reader, err := input.Client.Stream(
		ctx,
		messages,
		llmservice.GenerateOptions{
			Temperature: 1.0,
			MaxTokens:   131072,
			Thinking:    newagentshared.ResolveThinkingMode(input.ThinkingEnabled),
			Metadata: map[string]any{
				"stage":      executeStageName,
				"step_index": flowState.CurrentStep,
				"round_used": flowState.RoundUsed,
			},
		},
	)
	if err != nil {
		return nil, fmt.Errorf("执行阶段 Stream 请求失败: %w", err)
	}

	parser := newagentrouter.NewStreamDecisionParser()
	output := &executeDecisionStreamOutput{firstChunk: true}
	var fullText strings.Builder
	reasoningDigestor, digestorErr := emitter.NewReasoningDigestor(ctx, executeSpeakBlockID, executeStageName)
	if digestorErr != nil {
		return nil, fmt.Errorf("执行 thinking 摘要器初始化失败: %w", digestorErr)
	}
	defer func() {
		if reasoningDigestor != nil {
			_ = reasoningDigestor.Close(ctx)
		}
	}()

	for {
		chunk, recvErr := reader.Recv()
		if recvErr == io.EOF {
			break
		}
		if recvErr != nil {
			log.Printf("[WARN] execute stream recv error chat=%s err=%v", flowState.ConversationID, recvErr)
			break
		}

		if chunk != nil && strings.TrimSpace(chunk.ReasoningContent) != "" {
			if reasoningDigestor != nil {
				reasoningDigestor.Append(chunk.ReasoningContent)
			}
		}

		content := ""
		if chunk != nil {
			content = chunk.Content
		}

		visible, ready, _ := parser.Feed(content)
		if !ready {
			continue
		}

		result := parser.Result()
		output.rawText = result.RawBuffer
		output.parsedBeforeText = result.BeforeText
		output.parsedAfterText = result.AfterText

		if result.Fallback || result.ParseFailed {
			log.Printf(
				"[DEBUG] execute LLM 决策解析失败 chat=%s round=%d raw=%s",
				flowState.ConversationID,
				flowState.RoundUsed,
				output.rawText,
			)
			flowState.ConsecutiveCorrections++
			if flowState.ConsecutiveCorrections >= maxConsecutiveCorrections {
				return nil, fmt.Errorf(
					"连续 %d 次解析决策 JSON 失败，终止执行。原始输出=%s",
					flowState.ConsecutiveCorrections,
					output.rawText,
				)
			}

			errorDesc := "未识别到合法的 SMARTFLOW_DECISION 标签，无法继续解析。"
			optionHint := "请输出一个 <SMARTFLOW_DECISION>{JSON}</SMARTFLOW_DECISION>，然后再在标签外补充可见文本。"
			if strings.Contains(output.rawText, `"tool_call": [`) || strings.Contains(output.rawText, `"tool_call":[`) {
				errorDesc = "检测到 tool_call 字段被错误写成数组；每次只允许调用一个工具，不支持数组形式。"
				optionHint = "请把多次工具调用拆开，每次只保留一个 tool_call，然后再继续下一轮。"
			}
			newagentshared.AppendLLMCorrectionWithHint(conversationContext, output.rawText, errorDesc, optionHint)
			return nil, nil
		}

		decision, parseErr := llmservice.ParseJSONObject[newagentmodel.ExecuteDecision](result.DecisionJSON)
		if parseErr != nil {
			log.Printf(
				"[DEBUG] execute LLM JSON 解析失败 chat=%s round=%d json=%s raw=%s",
				flowState.ConversationID,
				flowState.RoundUsed,
				result.DecisionJSON,
				output.rawText,
			)
			flowState.ConsecutiveCorrections++
			if flowState.ConsecutiveCorrections >= maxConsecutiveCorrections {
				return nil, fmt.Errorf(
					"连续 %d 次解析决策 JSON 失败，终止执行。原始输出=%s",
					flowState.ConsecutiveCorrections,
					output.rawText,
				)
			}
			newagentshared.AppendLLMCorrectionWithHint(
				conversationContext,
				"",
				"决策标签内的 JSON 格式不合法。",
				"请确保 <SMARTFLOW_DECISION> 标签内是合法 JSON；当 action=next_plan/done 时，goal_check 必须是字符串（不要输出对象）。",
			)
			return nil, nil
		}
		output.decision = decision

		if visible != "" {
			if reasoningDigestor != nil {
				reasoningDigestor.MarkContentStarted()
			}
			if emitErr := emitter.EmitAssistantText(
				executeSpeakBlockID,
				executeStageName,
				visible,
				output.firstChunk,
			); emitErr != nil {
				return nil, fmt.Errorf("执行回答推送失败: %w", emitErr)
			}
			output.speakStreamed = true
			fullText.WriteString(visible)
			output.firstChunk = false
		}

		for {
			chunk2, recvErr2 := reader.Recv()
			if recvErr2 == io.EOF {
				break
			}
			if recvErr2 != nil {
				log.Printf("[WARN] execute speak stream error chat=%s err=%v", flowState.ConversationID, recvErr2)
				break
			}
			if chunk2 == nil {
				continue
			}
			if strings.TrimSpace(chunk2.ReasoningContent) != "" {
				if reasoningDigestor != nil {
					reasoningDigestor.Append(chunk2.ReasoningContent)
				}
			}
			if chunk2.Content != "" {
				if reasoningDigestor != nil {
					reasoningDigestor.MarkContentStarted()
				}
				if emitErr := emitter.EmitAssistantText(
					executeSpeakBlockID,
					executeStageName,
					chunk2.Content,
					output.firstChunk,
				); emitErr != nil {
					return nil, fmt.Errorf("执行回答推送失败: %w", emitErr)
				}
				output.speakStreamed = true
				fullText.WriteString(chunk2.Content)
				output.firstChunk = false
			}
		}
		break
	}

	if output.decision == nil {
		if strings.TrimSpace(output.rawText) == "" {
			log.Printf(
				"[WARN] execute LLM 返回空文本 chat=%s round=%d consecutive=%d/%d",
				flowState.ConversationID,
				flowState.RoundUsed,
				flowState.ConsecutiveCorrections+1,
				maxConsecutiveCorrections,
			)
			flowState.ConsecutiveCorrections++
			if flowState.ConsecutiveCorrections >= maxConsecutiveCorrections {
				return nil, fmt.Errorf("连续 %d 次模型返回空文本，终止执行", flowState.ConsecutiveCorrections)
			}
			newagentshared.AppendLLMCorrectionWithHint(
				conversationContext,
				"",
				"模型没有返回任何内容。",
				"请至少返回一个 <SMARTFLOW_DECISION>{JSON}</SMARTFLOW_DECISION> 形式的执行决策。",
			)
			return nil, nil
		}
		return nil, fmt.Errorf("执行阶段模型输出中未提取到决策标签")
	}

	output.streamedSpeak = fullText.String()
	output.decision.Speak = pickExecuteVisibleSpeak(
		output.streamedSpeak,
		output.parsedAfterText,
		output.parsedBeforeText,
		output.decision,
	)
	log.Printf(
		"[DEBUG] execute LLM 响应 chat=%s round=%d action=%s speak_len=%d raw_len=%d raw_preview=%.200s",
		flowState.ConversationID,
		flowState.RoundUsed,
		output.decision.Action,
		len(output.decision.Speak),
		len(output.rawText),
		output.rawText,
	)
	return output, nil
}

func handleExecuteDecision(
	ctx context.Context,
	input ExecuteNodeInput,
	runtimeState *newagentmodel.AgentRuntimeState,
	flowState *newagentmodel.CommonState,
	conversationContext *newagentmodel.ConversationContext,
	emitter *newagentstream.ChunkEmitter,
	output *executeDecisionStreamOutput,
) error {
	if output == nil || output.decision == nil {
		return nil
	}

	decision := output.decision
	if decision.Action == newagentmodel.ExecuteActionDone &&
		decision.ToolCall != nil &&
		strings.EqualFold(strings.TrimSpace(decision.ToolCall.Name), newagenttools.ToolNameContextToolsRemove) {
		decision.ToolCall = nil
	}

	if err := decision.Validate(); err != nil {
		flowState.ConsecutiveCorrections++
		log.Printf(
			"[WARN] execute 决策不合法 chat=%s round=%d consecutive=%d/%d err=%s",
			flowState.ConversationID,
			flowState.RoundUsed,
			flowState.ConsecutiveCorrections,
			maxConsecutiveCorrections,
			err.Error(),
		)
		if flowState.ConsecutiveCorrections >= maxConsecutiveCorrections {
			return fmt.Errorf(
				"连续 %d 次决策不合法，终止执行。%s (原始输出: %s)",
				flowState.ConsecutiveCorrections,
				err.Error(),
				output.rawText,
			)
		}
		_ = emitter.EmitStatus(
			executeStatusBlockID,
			executeStageName,
			"executing",
			fmt.Sprintf("执行校验：决策不合法：%s，已请求模型重试。", err.Error()),
			false,
		)
		newagentshared.AppendLLMCorrectionWithHint(
			conversationContext,
			"",
			fmt.Sprintf("本次执行决策不合法：%s", err.Error()),
			"合法的 action 包括：continue（继续当前步骤）、ask_user（追问用户）、confirm（写操作确认）、next_plan（推进到下一步）、done（任务完成）、abort（正式终止本轮流程）。",
		)
		return nil
	}

	flowState.ConsecutiveCorrections = 0
	decision.Speak = pickExecuteVisibleSpeak(
		decision.Speak,
		output.parsedAfterText,
		output.parsedBeforeText,
		decision,
	)
	decision.Speak = normalizeSpeak(decision.Speak)

	if decision.Action == newagentmodel.ExecuteActionConfirm &&
		decision.ToolCall != nil &&
		input.ToolRegistry != nil &&
		!input.ToolRegistry.IsWriteTool(decision.ToolCall.Name) {
		decision.Action = newagentmodel.ExecuteActionContinue
	}

	if decision.Action == newagentmodel.ExecuteActionContinue &&
		decision.ToolCall != nil &&
		newagenttools.IsContextManagementTool(decision.ToolCall.Name) {
		decision.Speak = ""
	}

	if !output.speakStreamed && strings.TrimSpace(decision.Speak) != "" {
		if emitErr := emitter.EmitAssistantText(
			executeSpeakBlockID,
			executeStageName,
			decision.Speak,
			output.firstChunk,
		); emitErr != nil {
			return fmt.Errorf("执行回答补发失败: %w", emitErr)
		}
		output.speakStreamed = true
		output.firstChunk = false
	}

	if output.speakStreamed {
		if tail := buildExecuteNormalizedSpeakTail(output.streamedSpeak, decision.Speak); tail != "" {
			if emitErr := emitter.EmitAssistantText(
				executeSpeakBlockID,
				executeStageName,
				tail,
				output.firstChunk,
			); emitErr != nil {
				return fmt.Errorf("执行回答尾段补发失败: %w", emitErr)
			}
			output.firstChunk = false
		}
	}

	if flowState.HasPlan() &&
		(decision.Action == newagentmodel.ExecuteActionNextPlan ||
			decision.Action == newagentmodel.ExecuteActionDone) {
		if strings.TrimSpace(decision.GoalCheck) == "" {
			flowState.ConsecutiveCorrections++
			if flowState.ConsecutiveCorrections >= maxConsecutiveCorrections {
				return fmt.Errorf("连续 %d 次 goal_check 为空，终止执行", flowState.ConsecutiveCorrections)
			}
			_ = emitter.EmitStatus(
				executeStatusBlockID,
				executeStageName,
				"executing",
				fmt.Sprintf("执行校验：action=%s 缺少 goal_check，已请求模型重试。", decision.Action),
				false,
			)
			newagentshared.AppendLLMCorrectionWithHint(
				conversationContext,
				"",
				fmt.Sprintf("你输出了 action=%s，但 goal_check 为空。", decision.Action),
				fmt.Sprintf("输出 %s 时，必须在 goal_check 中对照 done_when 逐条说明完成依据。", decision.Action),
			)
			return nil
		}
	}

	askUserHistoryAppended := false
	if strings.TrimSpace(decision.Speak) != "" {
		isConfirmWithCard := decision.Action == newagentmodel.ExecuteActionConfirm && !input.AlwaysExecute
		isAskUser := decision.Action == newagentmodel.ExecuteActionAskUser
		isAbort := decision.Action == newagentmodel.ExecuteActionAbort

		if !isConfirmWithCard && !isAskUser && !isAbort {
			msg := schema.AssistantMessage(decision.Speak, nil)
			newagentshared.PersistVisibleAssistantMessage(ctx, input.PersistVisibleMessage, flowState, msg)
		}
		if !isAbort {
			conversationContext.AppendHistory(&schema.Message{
				Role:    schema.Assistant,
				Content: decision.Speak,
			})
			if isAskUser {
				askUserHistoryAppended = true
			}
		}
	}

	switch decision.Action {
	case newagentmodel.ExecuteActionContinue:
		if decision.ToolCall != nil {
			if input.ToolRegistry != nil && input.ToolRegistry.IsWriteTool(decision.ToolCall.Name) {
				flowState.ConsecutiveCorrections++
				log.Printf(
					"[WARN] execute 决策协议违背 chat=%s round=%d action=continue tool=%s consecutive=%d/%d",
					flowState.ConversationID,
					flowState.RoundUsed,
					strings.TrimSpace(decision.ToolCall.Name),
					flowState.ConsecutiveCorrections,
					maxConsecutiveCorrections,
				)
				if flowState.ConsecutiveCorrections >= maxConsecutiveCorrections {
					return fmt.Errorf("连续 %d 次输出 continue+写工具，终止执行", flowState.ConsecutiveCorrections)
				}
				_ = emitter.EmitStatus(
					executeStatusBlockID,
					executeStageName,
					"executing",
					fmt.Sprintf(
						"执行校验：写工具 %q 未执行。原因：模型输出了 action=continue；所有写工具都必须使用 action=confirm。",
						strings.TrimSpace(decision.ToolCall.Name),
					),
					false,
				)
				llmOutput := decision.Speak
				if strings.TrimSpace(llmOutput) == "" {
					llmOutput = decision.Reason
				}
				newagentshared.AppendLLMCorrectionWithHint(
					conversationContext,
					llmOutput,
					fmt.Sprintf("你输出了 action=continue，但同时提供了 %q 这个写工具。", decision.ToolCall.Name),
					"所有写工具都必须使用 action=confirm，并放在同一个 tool_call 中；continue 仅用于读工具。如果写操作尚未执行，请直接回发 confirm。",
				)
				return nil
			}
			if shouldForceFeasibilityNegotiation(flowState, input.ToolRegistry, decision.ToolCall.Name) {
				runtimeState.OpenAskUserInteraction(
					uuid.NewString(),
					buildInfeasibleNegotiationQuestion(flowState),
					strings.TrimSpace(input.ResumeNode),
				)
				return nil
			}
			return executeToolCall(
				ctx,
				flowState,
				conversationContext,
				decision.ToolCall,
				emitter,
				input.ToolRegistry,
				input.ScheduleState,
				input.WriteSchedulePreview,
			)
		}
		if strings.TrimSpace(decision.Speak) == "" && strings.TrimSpace(decision.Reason) != "" {
			conversationContext.AppendHistory(&schema.Message{
				Role:    schema.Assistant,
				Content: decision.Reason,
			})
		}
		return nil

	case newagentmodel.ExecuteActionAskUser:
		question := resolveExecuteAskUserText(decision)
		runtimeState.OpenAskUserInteraction(uuid.NewString(), question, strings.TrimSpace(input.ResumeNode))
		runtimeState.SetPendingInteractionMetadata(newagentmodel.PendingMetaAskUserSpeakStreamed, output.speakStreamed)
		runtimeState.SetPendingInteractionMetadata(newagentmodel.PendingMetaAskUserHistoryAppended, askUserHistoryAppended)
		return nil

	case newagentmodel.ExecuteActionConfirm:
		if decision.ToolCall != nil && shouldForceFeasibilityNegotiation(flowState, input.ToolRegistry, decision.ToolCall.Name) {
			runtimeState.OpenAskUserInteraction(
				uuid.NewString(),
				buildInfeasibleNegotiationQuestion(flowState),
				strings.TrimSpace(input.ResumeNode),
			)
			return nil
		}
		if input.AlwaysExecute && decision.ToolCall != nil {
			return executeToolCall(
				ctx,
				flowState,
				conversationContext,
				decision.ToolCall,
				emitter,
				input.ToolRegistry,
				input.ScheduleState,
				input.WriteSchedulePreview,
			)
		}
		return handleExecuteActionConfirm(decision, runtimeState, flowState)

	case newagentmodel.ExecuteActionNextPlan:
		if !flowState.AdvanceStep() {
			flowState.Done()
		}
		appendExecuteStepAdvancedMarker(conversationContext)
		syncExecutePinnedContext(conversationContext, flowState)
		return nil

	case newagentmodel.ExecuteActionDone:
		flowState.Done()
		return nil

	case newagentmodel.ExecuteActionAbort:
		return handleExecuteActionAbort(decision, flowState)

	default:
		llmOutput := decision.Speak
		if strings.TrimSpace(llmOutput) == "" {
			llmOutput = decision.Reason
		}
		newagentshared.AppendLLMCorrectionWithHint(
			conversationContext,
			llmOutput,
			fmt.Sprintf("你输出的 action %q 不是合法的执行动作。", decision.Action),
			"合法的 action 包括：continue（继续当前步骤）、ask_user（追问用户）、confirm（写操作确认）、next_plan（推进到下一步）、done（任务完成）、abort（正式终止本轮流程）。",
		)
		return nil
	}
}
