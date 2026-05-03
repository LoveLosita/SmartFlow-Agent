package worker

import (
	"context"
	"fmt"

	memorymodel "github.com/LoveLosita/smartflow/backend/memory/model"
	memoryrepo "github.com/LoveLosita/smartflow/backend/memory/repo"
	memoryutils "github.com/LoveLosita/smartflow/backend/memory/utils"
	"github.com/LoveLosita/smartflow/backend/model"
	ragservice "github.com/LoveLosita/smartflow/backend/services/rag"
	"gorm.io/gorm"
)

// DecisionFlowOutcome 是一轮决策流程的汇总结果。
//
// 说明：
// 1. AddCount/UpdateCount/DeleteCount/NoneCount 分别统计四种动作的执行次数；
// 2. ItemsToSync 收集所有需要向量同步的 item（ADD 和 UPDATE 产出的）；
// 3. VectorDeletes 收集所有需要从向量库删除的 memory_id（DELETE 动作产出的）。
type DecisionFlowOutcome struct {
	AddCount      int
	UpdateCount   int
	DeleteCount   int
	NoneCount     int
	ItemsToSync   []model.MemoryItem // 需要向量同步的新增/更新 item
	VectorDeletes []int64            // 需要从向量库删除的 memory_id 列表
}

// factDecisionResult 是单条 fact 的决策执行结果，支持一对多动作。
// 原因：conflict 场景下会产生 DELETE + ADD 两个动作，需要打包返回。
type factDecisionResult struct {
	Outcomes []*ApplyActionOutcome
}

type candidateRecallResult struct {
	Items        []memorymodel.CandidateSnapshot
	FallbackMode string
}

// executeDecisionFlow 在 worker 内编排"召回→逐对比对→汇总→执行"全流程。
//
// 职责边界：
// 1. 对每条 fact 独立执行完整决策流程，fact 之间互不影响；
// 2. 所有数据库写操作在同一个事务内完成，保证原子性；
// 3. 向量同步在事务外异步执行，不影响事务提交。
//
// 降级策略：
// 1. Milvus 不可用时，回退到 MySQL 按类型查最近 N 条活跃记忆；
// 2. 单条 LLM 比对失败不影响其他候选，视为 unrelated；
// 3. 整体流程报错时，由上层根据 FallbackMode 决定是否退回旧路径。
func (r *Runner) executeDecisionFlow(
	ctx context.Context,
	job *model.MemoryJob,
	payload memorymodel.ExtractJobPayload,
	facts []memorymodel.NormalizedFact,
) (*DecisionFlowOutcome, error) {
	outcome := &DecisionFlowOutcome{
		ItemsToSync:   make([]model.MemoryItem, 0, len(facts)),
		VectorDeletes: make([]int64, 0),
	}

	// 1. 所有数据库写操作在同一个事务内完成。
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		itemRepo := r.itemRepo.WithTx(tx)
		auditRepo := r.auditRepo.WithTx(tx)
		jobRepo := r.jobRepo.WithTx(tx)

		for _, fact := range facts {
			// 2. 对每条 fact 执行完整决策流程。
			result, err := r.executeDecisionForFact(ctx, itemRepo, auditRepo, fact, job, payload)
			if err != nil {
				// 单条 fact 决策失败不影响其他 fact，记录日志后继续。
				if r.logger != nil {
					r.logger.Printf("[WARN][去重] 单条 fact 决策失败，跳过继续: job_id=%d user_id=%d memory_type=%s hash=%s err=%v", job.ID, payload.UserID, fact.MemoryType, fact.ContentHash, err)
				}
				continue
			}

			// 3. 汇总结果到全局 outcome。
			for _, actionOutcome := range result.Outcomes {
				r.collectActionOutcome(outcome, actionOutcome)
			}
		}

		// 4. 事务内最后确认 job 成功。
		return jobRepo.MarkSuccess(ctx, job.ID)
	})

	if err != nil {
		return nil, err
	}

	return outcome, nil
}

