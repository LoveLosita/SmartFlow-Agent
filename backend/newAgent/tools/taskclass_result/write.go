package taskclass_result

import "fmt"

// BuildUpsertTaskClassView 把 upsert_task_class 的稳定结果摘要转成任务类写入卡片。
//
// 步骤化说明：
// 1. 先基于父包传入的 Status/Result 生成折叠态标题、摘要和稳定指标，保证成功/失败都能快速扫读；
// 2. 再把任务类字段、配置、任务项列表和失败原因拆成 kv/items/callout section，避免前端继续回退 raw_text；
// 3. raw_text 与 machine_payload 始终保留，便于模型链路、调试链路和后续交互共用同一份 observation 语义。
func BuildUpsertTaskClassView(input BuildUpsertTaskClassViewInput) WriteResultView {
	status := normalizeStatus(input.Status)
	if status == "" {
		if input.Result.Success {
			status = StatusDone
		} else {
			status = StatusFailed
		}
	}

	items := buildTaskClassItemViews(input.Request.Items)
	sections := buildUpsertSections(input.Result, input.Request, items, status)

	return buildWriteResultView(
		status,
		buildUpsertTitle(input.Result, status),
		buildUpsertSubtitle(input.Result, input.Request, status),
		buildUpsertMetrics(input.Result, input.Request),
		items,
		sections,
		input.Observation,
		input.MachinePayload,
	)
}

func buildUpsertTitle(result UpsertResult, status string) string {
	if normalizeStatus(status) != StatusDone {
		return "任务类写入失败"
	}
	if result.Created {
		return "任务类已创建"
	}
	return "任务类已更新"
}

func buildUpsertSubtitle(result UpsertResult, request RequestSummary, status string) string {
	name := fallbackText(request.Name, "未命名任务类")
	itemCount := len(request.Items)
	if normalizeStatus(status) == StatusDone {
		action := "更新"
		if result.Created {
			action = "创建"
		}
		return fmt.Sprintf("已%s「%s」，共 %d 项任务", action, name, itemCount)
	}

	if len(result.ValidationIssues) > 0 {
		return fmt.Sprintf("「%s」校验未通过：%s", name, result.ValidationIssues[0])
	}
	if result.Error != "" {
		return fmt.Sprintf("「%s」写入失败：%s", name, result.Error)
	}
	return fmt.Sprintf("「%s」写入失败，请查看详情", name)
}

func buildUpsertMetrics(result UpsertResult, request RequestSummary) []MetricField {
	action := "更新"
	if result.Created {
		action = "创建"
	}
	if !result.Success && request.RequestedID == 0 {
		action = "创建尝试"
	}
	if !result.Success && request.RequestedID > 0 {
		action = "更新尝试"
	}

	return []MetricField{
		buildMetric("任务类数量", "1 个"),
		buildMetric("任务项数量", fmt.Sprintf("%d 项", len(request.Items))),
		buildMetric("来源", formatSourceCN(request.Source)),
		buildMetric("写入方式", action),
	}
}

func buildUpsertSections(
	result UpsertResult,
	request RequestSummary,
	items []ItemView,
	status string,
) []map[string]any {
	sections := []map[string]any{
		buildResultCallout(result, request, status),
		buildKVSection("任务类字段", buildTaskClassFields(result, request)),
		buildKVSection("排程配置", buildTaskClassConfigFields(request)),
	}

	if len(items) > 0 {
		sections = append(sections, buildItemsSection("任务项列表", items))
	} else {
		sections = append(sections, buildCalloutSection(
			"任务项列表",
			"当前没有可展示的任务项。",
			"info",
			[]string{"如果这是一次失败写入，请优先检查 task_class.items 或顶层 items 入参是否完整。"},
		))
	}

	if len(result.ValidationIssues) > 0 {
		sections = append(sections, buildCalloutSection(
			"校验失败原因",
			"请求参数未通过后端校验。",
			"warning",
			normalizeStringSlice(result.ValidationIssues),
		))
	}
	return sections
}

