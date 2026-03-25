package agentsvc

import (
	"context"
	"errors"
	"log"
	"strings"

	agentgraph "github.com/LoveLosita/smartflow/backend/agent2/graph"
	agentmodel "github.com/LoveLosita/smartflow/backend/agent2/model"
	agentnode "github.com/LoveLosita/smartflow/backend/agent2/node"
	"github.com/LoveLosita/smartflow/backend/model"
	"github.com/LoveLosita/smartflow/backend/respond"
	"github.com/cloudwego/eino-ext/components/model/ark"
)

// runScheduleRefineFlow 鎵ц鈥滆繛缁璇濆井璋冩帓绋嬧€濆垎鏀€?
//
// 鑱岃矗杈圭晫锛?
// 1. 璐熻矗璇诲彇鈥滀笂涓€鐗堟帓绋嬮瑙堝揩鐓р€濓紙浼樺厛 Redis锛岀己澶卞啀鍥炴簮 MySQL锛夛紱
// 2. 璐熻矗璋冪敤鐙珛 schedulerefine 鍥鹃摼璺畬鎴愭湰杞井璋冿紱
// 3. 璐熻矗鎶婂井璋冪粨鏋滃洖鍐欓瑙堢紦瀛樹笌鐘舵€佸揩鐓э紝渚涘悗缁户缁井璋冿紱
// 4. 涓嶈礋璐ｈ亰澶╂秷鎭寔涔呭寲锛堟秷鎭寔涔呭寲鐢?AgentChat 涓婚摼璺粺涓€澶勭悊锛夈€?
func (s *AgentService) runScheduleRefineFlow(
	ctx context.Context,
	selectedModel *ark.ChatModel,
	userMessage string,
	userID int,
	chatID string,
	traceID string,
	emitStage func(stage, detail string),
	outChan chan<- string,
	modelName string,
) (string, error) {
	_ = outChan
	_ = modelName

	// 1. 渚濊禆棰勬锛氭ā鍨嬩负绌烘椂鏃犳硶鎵ц浠讳綍鑺傜偣锛岀洿鎺ュけ璐ラ伩鍏嶇┖鎸囬拡銆?
	if selectedModel == nil {
		return "", errors.New("schedule refine model is nil")
	}

	emitStage("schedule_refine.context.loading", "正在加载上一版排程上下文。")

	// 2. 鍏堟煡 Redis 棰勮蹇収锛屼繚璇佺儹璺緞浣庡欢杩熴€?
	// 2.1 濡傛灉 Redis 鏈懡涓紝鍐嶅洖婧?MySQL 蹇収鍏滃簳锛?
	// 2.2 濡傛灉涓よ€呴兘娌℃湁锛岃鏄庡綋鍓嶄細璇濇病鏈夊彲寰皟鍩虹锛岀洿鎺ヨ繑鍥炰笟鍔￠敊璇€?
	preview := s.loadSchedulePreviewContext(ctx, userID, chatID)
	if preview == nil {
		return "", respond.SchedulePlanPreviewNotFound
	}

	// 3. 鍒濆鍖栧井璋冪姸鎬佸苟杩愯鐙珛鍥俱€?
	state := agentnode.NewScheduleRefineState(traceID, userID, chatID, userMessage, preview)
	finalState, runErr := agentgraph.RunScheduleRefineGraph(ctx, agentnode.ScheduleRefineGraphRunInput{
		Model:     selectedModel,
		State:     state,
		EmitStage: emitStage,
	})
	if runErr != nil {
		return "", runErr
	}
	if finalState == nil {
		return "", errors.New("schedule refine graph returned nil state")
	}

	// 4. 璋冪敤鐩殑锛?
	// 4.1 saveSchedulePlanPreview 鐩墠鏄€滈瑙堢紦瀛?+ MySQL 蹇収鈥濈殑缁熶竴鍐欏叆鍙ｏ紱
	// 4.2 杩欓噷鎶?refine state 鏄犲皠涓?scheduleplan state锛屽鐢ㄥ凡鏈夎惤鐩橀摼璺紱
	// 4.3 浣嗚嫢鏄€滅嫭绔嬪鍚堝垎鏀凡鍑虹珯銆佺粓瀹′粛澶辫触鈥濓紝鍒欎笉瑕嗙洊涓婁竴鐗堥瑙堬紝閬垮厤澶栭儴璇互涓烘柊鏂规宸查獙璇侀€氳繃銆?
	if shouldPersistScheduleRefinePreview(finalState) {
		s.saveSchedulePlanPreviewAgent2(ctx, userID, chatID, convertRefineStateToPlanState(finalState))
	} else {
		emitStage("schedule_refine.preview.skipped", "复合分支终审未通过，本轮结果不覆盖上一版预览。")
	}

	reply := strings.TrimSpace(finalState.FinalSummary)
	if reply == "" {
		reply = "微调已完成，但本轮未生成总结文案。"
	}
	return reply, nil
}

