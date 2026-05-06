package sv

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	tokenstoredao "github.com/LoveLosita/smartflow/backend/services/tokenstore/dao"
	storemodel "github.com/LoveLosita/smartflow/backend/services/tokenstore/model"
	creditcontracts "github.com/LoveLosita/smartflow/backend/shared/contracts/creditstore"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

const (
	creditSnapshotSourceCache = "cache"
	creditSnapshotSourceDB    = "db"
)

type creditProductSnapshot struct {
	ProductID         uint64 `json:"product_id"`
	SKU               string `json:"sku"`
	Name              string `json:"name"`
	Description       string `json:"description"`
	CreditAmount      int64  `json:"credit_amount"`
	PriceCent         int64  `json:"price_cent"`
	OriginalPriceCent int64  `json:"original_price_cent"`
	PriceText         string `json:"price_text"`
	Currency          string `json:"currency"`
	Badge             string `json:"badge"`
	Status            string `json:"status"`
	SortOrder         int    `json:"sort_order"`
}

type applyCreditLedgerRequest struct {
	EventID      string
	UserID       uint64
	Source       string
	SourceLabel  string
	Direction    string
	OrderID      *uint64
	SourceRefID  *string
	Amount       int64
	Status       string
	Description  string
	MetadataJSON string
	CreatedAt    time.Time
}

func creditPageResult(page int, pageSize int, total int64) creditcontracts.PageResult {
	return creditcontracts.PageResult{
		Page:     page,
		PageSize: pageSize,
		Total:    int(total),
		HasMore:  int64(page*pageSize) < total,
	}
}

func creditProductViewFromModel(product storemodel.CreditProduct) creditcontracts.CreditProductView {
	originalPriceCent := normalizeOriginalPriceCent(product.OriginalPriceCent, product.PriceCent)
	return creditcontracts.CreditProductView{
		ProductID:         product.ID,
		Name:              product.Name,
		Description:       product.Description,
		CreditAmount:      product.CreditAmount,
		PriceCent:         product.PriceCent,
		OriginalPriceCent: originalPriceCent,
		PriceText:         formatPriceText(product.Currency, product.PriceCent),
		Currency:          product.Currency,
		Badge:             product.Badge,
		Status:            product.Status,
		SortOrder:         product.SortOrder,
	}
}

func creditOrderViewFromModel(order storemodel.CreditOrder) creditcontracts.CreditOrderView {
	return creditcontracts.CreditOrderView{
		OrderID:         order.ID,
		OrderNo:         order.OrderNo,
		Status:          order.Status,
		ProductSnapshot: order.ProductSnapshotJSON,
		ProductName:     order.ProductName,
		Quantity:        order.Quantity,
		CreditAmount:    order.CreditAmount,
		AmountCent:      order.AmountCent,
		PriceText:       formatPriceText(order.Currency, order.AmountCent),
		Currency:        order.Currency,
		PaymentMode:     order.PaymentMode,
		CreatedAt:       formatTime(order.CreatedAt),
		PaidAt:          formatTimePtr(order.PaidAt),
		CreditedAt:      formatTimePtr(order.CreditedAt),
	}
}

func creditTransactionViewFromModel(ledger storemodel.CreditLedger) creditcontracts.CreditTransactionView {
	return creditcontracts.CreditTransactionView{
		TransactionID: ledger.ID,
		EventID:       ledger.EventID,
		Source:        ledger.Source,
		SourceLabel:   creditSourceLabel(ledger.Source, ledger.SourceLabel),
		Direction:     ledger.Direction,
		Amount:        ledger.Amount,
		BalanceAfter:  ledger.BalanceAfter,
		Status:        ledger.Status,
		Description:   ledger.Description,
		MetadataJSON:  ledger.MetadataJSON,
		CreatedAt:     formatTime(ledger.CreatedAt),
		OrderID:       ledger.OrderID,
	}
}

