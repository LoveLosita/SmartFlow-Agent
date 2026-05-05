package agentexecute

import (
	"encoding/json"
	"fmt"
	"strings"

	agentmodel "github.com/LoveLosita/smartflow/backend/services/agent/model"
	agenttools "github.com/LoveLosita/smartflow/backend/services/agent/tools"
)

func shouldForceFeasibilityNegotiation(
	flowState *agentmodel.CommonState,
	registry *agenttools.ToolRegistry,
	toolName string,
) bool {
	if flowState == nil || registry == nil {
		return false
	}
	if !flowState.HealthCheckDone || flowState.HealthIsFeasible {
		return false
	}
	if !registry.IsWriteTool(toolName) || !registry.RequiresScheduleState(toolName) {
		return false
	}
	return true
}

func buildInfeasibleNegotiationQuestion(flowState *agentmodel.CommonState) string {
	capacityGap := 0
	reasonCode := "capacity_insufficient"
	if flowState != nil {
		capacityGap = flowState.HealthCapacityGap
		if strings.TrimSpace(flowState.HealthReasonCode) != "" {
			reasonCode = strings.TrimSpace(flowState.HealthReasonCode)
		}
	}
	return fmt.Sprintf(
		"当前计划不可行：analyze_health 判断当前约束不可行（capacity_gap=%d，reason=%s）。在继续写操作前，请先与用户协商：扩展时间窗、放宽约束、缩减范围或预算，或接受风险收口。",
		capacityGap,
		reasonCode,
	)
}

func buildInfeasibleBlockedResult(flowState *agentmodel.CommonState) string {
	capacityGap := 0
	reasonCode := "capacity_insufficient"
	if flowState != nil {
		capacityGap = flowState.HealthCapacityGap
		if strings.TrimSpace(flowState.HealthReasonCode) != "" {
			reasonCode = strings.TrimSpace(flowState.HealthReasonCode)
		}
	}
	return fmt.Sprintf(
		"已阻断本次写操作：analyze_health 判定当前约束不可行（capacity_gap=%d，reason=%s）。请先与用户协商：扩展时间窗 / 放宽约束 / 缩减范围或预算 / 接受风险收口。",
		capacityGap,
		reasonCode,
	)
}

type contextToolsResultEnvelope struct {
	Tool    string   `json:"tool"`
	Success bool     `json:"success"`
	Domain  string   `json:"domain,omitempty"`
	Packs   []string `json:"packs,omitempty"`
	Mode    string   `json:"mode,omitempty"`
	All     bool     `json:"all,omitempty"`
}

type analyzeHealthResultEnvelope struct {
	Tool        string                         `json:"tool"`
	Success     bool                           `json:"success"`
	Feasibility *analyzeHealthFeasibilityBrief `json:"feasibility,omitempty"`
	Decision    *analyzeHealthDecisionBrief    `json:"decision,omitempty"`
}

type analyzeHealthFeasibilityBrief struct {
	IsFeasible  bool   `json:"is_feasible"`
	CapacityGap int    `json:"capacity_gap"`
	ReasonCode  string `json:"reason_code"`
}

type analyzeHealthDecisionBrief struct {
	ShouldContinueOptimize bool   `json:"should_continue_optimize"`
	PrimaryProblem         string `json:"primary_problem,omitempty"`
	RecommendedOperation   string `json:"recommended_operation,omitempty"`
	IsForcedImperfection   bool   `json:"is_forced_imperfection"`
	ImprovementSignal      string `json:"improvement_signal,omitempty"`
}

type upsertTaskClassResultEnvelope struct {
	Tool       string                         `json:"tool"`
	Success    bool                           `json:"success"`
	Validation *upsertTaskClassValidationPart `json:"validation,omitempty"`
	Error      string                         `json:"error,omitempty"`
	ErrorCode  string                         `json:"error_code,omitempty"`
}

type upsertTaskClassValidationPart struct {
	OK     bool     `json:"ok"`
	Issues []string `json:"issues"`
}