// executeDecisionForFact 对单条 fact 执行完整决策流程。
//
// 步骤：
// 1. Hash 精确命中检查 — 已有完全相同内容则直接跳过；
// 2. Milvus 语义召回 — 从旧记忆中筛出 TopK 候选（含降级）；
// 3. 逐对 LLM 比对 — 每次拿一条新 fact 和一条旧候选比对；
// 4. 确定性汇总 — 根据 LLM 比对结果确定 ADD/UPDATE/DELETE/NONE；
// 5. 校验 + 执行 — 落为数据库动作 + 审计日志。
func (r *Runner) executeDecisionForFact(
	ctx context.Context,
	itemRepo *memoryrepo.ItemRepo,
	auditRepo *memoryrepo.AuditRepo,
	fact memorymodel.NormalizedFact,
	job *model.MemoryJob,
	payload memorymodel.ExtractJobPayload,
) (*factDecisionResult, error) {
	result := &factDecisionResult{}

	// Step 1: Hash 精确命中检查。
	// 原因：如果已有完全相同内容的记忆，直接跳过，无需调 LLM。
	existing, err := itemRepo.FindActiveByHash(ctx, payload.UserID, fact.ContentHash)
	if err != nil {
		if r.logger != nil {
			r.logger.Printf("[WARN][去重] Hash 精确匹配查询失败: user_id=%d memory_type=%s hash=%s err=%v", payload.UserID, fact.MemoryType, fact.ContentHash, err)
		}
	}
	if len(existing) > 0 {
		r.recordDecisionObservation(ctx, job, payload, fact, 0, memorymodel.DecisionActionNone, "hash_exact", true, nil)
		result.Outcomes = append(result.Outcomes, &ApplyActionOutcome{
			Action:    memorymodel.DecisionActionNone,
			NeedsSync: false,
		})
		return result, nil
	}

	// Step 2: Milvus 语义召回（含降级）。
	recallResult := r.recallCandidates(ctx, payload, fact)
	candidates := recallResult.Items

	// 打印召回候选详情，便于排查向量召回和阈值过滤效果。
	if r.logger != nil {
		r.logger.Printf("[DEBUG][去重] 语义召回候选: job_id=%d user_id=%d memory_type=%s candidate_count=%d",
			job.ID, payload.UserID, fact.MemoryType, len(candidates))
		for _, c := range candidates {
			r.logger.Printf("[DEBUG][去重] 候选详情: memory_id=%d score=%.4f content=\"%s\"",
				c.MemoryID, c.Score, truncateRunes(c.Content, 50))
		}
	}

	// Step 3: 逐对 LLM 比对。
	comparisons := r.compareWithCandidates(ctx, fact, candidates)

	// Step 4: 确定性汇总。
	decision := memoryutils.AggregateComparisons(fact, comparisons, candidates)

	// 打印汇总决策结果，便于排查去重终态。
	if r.logger != nil {
		r.logger.Printf("[DEBUG][去重] 汇总决策: job_id=%d action=%s target_id=%d reason=\"%s\"",
			job.ID, decision.Action, decision.TargetID, decision.Reason)
	}

	// Step 5: 校验 + 执行。
	actionOutcome, err := ApplyFinalDecision(ctx, itemRepo, auditRepo, *decision, fact, job, payload)
	if err != nil {
		r.recordDecisionObservation(ctx, job, payload, fact, len(candidates), decision.Action, recallResult.FallbackMode, false, err)
		return nil, fmt.Errorf("执行决策动作失败: %w", err)
	}
	result.Outcomes = append(result.Outcomes, actionOutcome)
	r.recordDecisionObservation(ctx, job, payload, fact, len(candidates), decision.Action, recallResult.FallbackMode, true, nil)

	// Step 6: conflict (DELETE) 后需要补一个 ADD 写入新 fact。
	// 原因：旧记忆矛盾需删除，但新事实本身仍然有效，必须写入。
	if decision.Action == memorymodel.DecisionActionDelete {
		addDecision := memorymodel.FinalDecision{
			Action: memorymodel.DecisionActionAdd,
			Reason: "冲突旧记忆已删除，写入新事实",
		}
		addOutcome, addErr := ApplyFinalDecision(ctx, itemRepo, auditRepo, addDecision, fact, job, payload)
		if addErr != nil {
			if r.logger != nil {
				r.logger.Printf("[WARN] 冲突后补增失败: memory_type=%s err=%v", fact.MemoryType, addErr)
			}
		} else if addOutcome != nil {
			result.Outcomes = append(result.Outcomes, addOutcome)
		}
	}

	return result, nil
}