func creditPriceRuleViewFromModel(rule storemodel.CreditPriceRule) creditcontracts.CreditPriceRuleView {
	chargePrices := creditcontracts.DeriveChargePriceMicrosSet(
		rule.InputPriceMicros,
		rule.OutputPriceMicros,
		rule.CachedPriceMicros,
		rule.ReasoningPriceMicros,
		rule.ProfitRateBps,
	)
	return creditcontracts.CreditPriceRuleView{
		RuleID:                     rule.ID,
		Scene:                      rule.Scene,
		ProviderName:               rule.ProviderName,
		ModelName:                  rule.ModelName,
		InputPriceMicros:           rule.InputPriceMicros,
		OutputPriceMicros:          rule.OutputPriceMicros,
		CachedPriceMicros:          rule.CachedPriceMicros,
		ReasoningPriceMicros:       rule.ReasoningPriceMicros,
		CreditPerYuan:              rule.CreditPerYuan,
		ProfitRateBps:              rule.ProfitRateBps,
		ChargeInputPriceMicros:     chargePrices.InputChargePriceMicros,
		ChargeOutputPriceMicros:    chargePrices.OutputChargePriceMicros,
		ChargeCachedPriceMicros:    chargePrices.CachedChargePriceMicros,
		ChargeReasoningPriceMicros: chargePrices.ReasoningChargePriceMicros,
		Status:                     rule.Status,
		Priority:                   rule.Priority,
		Description:                rule.Description,
	}
}

func creditRewardRuleViewFromModel(rule storemodel.CreditRewardRule) creditcontracts.CreditRewardRuleView {
	return creditcontracts.CreditRewardRuleView{
		RuleID:      rule.ID,
		Source:      rule.Source,
		Name:        rule.Name,
		Amount:      rule.Amount,
		Status:      rule.Status,
		Description: rule.Description,
	}
}