func updateActiveToolDomainSnapshot(flowState *agentmodel.CommonState, toolName string, result string) {
	if flowState == nil || !agenttools.IsContextManagementTool(toolName) {
		return
	}

	var envelope contextToolsResultEnvelope
	if err := json.Unmarshal([]byte(result), &envelope); err != nil {
		return
	}
	if !envelope.Success {
		return
	}

	switch strings.TrimSpace(toolName) {
	case agenttools.ToolNameContextToolsAdd:
		domain := agenttools.NormalizeToolDomain(envelope.Domain)
		if domain == "" {
			return
		}
		nextPacks := agenttools.ResolveEffectiveToolPacks(domain, envelope.Packs)
		mode := strings.ToLower(strings.TrimSpace(envelope.Mode))
		if mode == "merge" && agenttools.NormalizeToolDomain(flowState.ActiveToolDomain) == domain {
			merged := make([]string, 0, len(flowState.ActiveToolPacks)+len(nextPacks))
			seen := make(map[string]struct{}, len(flowState.ActiveToolPacks)+len(nextPacks))
			current := agenttools.ResolveEffectiveToolPacks(domain, flowState.ActiveToolPacks)
			for _, pack := range current {
				if _, exists := seen[pack]; exists {
					continue
				}
				seen[pack] = struct{}{}
				merged = append(merged, pack)
			}
			for _, pack := range nextPacks {
				if _, exists := seen[pack]; exists {
					continue
				}
				seen[pack] = struct{}{}
				merged = append(merged, pack)
			}
			nextPacks = merged
		}
		flowState.ActiveToolDomain = domain
		flowState.ActiveToolPacks = nextPacks
	case agenttools.ToolNameContextToolsRemove:
		if envelope.All {
			flowState.ActiveToolDomain = ""
			flowState.ActiveToolPacks = nil
			return
		}
		domain := agenttools.NormalizeToolDomain(envelope.Domain)
		if domain == "" {
			return
		}
		currentDomain := agenttools.NormalizeToolDomain(flowState.ActiveToolDomain)
		if currentDomain != domain {
			return
		}

		removedPacks := agenttools.NormalizeToolPacks(domain, envelope.Packs)
		if len(removedPacks) == 0 {
			flowState.ActiveToolDomain = ""
			flowState.ActiveToolPacks = nil
			return
		}

		currentEffective := agenttools.ResolveEffectiveToolPacks(domain, flowState.ActiveToolPacks)
		if len(currentEffective) == 0 {
			flowState.ActiveToolDomain = ""
			flowState.ActiveToolPacks = nil
			return
		}

		removedSet := make(map[string]struct{}, len(removedPacks))
		for _, pack := range removedPacks {
			removedSet[pack] = struct{}{}
		}
		remaining := make([]string, 0, len(currentEffective))
		for _, pack := range currentEffective {
			if _, shouldRemove := removedSet[pack]; shouldRemove {
				continue
			}
			remaining = append(remaining, pack)
		}
		if len(remaining) == 0 {
			flowState.ActiveToolDomain = ""
			flowState.ActiveToolPacks = nil
			return
		}
		flowState.ActiveToolPacks = remaining
	}
}

func updateHealthFeasibilitySnapshot(flowState *agentmodel.CommonState, toolName string, result string) {
	if flowState == nil || !strings.EqualFold(strings.TrimSpace(toolName), toolAnalyzeHealth) {
		return
	}

	flowState.HealthCheckDone = false
	flowState.HealthIsFeasible = true
	flowState.HealthCapacityGap = 0
	flowState.HealthReasonCode = ""

	var envelope analyzeHealthResultEnvelope
	if err := json.Unmarshal([]byte(result), &envelope); err != nil {
		return
	}
	if !envelope.Success || envelope.Feasibility == nil {
		return
	}

	flowState.HealthCheckDone = true
	flowState.HealthIsFeasible = envelope.Feasibility.IsFeasible
	flowState.HealthCapacityGap = envelope.Feasibility.CapacityGap
	flowState.HealthReasonCode = strings.TrimSpace(envelope.Feasibility.ReasonCode)
}