// recallCandidates 从旧记忆中召回候选，先尝试 Milvus，降级时用 MySQL。
func (r *Runner) recallCandidates(
	ctx context.Context,
	payload memorymodel.ExtractJobPayload,
	fact memorymodel.NormalizedFact,
) candidateRecallResult {
	// 1. 优先使用 Milvus 向量语义召回。
	if r.ragRuntime != nil {
		retrieveResult, err := r.ragRuntime.RetrieveMemory(ctx, ragservice.MemoryRetrieveRequest{
			Query:       fact.Content,
			TopK:        r.cfg.DecisionCandidateTopK,
			Threshold:   r.cfg.DecisionCandidateMinScore,
			UserID:      payload.UserID,
			MemoryTypes: []string{fact.MemoryType},
			Action:      "search",
		})
		if err == nil && len(retrieveResult.Items) > 0 {
			candidates := r.buildCandidatesFromRAG(retrieveResult.Items)
			if len(candidates) > 0 {
				return candidateRecallResult{
					Items:        candidates,
					FallbackMode: "rag",
				}
			}
			// RAG 返回了结果但 DocumentID 全部解析失败，降级到 MySQL。
			if r.logger != nil {
				r.logger.Printf("[WARN][去重] Milvus 返回 %d 条结果但 DocumentID 全部解析失败，降级到 MySQL: user_id=%d memory_type=%s", len(retrieveResult.Items), payload.UserID, fact.MemoryType)
			}
		}
		if err != nil && r.logger != nil {
			r.logger.Printf("[WARN][去重] Milvus 语义召回失败，降级到 MySQL: user_id=%d memory_type=%s topk=%d err=%v", payload.UserID, fact.MemoryType, r.cfg.DecisionCandidateTopK, err)
		}
		return candidateRecallResult{
			Items:        r.recallCandidatesFromMySQL(ctx, payload, fact),
			FallbackMode: "rag_to_mysql",
		}
	}

	// 2. 降级：按 user_id + memory_type + status=active 查最近 N 条。
	return candidateRecallResult{
		Items:        r.recallCandidatesFromMySQL(ctx, payload, fact),
		FallbackMode: "mysql_only",
	}
}

// buildCandidatesFromRAG 从 RAG 检索结果构建候选快照列表。
//
// 步骤：
// 1. 从 DocumentID（格式 memory:{id}）解析出 mysql_id；
// 2. 从 metadata 提取 title 和 memory_type；
// 3. 跳过无法解析 DocumentID 的结果。
func (r *Runner) buildCandidatesFromRAG(hits []ragservice.RetrieveHit) []memorymodel.CandidateSnapshot {
	candidates := make([]memorymodel.CandidateSnapshot, 0, len(hits))
	for _, hit := range hits {
		memoryID := parseMemoryID(hit.DocumentID)
		if memoryID <= 0 {
			if r.logger != nil {
				r.logger.Printf("[WARN][去重] DocumentID 解析失败，跳过候选: document_id=%q", hit.DocumentID)
			}
			continue
		}

		candidates = append(candidates, memorymodel.CandidateSnapshot{
			MemoryID:   memoryID,
			Title:      asStringFromMap(hit.Metadata, "title"),
			Content:    hit.Text,
			MemoryType: asStringFromMap(hit.Metadata, "memory_type"),
			Score:      hit.Score,
		})
	}
	return candidates
}

// recallCandidatesFromMySQL 从 MySQL 查最近 N 条活跃记忆作为候选。
// 这是 Milvus 不可用时的降级方案。
func (r *Runner) recallCandidatesFromMySQL(
	ctx context.Context,
	payload memorymodel.ExtractJobPayload,
	fact memorymodel.NormalizedFact,
) []memorymodel.CandidateSnapshot {
	items, err := r.itemRepo.FindByQuery(ctx, memorymodel.ItemQuery{
		UserID:      payload.UserID,
		MemoryTypes: []string{fact.MemoryType},
		Statuses:    []string{model.MemoryItemStatusActive},
		Limit:       r.cfg.DecisionCandidateTopK,
	})
	if err != nil {
		if r.logger != nil {
			r.logger.Printf("[WARN] MySQL 降级召回失败: err=%v", err)
		}
		return nil
	}

	candidates := make([]memorymodel.CandidateSnapshot, 0, len(items))
	for _, item := range items {
		candidates = append(candidates, memorymodel.CandidateSnapshot{
			MemoryID:   item.ID,
			Title:      item.Title,
			Content:    item.Content,
			MemoryType: item.MemoryType,
			Score:      0, // MySQL 降级无向量分数
		})
	}
	return candidates
}

