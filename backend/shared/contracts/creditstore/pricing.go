package creditstore

const (
	// ProfitRateScaleBPS 表示利润率的基点精度：10000 = 100%。
	ProfitRateScaleBPS = int64(10_000)
)

// ChargePriceMicrosSet 表示在“原始 CNY 成本 + 利润率”之后得到的实际计费单价。
type ChargePriceMicrosSet struct {
	InputChargePriceMicros     int64
	OutputChargePriceMicros    int64
	CachedChargePriceMicros    int64
	ReasoningChargePriceMicros int64
}

// DeriveChargePriceMicrosSet 负责把一条价格规则里的原始 CNY 成本推导成实际计费用单价。
//
// 职责边界：
// 1. 这里只处理“原始成本 + 利润率”的价格推导，不负责 token 数量聚合与 credit 折算。
// 2. cached/reasoning 仍复用现有兜底语义：未配置时分别退回 input/output 单价。
// 3. 若利润率配置为负且绝对值过大导致结果 <= 0，则统一按 0 处理，避免落出负价格。
func DeriveChargePriceMicrosSet(inputPriceMicros, outputPriceMicros, cachedPriceMicros, reasoningPriceMicros, profitRateBps int64) ChargePriceMicrosSet {
	if cachedPriceMicros <= 0 {
		cachedPriceMicros = inputPriceMicros
	}
	if reasoningPriceMicros <= 0 {
		reasoningPriceMicros = outputPriceMicros
	}

	return ChargePriceMicrosSet{
		InputChargePriceMicros:     ApplyProfitRateToPriceMicros(inputPriceMicros, profitRateBps),
		OutputChargePriceMicros:    ApplyProfitRateToPriceMicros(outputPriceMicros, profitRateBps),
		CachedChargePriceMicros:    ApplyProfitRateToPriceMicros(cachedPriceMicros, profitRateBps),
		ReasoningChargePriceMicros: ApplyProfitRateToPriceMicros(reasoningPriceMicros, profitRateBps),
	}
}

// ApplyProfitRateToPriceMicros 负责把“成本单价”按利润率转换成“实际计费单价”。
func ApplyProfitRateToPriceMicros(priceMicros int64, profitRateBps int64) int64 {
	if priceMicros <= 0 {
		return 0
	}

	scaledRate := ProfitRateScaleBPS + profitRateBps
	if scaledRate <= 0 {
		return 0
	}

	return ceilDivInt64(priceMicros*scaledRate, ProfitRateScaleBPS)
}

func ceilDivInt64(numerator int64, denominator int64) int64 {
	if numerator <= 0 || denominator <= 0 {
		return 0
	}
	return (numerator + denominator - 1) / denominator
}