func updateTaskClassUpsertSnapshot(flowState *agentmodel.CommonState, toolName string, result string) {
	if flowState == nil || !strings.EqualFold(strings.TrimSpace(toolName), "upsert_task_class") {
		return
	}

	flowState.TaskClassUpsertLastTried = true
	flowState.TaskClassUpsertLastSuccess = false
	flowState.TaskClassUpsertLastIssues = nil

	var envelope upsertTaskClassResultEnvelope
	if err := json.Unmarshal([]byte(result), &envelope); err != nil {
		flowState.TaskClassUpsertConsecutiveFailures++
		return
	}

	success := envelope.Success
	issues := make([]string, 0)
	if envelope.Validation != nil {
		issues = append(issues, parseAnyToStringSlice(any(envelope.Validation.Issues))...)
		if !envelope.Validation.OK {
			success = false
		}
	}
	if !success && strings.TrimSpace(envelope.Error) != "" && len(issues) == 0 {
		issues = append(issues, strings.TrimSpace(envelope.Error))
	}
	issues = uniqueNonEmptyStrings(issues)

	flowState.TaskClassUpsertLastSuccess = success
	flowState.TaskClassUpsertLastIssues = issues
	if success {
		flowState.TaskClassUpsertConsecutiveFailures = 0
		return
	}
	flowState.TaskClassUpsertConsecutiveFailures++
}

func uniqueNonEmptyStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		text := strings.TrimSpace(value)
		if text == "" {
			continue
		}
		if _, exists := seen[text]; exists {
			continue
		}
		seen[text] = struct{}{}
		result = append(result, text)
	}
	return result
}

func updateHealthSnapshotV2(flowState *agentmodel.CommonState, toolName string, result string) {
	if flowState == nil || !strings.EqualFold(strings.TrimSpace(toolName), toolAnalyzeHealth) {
		return
	}

	prevSignal := strings.TrimSpace(flowState.HealthImprovementSignal)
	flowState.HealthCheckDone = false
	flowState.HealthIsFeasible = true
	flowState.HealthCapacityGap = 0
	flowState.HealthReasonCode = ""
	flowState.HealthShouldContinueOptimize = false
	flowState.HealthTightnessLevel = ""
	flowState.HealthPrimaryProblem = ""
	flowState.HealthRecommendedOperation = ""
	flowState.HealthIsForcedImperfection = false
	flowState.HealthImprovementSignal = ""

	var envelope struct {
		Success     bool                           `json:"success"`
		Feasibility *analyzeHealthFeasibilityBrief `json:"feasibility,omitempty"`
		Metrics     struct {
			Tightness *struct {
				TightnessLevel string `json:"tightness_level"`
			} `json:"tightness,omitempty"`
		} `json:"metrics"`
		Decision *analyzeHealthDecisionBrief `json:"decision,omitempty"`
	}
	if err := json.Unmarshal([]byte(result), &envelope); err != nil {
		flowState.HealthStagnationCount = 0
		return
	}
	if !envelope.Success || envelope.Feasibility == nil {
		flowState.HealthStagnationCount = 0
		return
	}

	flowState.HealthCheckDone = true
	flowState.HealthIsFeasible = envelope.Feasibility.IsFeasible
	flowState.HealthCapacityGap = envelope.Feasibility.CapacityGap
	flowState.HealthReasonCode = strings.TrimSpace(envelope.Feasibility.ReasonCode)
	if envelope.Metrics.Tightness != nil {
		flowState.HealthTightnessLevel = strings.TrimSpace(envelope.Metrics.Tightness.TightnessLevel)
	}
	if envelope.Decision != nil {
		flowState.HealthShouldContinueOptimize = envelope.Decision.ShouldContinueOptimize
		flowState.HealthPrimaryProblem = strings.TrimSpace(envelope.Decision.PrimaryProblem)
		flowState.HealthRecommendedOperation = strings.TrimSpace(envelope.Decision.RecommendedOperation)
		flowState.HealthIsForcedImperfection = envelope.Decision.IsForcedImperfection
		flowState.HealthImprovementSignal = strings.TrimSpace(envelope.Decision.ImprovementSignal)
	}
	if signal := strings.TrimSpace(flowState.HealthImprovementSignal); signal != "" && prevSignal != "" && signal == prevSignal {
		flowState.HealthStagnationCount++
		return
	}
	flowState.HealthStagnationCount = 0
}