// loadSchedulePreviewContext 璇诲彇鈥滃彲鐢ㄤ簬杩炵画寰皟鈥濈殑鎺掔▼涓婁笅鏂囧揩鐓с€?
//
// 姝ラ鍖栬鏄庯細
// 1. 鍏堟煡 Redis锛氬懡涓垯鐩存帴杩斿洖锛屾椂寤舵渶灏忥紱
// 2. Redis miss 鍐嶆煡 MySQL锛氫繚璇佺紦瀛樿繃鏈熷悗浠嶅彲缁х画寰皟锛?
// 3. 鑻?MySQL 鍛戒腑涓?Redis 鍙敤锛岄『渚垮洖濉?Redis锛屾彁鍗囧悗缁懡涓巼锛?
// 4. 浠讳竴姝ュけ璐ヤ粎鎵撴棩蹇楋紝涓?panic锛岀敱涓婂眰鏍规嵁杩斿洖 nil 鍋氱粺涓€澶勭悊銆?
func (s *AgentService) loadSchedulePreviewContext(ctx context.Context, userID int, chatID string) *model.SchedulePlanPreviewCache {
	normalizedChatID := strings.TrimSpace(chatID)
	if normalizedChatID == "" || userID <= 0 {
		return nil
	}

	if s.cacheDAO != nil {
		preview, err := s.cacheDAO.GetSchedulePlanPreviewFromCache(ctx, userID, normalizedChatID)
		if err != nil {
			log.Printf("璇诲彇鎺掔▼棰勮缂撳瓨澶辫触 chat_id=%s: %v", normalizedChatID, err)
		} else if preview != nil {
			return preview
		}
	}

	if s.repo == nil {
		return nil
	}
	snapshot, err := s.repo.GetScheduleStateSnapshot(ctx, userID, normalizedChatID)
	if err != nil {
		log.Printf("璇诲彇鎺掔▼鐘舵€佸揩鐓уけ璐?chat_id=%s: %v", normalizedChatID, err)
		return nil
	}
	if snapshot == nil {
		return nil
	}

	preview := snapshotToSchedulePlanPreviewCache(snapshot)
	if preview != nil && s.cacheDAO != nil {
		if setErr := s.cacheDAO.SetSchedulePlanPreviewToCache(ctx, userID, normalizedChatID, preview); setErr != nil {
			log.Printf("鍥炲～鎺掔▼棰勮缂撳瓨澶辫触 chat_id=%s: %v", normalizedChatID, setErr)
		}
	}
	return preview
}

// convertRefineStateToPlanState 鎶?schedulerefine 鐘舵€佹槧灏勪负 scheduleplan 鐘舵€併€?
//
// 璁捐鎰忓浘锛?
// 1. 澶嶇敤鐜版湁 saveSchedulePlanPreview 鍐欏叆閾捐矾锛屽噺灏戦噸澶嶈惤鐩樹唬鐮侊紱
// 2. 浠呮槧灏勨€滈瑙堟寔涔呭寲蹇呴』瀛楁鈥濓紝閬垮厤鎶?refine 杩愯鏈熶复鏃跺瓧娈靛甫鍏ュ瓨鍌ㄥ眰锛?
// 3. 鍚庣画濡傝鎵╁睍 refine 涓撳睘蹇収瀛楁锛屽彲鍦ㄨ鏄犲皠澶勯泦涓紨杩涖€?
func convertRefineStateToPlanState(st *agentnode.ScheduleRefineState) *agentmodel.SchedulePlanState {
	if st == nil {
		return nil
	}
	adjustmentScope := "medium"
	if st.Contract.Strategy == "keep" {
		adjustmentScope = "small"
	}
	return &agentmodel.SchedulePlanState{
		TraceID:         strings.TrimSpace(st.TraceID),
		UserID:          st.UserID,
		ConversationID:  strings.TrimSpace(st.ConversationID),
		UserIntent:      strings.TrimSpace(st.UserIntent),
		Constraints:     append([]string(nil), st.Constraints...),
		TaskClassIDs:    append([]int(nil), st.TaskClassIDs...),
		Strategy:        "steady",
		AdjustmentScope: adjustmentScope,
		IsAdjustment:    true,

		HybridEntries:  append([]model.HybridScheduleEntry(nil), st.HybridEntries...),
		AllocatedItems: cloneTaskClassItems(st.AllocatedItems),
		CandidatePlans: cloneWeekSchedules(st.CandidatePlans),

		FinalSummary: strings.TrimSpace(st.FinalSummary),
		Completed:    st.Completed,
	}
}

// shouldPersistScheduleRefinePreview 鍒ゆ柇鈥滄湰杞井璋冪粨鏋滄槸鍚﹀簲瑕嗙洊涓婁竴鐗堥瑙堚€濄€?
//
// 鑱岃矗杈圭晫锛?
// 1. 榛樿娌跨敤鍘熸湁 refine 鎸佷箙鍖栫瓥鐣ワ紝淇濊瘉鏅€?ReAct 寰皟閾捐矾涓嶅彈褰卞搷锛?
// 2. 浠呭綋鈥滅嫭绔嬪鍚堝垎鏀凡鐩存帴鍑虹珯锛屼絾缁堝鏈€氳繃鈥濇椂锛屾嫆缁濊鐩栦笂涓€鐗堥瑙堬紱
// 3. 杩欐牱鍙互閬垮厤澶栧眰鎶婃湭缁忛獙璇佺殑澶嶅悎缁撴灉褰撴垚鏂扮殑鍩虹嚎缁х画婊氬姩寰皟銆?
func shouldPersistScheduleRefinePreview(st *agentnode.ScheduleRefineState) bool {
	if st == nil {
		return false
	}
	if st.CompositeRouteSucceeded && !agentnode.FinalHardCheckPassed(st) {
		return false
	}
	return true
}
