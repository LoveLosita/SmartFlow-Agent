package sv

import (
	"context"
	"fmt"
	"strings"
	"time"

	storemodel "github.com/LoveLosita/smartflow/backend/services/tokenstore/model"
	creditcontracts "github.com/LoveLosita/smartflow/backend/shared/contracts/creditstore"
	sharedevents "github.com/LoveLosita/smartflow/backend/shared/events"
)

type creditChargeMetadata struct {
	Scene           string    `json:"scene"`
	RequestID       string    `json:"request_id"`
	ConversationID  string    `json:"conversation_id"`
	ModelAlias      string    `json:"model_alias"`
	ProviderName    string    `json:"provider_name"`
	ModelName       string    `json:"model_name"`
	InputTokens     int64     `json:"input_tokens"`
	OutputTokens    int64     `json:"output_tokens"`
	CachedTokens    int64     `json:"cached_tokens"`
	ReasoningTokens int64     `json:"reasoning_tokens"`
	TotalTokens     int64     `json:"total_tokens"`
	RMBCostMicros   int64     `json:"rmb_cost_micros"`
	CreditCost      int64     `json:"credit_cost"`
	SkipCharge      bool      `json:"skip_charge"`
	TriggeredAt     time.Time `json:"triggered_at"`
}

// RecordCreditCharge 负责把 LLM 扣费事件写入 Credit 权威账本。
func (s *Service) RecordCreditCharge(ctx context.Context, payload sharedevents.CreditChargeRequestedPayload) (*creditcontracts.CreditTransactionView, error) {
	if err := s.Ready(); err != nil {
		return nil, err
	}
	if err := payload.Validate(); err != nil {
		return nil, err
	}

	sourceRefID := strings.TrimSpace(payload.RequestID)
	if sourceRefID == "" {
		sourceRefID = strings.TrimSpace(payload.ConversationID)
	}
	var sourceRefIDPtr *string
	if sourceRefID != "" {
		sourceRefIDPtr = &sourceRefID
	}

	amount := -payload.CreditCost
	status := storemodel.CreditLedgerStatusApplied
	if payload.SkipCharge {
		amount = 0
		status = storemodel.CreditLedgerStatusSkipped
	}

	ledger, _, err := s.applyCreditLedger(ctx, applyCreditLedgerRequest{
		EventID:      strings.TrimSpace(payload.EventID),
		UserID:       payload.UserID,
		Source:       storemodel.CreditLedgerSourceCharge,
		SourceLabel:  creditSourceLabel(storemodel.CreditLedgerSourceCharge, ""),
		Direction:    storemodel.CreditLedgerDirectionExpense,
		SourceRefID:  sourceRefIDPtr,
		Amount:       amount,
		Status:       status,
		Description:  creditChargeDescription(payload),
		MetadataJSON: creditMetadataJSON(creditChargeMetadataFromPayload(payload)),
		CreatedAt:    payload.TriggeredAt,
	})
	if err != nil {
		return nil, err
	}

	view := creditTransactionViewFromModel(*ledger)
	return &view, nil
}

func creditChargeMetadataFromPayload(payload sharedevents.CreditChargeRequestedPayload) creditChargeMetadata {
	return creditChargeMetadata{
		Scene:           payload.Scene,
		RequestID:       payload.RequestID,
		ConversationID:  payload.ConversationID,
		ModelAlias:      payload.ModelAlias,
		ProviderName:    payload.ProviderName,
		ModelName:       payload.ModelName,
		InputTokens:     payload.InputTokens,
		OutputTokens:    payload.OutputTokens,
		CachedTokens:    payload.CachedTokens,
		ReasoningTokens: payload.ReasoningTokens,
		TotalTokens:     payload.TotalTokens,
		RMBCostMicros:   payload.RMBCostMicros,
		CreditCost:      payload.CreditCost,
		SkipCharge:      payload.SkipCharge,
		TriggeredAt:     payload.TriggeredAt,
	}
}

func creditChargeDescription(payload sharedevents.CreditChargeRequestedPayload) string {
	modelText := strings.TrimSpace(payload.ModelAlias)
	if modelText == "" {
		modelText = strings.TrimSpace(payload.ModelName)
	}
	sceneText := strings.TrimSpace(payload.Scene)
	switch {
	case sceneText != "" && modelText != "":
		return fmt.Sprintf("AI 调用扣费（%s / %s）", sceneText, modelText)
	case modelText != "":
		return fmt.Sprintf("AI 调用扣费（%s）", modelText)
	default:
		return "AI 调用扣费"
	}
}