func buildCreditProductSnapshot(product storemodel.CreditProduct) (string, error) {
	originalPriceCent := normalizeOriginalPriceCent(product.OriginalPriceCent, product.PriceCent)
	snapshot := creditProductSnapshot{
		ProductID:         product.ID,
		SKU:               product.SKU,
		Name:              product.Name,
		Description:       product.Description,
		CreditAmount:      product.CreditAmount,
		PriceCent:         product.PriceCent,
		OriginalPriceCent: originalPriceCent,
		PriceText:         formatPriceText(product.Currency, product.PriceCent),
		Currency:          product.Currency,
		Badge:             product.Badge,
		Status:            product.Status,
		SortOrder:         product.SortOrder,
	}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func normalizeOriginalPriceCent(originalPriceCent int64, priceCent int64) int64 {
	if originalPriceCent > 0 {
		return originalPriceCent
	}
	return priceCent
}

func newCreditOrderNo() string {
	return fmt.Sprintf(
		"CS%s%s",
		time.Now().Format("20060102150405"),
		strings.ReplaceAll(uuid.NewString(), "-", ""),
	)
}

func creditOrderLedgerEventID(orderID uint64) string {
	return fmt.Sprintf("credit-order:%d:paid", orderID)
}

func creditSourceLabel(source string, fallback string) string {
	if strings.TrimSpace(fallback) != "" {
		return fallback
	}
	switch strings.TrimSpace(source) {
	case storemodel.CreditLedgerSourcePurchase:
		return "购买充值"
	case storemodel.CreditLedgerSourceCharge:
		return "AI 调用扣费"
	case storemodel.CreditLedgerSourceForumLike:
		return "计划被点赞"
	case storemodel.CreditLedgerSourceForumImport:
		return "计划被导入"
	case storemodel.CreditLedgerSourceManual:
		return "人工补发"
	default:
		return "Credit 流水"
	}
}

func creditDirectionFromAmount(amount int64) string {
	if amount < 0 {
		return storemodel.CreditLedgerDirectionExpense
	}
	return storemodel.CreditLedgerDirectionIncome
}

func creditShouldAffectBalance(req applyCreditLedgerRequest) bool {
	return strings.TrimSpace(req.Status) == storemodel.CreditLedgerStatusApplied && req.Amount != 0
}

func (s *Service) applyCreditLedger(ctx context.Context, req applyCreditLedgerRequest) (*storemodel.CreditLedger, *storemodel.CreditAccount, error) {
	if err := s.Ready(); err != nil {
		return nil, nil, err
	}
	normalized, err := normalizeApplyCreditLedgerRequest(req)
	if err != nil {
		return nil, nil, err
	}

	var resultLedger *storemodel.CreditLedger
	var resultAccount *storemodel.CreditAccount
	err = s.creditDAO.Transaction(ctx, func(txDAO *tokenstoredao.CreditStoreDAO) error {
		ledger, account, err := s.applyCreditLedgerWithDAO(ctx, txDAO, normalized)
		if err != nil {
			return err
		}
		resultLedger = ledger
		resultAccount = account
		return nil
	})
	if err != nil {
		return nil, nil, err
	}

	s.syncCreditCacheBestEffort(ctx, normalized.UserID, resultAccount, resultLedger)
	return resultLedger, resultAccount, nil
}

func (s *Service) applyCreditLedgerWithDAO(ctx context.Context, txDAO *tokenstoredao.CreditStoreDAO, req applyCreditLedgerRequest) (*storemodel.CreditLedger, *storemodel.CreditAccount, error) {
	if txDAO == nil {
		return nil, nil, errors.New("credit dao is nil")
	}

	existing, findErr := txDAO.FindLedgerByEventID(ctx, req.EventID)
	if findErr != nil {
		return nil, nil, findErr
	}
	if existing != nil {
		if err := validateExistingCreditLedger(*existing, req); err != nil {
			return nil, nil, err
		}
		account, accountErr := txDAO.FindAccountByUserID(ctx, req.UserID)
		if accountErr != nil {
			return nil, nil, accountErr
		}
		return existing, account, nil
	}

	account, accountErr := txDAO.LockAccountByUserID(ctx, req.UserID)
	if accountErr != nil {
		return nil, nil, accountErr
	}
	if account == nil && creditShouldAffectBalance(req) {
		account = &storemodel.CreditAccount{
			UserID: req.UserID,
		}
		if err := txDAO.CreateAccount(ctx, account); err != nil {
			if !isDuplicateKeyError(err) {
				return nil, nil, err
			}
			account, err = txDAO.LockAccountByUserID(ctx, req.UserID)
			if err != nil {
				return nil, nil, err
			}
			if account == nil {
				return nil, nil, errors.New("credit account duplicated but not found by user_id")
			}
		}
	}

	balanceBefore := int64(0)
	if account != nil {
		balanceBefore = account.Balance
	}
	balanceAfter := balanceBefore
	if creditShouldAffectBalance(req) {
		balanceAfter += req.Amount
	}

	ledger := &storemodel.CreditLedger{
		EventID:       req.EventID,
		UserID:        req.UserID,
		Source:        req.Source,
		SourceLabel:   creditSourceLabel(req.Source, req.SourceLabel),
		Direction:     req.Direction,
		OrderID:       req.OrderID,
		SourceRefID:   req.SourceRefID,
		Amount:        req.Amount,
		BalanceBefore: balanceBefore,
		BalanceAfter:  balanceAfter,
		Status:        req.Status,
		Description:   req.Description,
		MetadataJSON:  req.MetadataJSON,
		CreatedAt:     req.CreatedAt,
	}
	if err := txDAO.CreateLedger(ctx, ledger); err != nil {
		if !isDuplicateKeyError(err) {
			return nil, nil, err
		}
		existing, findErr := txDAO.FindLedgerByEventID(ctx, req.EventID)
		if findErr != nil {
			return nil, nil, findErr
		}
		if existing != nil {
			if err := validateExistingCreditLedger(*existing, req); err != nil {
				return nil, nil, err
			}
			account, accountErr := txDAO.FindAccountByUserID(ctx, req.UserID)
			if accountErr != nil {
				return nil, nil, accountErr
			}
			return existing, account, nil
		}
		return nil, nil, errors.New("credit ledger duplicated but not found by event_id")
	}

	if creditShouldAffectBalance(req) {
		account.Balance = balanceAfter
		account.LastLedgerEventID = req.EventID
		if req.Amount > 0 {
			if req.Source == storemodel.CreditLedgerSourcePurchase {
				account.TotalRecharged += req.Amount
			} else {
				account.TotalRewarded += req.Amount
			}
		} else {
			account.TotalConsumed += -req.Amount
		}
		if err := txDAO.SaveAccount(ctx, account); err != nil {
			return nil, nil, err
		}
	}

	return ledger, account, nil
}

func normalizeApplyCreditLedgerRequest(req applyCreditLedgerRequest) (applyCreditLedgerRequest, error) {
	normalized := req
	normalized.EventID = strings.TrimSpace(req.EventID)
	normalized.Source = strings.ToLower(strings.TrimSpace(req.Source))
	normalized.SourceLabel = strings.TrimSpace(req.SourceLabel)
	normalized.Direction = strings.ToLower(strings.TrimSpace(req.Direction))
	normalized.Status = strings.ToLower(strings.TrimSpace(req.Status))
	normalized.Description = strings.TrimSpace(req.Description)
	normalized.MetadataJSON = strings.TrimSpace(req.MetadataJSON)
	if normalized.CreatedAt.IsZero() {
		normalized.CreatedAt = time.Now()
	}

	switch {
	case normalized.EventID == "":
		return applyCreditLedgerRequest{}, tokenStoreBadRequest("credit event_id 不能为空")
	case normalized.UserID == 0:
		return applyCreditLedgerRequest{}, tokenStoreBadRequest("credit user_id 不能为空")
	case normalized.Source == "":
		return applyCreditLedgerRequest{}, tokenStoreBadRequest("credit source 不能为空")
	case normalized.Direction != storemodel.CreditLedgerDirectionIncome && normalized.Direction != storemodel.CreditLedgerDirectionExpense:
		return applyCreditLedgerRequest{}, tokenStoreBadRequest("credit direction 仅支持 income 或 expense")
	case normalized.Status == "":
		return applyCreditLedgerRequest{}, tokenStoreBadRequest("credit status 不能为空")
	}
	return normalized, nil
}

func validateExistingCreditLedger(existing storemodel.CreditLedger, req applyCreditLedgerRequest) error {
	if existing.UserID != req.UserID ||
		existing.Source != req.Source ||
		existing.Direction != req.Direction ||
		existing.Amount != req.Amount ||
		existing.Status != req.Status {
		return tokenStoreBadRequest("credit event_id 幂等冲突：已存在流水与本次请求不一致")
	}
	return nil
}

func (s *Service) syncCreditCacheBestEffort(ctx context.Context, userID uint64, account *storemodel.CreditAccount, ledger *storemodel.CreditLedger) {
	if s == nil || s.creditCache == nil || userID == 0 {
		return
	}

	if account == nil {
		loaded, err := s.creditDAO.FindAccountByUserID(ctx, userID)
		if err != nil {
			log.Printf("tokenstore credit cache fallback load failed: user_id=%d err=%v", userID, err)
			return
		}
		account = loaded
	}

	snapshot := tokenstoredao.CreditBalanceSnapshot{
		UserID:         userID,
		UpdatedAt:      time.Now(),
		TotalRecharged: 0,
		TotalRewarded:  0,
		TotalConsumed:  0,
	}
	if account != nil {
		snapshot.Balance = account.Balance
		snapshot.TotalRecharged = account.TotalRecharged
		snapshot.TotalRewarded = account.TotalRewarded
		snapshot.TotalConsumed = account.TotalConsumed
		if !account.UpdatedAt.IsZero() {
			snapshot.UpdatedAt = account.UpdatedAt
		}
	} else if ledger != nil && !ledger.CreatedAt.IsZero() {
		snapshot.Balance = ledger.BalanceAfter
		snapshot.UpdatedAt = ledger.CreatedAt
	}

	if err := s.creditCache.SetCreditBalanceSnapshot(ctx, userID, snapshot, 0); err != nil {
		log.Printf("tokenstore credit cache snapshot write failed: user_id=%d err=%v", userID, err)
	}

	if snapshot.Balance <= 0 {
		if err := s.creditCache.SetUserCreditBlocked(ctx, userID, 0); err != nil {
			log.Printf("tokenstore credit blocked flag write failed: user_id=%d err=%v", userID, err)
		}
		return
	}
	if err := s.creditCache.DeleteUserCreditBlocked(ctx, userID); err != nil {
		log.Printf("tokenstore credit blocked flag delete failed: user_id=%d err=%v", userID, err)
	}
}

func creditMetadataJSON(payload any) string {
	if payload == nil {
		return ""
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return ""
	}
	return string(raw)
}

func normalizeCreditRecordNotFound(err error, fallback error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return fallback
	}
	return err
}
