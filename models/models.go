package models

type FutureResult struct {
	Exchange   string  `json:"ex"`
	Symbol     string  `json:"sym"`
	BaseCoin   string  `json:"bc"`
	Multiplier float64 `json:"mult"`
	Bid        float64 `json:"b"`
	Ask        float64 `json:"a"`
	BidSize    float64 `json:"bq"`
	AskSize    float64 `json:"aq"`
	Timestamp  int64   `json:"ts"`

	Funding     float64 `json:"f"`
	NextFunding int64   `json:"nf"`
	Interval    int     `json:"i"`
	MarkPrice   float64 `json:"mp"`
	IndexPrice  float64 `json:"ip"`
	Deviation   float64 `json:"dev"`
	TakerFee    float64 `json:"fee"`
}

type SpotResult struct {
	Exchange   string  `json:"ex"`
	Symbol     string  `json:"sym"`
	BaseCoin   string  `json:"bc"`
	Multiplier float64 `json:"mult"`
	Bid        float64 `json:"b"`
	Ask        float64 `json:"a"`
	BidSize    float64 `json:"bq"`
	AskSize    float64 `json:"aq"`
	TakerFee   float64 `json:"fee"`
}

type MarginResult struct {
	Exchange   string  `json:"ex"`
	Symbol     string  `json:"sym"`
	BaseCoin   string  `json:"bc"`
	Multiplier float64 `json:"mult"`
	Bid        float64 `json:"b"`
	Ask        float64 `json:"a"`
	BidSize    float64 `json:"bq"`
	AskSize    float64 `json:"aq"`
	TakerFee   float64 `json:"fee"`
}

type FundingHistoryResult struct {
	Exchange   string  `json:"ex"`
	Symbol     string  `json:"sym"`
	BaseCoin   string  `json:"bc"`
	Funding1D  float64 `json:"f1d"`
	Funding3D  float64 `json:"f3d"`
	Funding7D  float64 `json:"f7d"`
	Funding30D float64 `json:"f30d"`
	Funding90D float64 `json:"f90d"`
}

// ⭐️ РАСШИРЕНО ХРАНИЛИЩЕ ⭐️
// Индекс и Марк-прайсы отправляются прямо в фронтенд!
type Opportunity struct {
	Coin         string  `json:"coin"`
	BuyExchange  string  `json:"buy_exchange"`
	SellExchange string  `json:"sell_exchange"`
	BuySymbol    string  `json:"buy_symbol"`
	SellSymbol   string  `json:"sell_symbol"`
	Spread       float64 `json:"spread"`
	BidPrice     float64 `json:"bid_price"`
	AskPrice     float64 `json:"ask_price"`

	BuyCapacityUSD  float64 `json:"buy_cap"`
	SellCapacityUSD float64 `json:"sell_cap"`

	BuyInterval    int     `json:"buy_interval"`
	SellInterval   int     `json:"sell_interval"`
	BuyCommission  float64 `json:"buy_commission"`
	SellCommission float64 `json:"sell_commission"`

	// НОВЫЕ ПЕРЕМЕННЫЕ ОТКЛОНЕНИЙ:
	BuyDeviation   float64 `json:"buy_deviation"`
	SellDeviation  float64 `json:"sell_deviation"`
	BuyMarkPrice   float64 `json:"buy_mark_price"`
	SellMarkPrice  float64 `json:"sell_mark_price"`
	BuyIndexPrice  float64 `json:"buy_index_price"`
	SellIndexPrice float64 `json:"sell_index_price"`

	FundingRates    map[string]float64 `json:"funding_rates"`
	AccumulatedBuy  map[string]float64 `json:"accumulated_buy"`
	AccumulatedSell map[string]float64 `json:"accumulated_sell"`
}