func buildResultCallout(result UpsertResult, request RequestSummary, status string) map[string]any {
	if normalizeStatus(status) == StatusDone {
		action := "更新"
		if result.Created {
			action = "创建"
		}
		detailLines := []string{
			fmt.Sprintf("任务类：%s", fallbackText(request.Name, "未命名任务类")),
			fmt.Sprintf("任务类 ID：%d", resolveDisplayTaskClassID(result, request)),
			fmt.Sprintf("任务项数量：%d 项", len(request.Items)),
		}
		return buildCalloutSection(
			"写入结果",
			fmt.Sprintf("已%s任务类，结果可直接用于后续排程。", action),
			"success",
			detailLines,
		)
	}

	reason := result.Error
	if len(result.ValidationIssues) > 0 {
		reason = result.ValidationIssues[0]
	}
	if reason == "" {
		reason = "写入流程未返回明确失败原因，请查看原始 observation。"
	}
	return buildCalloutSection(
		"写入失败",
		reason,
		"danger",
		[]string{
			fmt.Sprintf("来源：%s", formatSourceCN(request.Source)),
			fmt.Sprintf("任务类：%s", fallbackText(request.Name, "未命名任务类")),
			fmt.Sprintf("任务项数量：%d 项", len(request.Items)),
		},
	)
}

func buildTaskClassFields(result UpsertResult, request RequestSummary) []KVField {
	return []KVField{
		buildKVField("任务类 ID", fmt.Sprintf("%d", resolveDisplayTaskClassID(result, request))),
		buildKVField("名称", fallbackText(request.Name, "未命名任务类")),
		buildKVField("模式", formatModeCN(request.Mode)),
		buildKVField("日期范围", formatDateRangeCN(request.StartDate, request.EndDate)),
		buildKVField("学科类型", formatSubjectTypeCN(request.SubjectType)),
		buildKVField("难度等级", formatLevelCN(request.DifficultyLevel)),
		buildKVField("认知强度", formatLevelCN(request.CognitiveIntensity)),
		buildKVField("来源", formatSourceCN(request.Source)),
	}
}

func buildTaskClassConfigFields(request RequestSummary) []KVField {
	return []KVField{
		buildKVField("总节数", fmt.Sprintf("%d", request.TotalSlots)),
		buildKVField("允许补位课程", formatBoolCN(request.AllowFillerCourse)),
		buildKVField("推进策略", formatStrategyCN(request.Strategy)),
		buildKVField("排除半天块", formatIntListCN(request.ExcludedSlots, "无", func(value int) string {
			return fmt.Sprintf("第%d块", value)
		})),
		buildKVField("排除星期", formatIntListCN(request.ExcludedDaysOfWeek, "无", formatWeekdayCN)),
	}
}

func buildTaskClassItemViews(items []TaskClassItemSummary) []ItemView {
	if len(items) == 0 {
		return make([]ItemView, 0)
	}
	out := make([]ItemView, 0, len(items))
	for _, item := range items {
		detailLines := []string{
			"内容：" + fallbackText(item.Content, "未填写内容"),
			"嵌入时间：" + formatEmbeddedTimeCN(item),
		}
		if item.ID > 0 {
			detailLines = append(detailLines, fmt.Sprintf("任务项 ID：%d", item.ID))
		}
		out = append(out, buildItem(
			truncateText(item.Content, 28),
			fmt.Sprintf("第 %d 项", maxInt(item.Order, 0)),
			buildTaskClassItemTags(item),
			detailLines,
			map[string]any{
				"id":            item.ID,
				"order":         item.Order,
				"embedded_week": item.EmbeddedWeek,
				"embedded_day":  item.EmbeddedDay,
				"section_from":  item.EmbeddedSectionFrom,
				"section_to":    item.EmbeddedSectionTo,
			},
		))
	}
	return out
}

func buildTaskClassItemTags(item TaskClassItemSummary) []string {
	tags := []string{fmt.Sprintf("顺序 %d", maxInt(item.Order, 0))}
	if item.EmbeddedWeek > 0 && item.EmbeddedDay > 0 {
		tags = append(tags, formatEmbeddedTimeCN(item))
	} else {
		tags = append(tags, "未指定嵌入时间")
	}
	return tags
}

func resolveDisplayTaskClassID(result UpsertResult, request RequestSummary) int {
	if result.TaskClassID > 0 {
		return result.TaskClassID
	}
	return request.RequestedID
}

func maxInt(values ...int) int {
	if len(values) == 0 {
		return 0
	}
	best := values[0]
	for _, value := range values[1:] {
		if value > best {
			best = value
		}
	}
	return best
}
