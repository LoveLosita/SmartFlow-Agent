package llm

import (
	"sync"

	"github.com/cloudwego/eino/schema"
)

// usageAccountingStreamReader 负责在流式读取结束时统一回收 usage。
type usageAccountingStreamReader struct {
	source StreamReader
	onDone func(usage *schema.TokenUsage)

	once  sync.Once
	usage *schema.TokenUsage
}

func NewUsageAccountingStreamReader(source StreamReader, onDone func(usage *schema.TokenUsage)) StreamReader {
	if source == nil {
		return nil
	}
	return &usageAccountingStreamReader{
		source: source,
		onDone: onDone,
	}
}

func (r *usageAccountingStreamReader) Recv() (*schema.Message, error) {
	if r == nil || r.source == nil {
		return nil, nil
	}

	msg, err := r.source.Recv()
	if msg != nil && msg.ResponseMeta != nil {
		r.usage = MergeUsage(r.usage, msg.ResponseMeta.Usage)
	}
	if err != nil {
		r.finish()
	}
	return msg, err
}

func (r *usageAccountingStreamReader) Close() error {
	if r == nil || r.source == nil {
		return nil
	}
	err := r.source.Close()
	r.finish()
	return err
}

func (r *usageAccountingStreamReader) finish() {
	if r == nil {
		return
	}
	r.once.Do(func() {
		if r.onDone != nil {
			r.onDone(CloneUsage(r.usage))
		}
	})
}
