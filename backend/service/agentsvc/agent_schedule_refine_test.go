package agentsvc

import (
	"testing"

	"github.com/LoveLosita/smartflow/backend/agent/schedulerefine"
)

func TestShouldPersistScheduleRefinePreviewSkipsFailedCompositeRoute(t *testing.T) {
	st := &schedulerefine.ScheduleRefineState{
		CompositeRouteSucceeded: true,
		HardCheck: schedulerefine.HardCheckReport{
			PhysicsPassed: true,
			OrderPassed:   true,
			IntentPassed:  false,
		},
	}

	if shouldPersistScheduleRefinePreview(st) {
		t.Fatalf("期望复合分支终审失败时不覆盖上一版预览")
	}
}

func TestShouldPersistScheduleRefinePreviewAllowsPassedCompositeRoute(t *testing.T) {
	st := &schedulerefine.ScheduleRefineState{
		CompositeRouteSucceeded: true,
		HardCheck: schedulerefine.HardCheckReport{
			PhysicsPassed: true,
			OrderPassed:   true,
			IntentPassed:  true,
		},
	}

	if !shouldPersistScheduleRefinePreview(st) {
		t.Fatalf("期望复合分支终审通过时允许覆盖预览")
	}
}

func TestShouldPersistScheduleRefinePreviewKeepsReactPathBehavior(t *testing.T) {
	st := &schedulerefine.ScheduleRefineState{
		CompositeRouteSucceeded: false,
		HardCheck: schedulerefine.HardCheckReport{
			PhysicsPassed: true,
			OrderPassed:   true,
			IntentPassed:  false,
		},
	}

	if !shouldPersistScheduleRefinePreview(st) {
		t.Fatalf("期望非复合直出分支继续沿用原有预览持久化策略")
	}
}