// compareWithCandidates 对每个候选逐一调 LLM 做关系判断。
//
// 说明：
// 1. LLM 调用失败时视为 unrelated，不影响其他候选的比对；
// 2. 对比对结果做校验，不合法的也视为 unrelated；
// 3. 无候选或决策编排器为空时返回空切片，上层直接走 ADD 路径。
func (r *Runner) compareWithCandidates(
	ctx context.Context,
	fact memorymodel.NormalizedFact,
	candidates []memorymodel.CandidateSnapshot,
) []memorymodel.ComparisonResult {
	if r.decisionOrchestrator == nil || len(candidates) == 0 {
		return nil
	}

	comparisons := make([]memorymodel.ComparisonResult, 0, len(candidates))
	for _, candidate := range candidates {
		compResult, err := r.decisionOrchestrator.Compare(ctx, fact, candidate)
		if err != nil {
			// LLM 调用失败 → 视为 unrelated，不影响其他候选。
			if r.logger != nil {
				r.logger.Printf("[WARN][去重] LLM 逐对比较调用失败，视为 unrelated: candidate_id=%d memory_type=%s err=%v", candidate.MemoryID, fact.MemoryType, err)
			}
			continue
		}

		// 校验 LLM 输出合法性，不合法也跳过。
		if validateErr := memoryutils.ValidateComparisonResult(compResult); validateErr != nil {
			if r.logger != nil {
				r.logger.Printf("[WARN][去重] LLM 比对结果校验不通过，视为 unrelated: candidate_id=%d memory_type=%s relation=%s err=%v", candidate.MemoryID, fact.MemoryType, compResult.Relation, validateErr)
			}
			continue
		}

		comparisons = append(comparisons, *compResult)

		// 打印 LLM 比对结果，便于排查误判。
		if r.logger != nil {
			r.logger.Printf("[DEBUG][去重] LLM 比对结果: candidate_id=%d score=%.4f relation=%s reason=\"%s\" candidate_content=\"%s\"",
				candidate.MemoryID, candidate.Score, compResult.Relation, compResult.Reason, truncateRunes(candidate.Content, 50))
		}
	}
	return comparisons
}

// collectActionOutcome 汇总单个动作结果到全局 outcome。
func (r *Runner) collectActionOutcome(outcome *DecisionFlowOutcome, actionOutcome *ApplyActionOutcome) {
	if actionOutcome == nil {
		return
	}

	switch actionOutcome.Action {
	case memorymodel.DecisionActionAdd:
		outcome.AddCount++
		if actionOutcome.NeedsSync && actionOutcome.NewItem != nil {
			outcome.ItemsToSync = append(outcome.ItemsToSync, *actionOutcome.NewItem)
		}
	case memorymodel.DecisionActionUpdate:
		outcome.UpdateCount++
		if actionOutcome.NeedsSync && actionOutcome.NewItem != nil {
			outcome.ItemsToSync = append(outcome.ItemsToSync, *actionOutcome.NewItem)
		}
	case memorymodel.DecisionActionDelete:
		outcome.DeleteCount++
		outcome.VectorDeletes = append(outcome.VectorDeletes, actionOutcome.MemoryID)
	case memorymodel.DecisionActionNone:
		outcome.NoneCount++
	}
}

// asStringFromMap 从 metadata map 中安全提取字符串值。
func asStringFromMap(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	v, ok := m[key]
	if !ok || v == nil {
		return ""
	}
	return fmt.Sprintf("%v", v)
}

// truncateRunes 截取字符串前 n 个 rune，超出则追加 "..."。
// 用途：日志内容预览，避免超长内容撑爆单行日志。
func truncateRunes(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	if n <= 0 {
		return ""
	}
	return string(runes[:n]) + "..."
}
