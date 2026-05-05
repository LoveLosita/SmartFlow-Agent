package agentnode

import (
	"context"

	agentexecute "github.com/LoveLosita/smartflow/backend/services/agent/node/execute"
)

type ExecuteNodeInput = agentexecute.ExecuteNodeInput
type ExecuteRoundObservation = agentexecute.ExecuteRoundObservation

func RunExecuteNode(ctx context.Context, input ExecuteNodeInput) error {
	return agentexecute.RunExecuteNode(ctx, input)
}
