package exchange

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math/big"
	"sync"
	"time"

	bot "github.com/MixinNetwork/bot-api-go-client"
	"github.com/MixinNetwork/go-number"
	"github.com/fox-one/ocean.one/engine"
	"github.com/satori/go.uuid"
	"github.com/ugorji/go/codec"
)

const (
	AmountPrecision = 4
	MaxPrice        = 1000000000
	MaxAmount       = 5000000000
	MaxFunds        = MaxPrice * MaxAmount

	MixinAssetId   = "c94ac88f-4671-3976-b60a-09064f1811e8"
	BitcoinAssetId = "c6d0c728-2624-429b-8e0d-d9d19b6592fa"
	USDTAssetId    = "815b0b1a-2764-3736-8faa-42d694fa620a"
)

type Error struct {
	Status      int    `json:"status"`
	Code        int    `json:"code"`
	Description string `json:"description"`
}

type Snapshot struct {
	SnapshotId string `json:"snapshot_id"`
	Amount     string `json:"amount"`
	Asset      struct {
		AssetId string `json:"asset_id"`
	} `json:"asset"`
	CreatedAt time.Time `json:"created_at"`

	TraceId    string `json:"trace_id"`
	UserId     string `json:"user_id"`
	OpponentId string `json:"opponent_id"`
	Data       string `json:"data"`
}

type OrderAction struct {
	U []byte    // user
	S string    // side
	A uuid.UUID // asset
	P string    // price
	T string    // type
	O uuid.UUID // order
}

func (ex *Exchange) ensureProcessSnapshot(ctx context.Context, s *Snapshot) {
	for {
		err := ex.processSnapshot(ctx, s)
		if err == nil {
			break
		}
		log.Println("ensureProcessSnapshot", err)
		time.Sleep(100 * time.Millisecond)
	}
}

func (ex *Exchange) processSnapshot(ctx context.Context, s *Snapshot) error {
	if ex.brokers[s.UserId] == nil {
		return nil
	}
	if s.OpponentId == "" || s.TraceId == "" {
		return nil
	}
	if number.FromString(s.Amount).Exhausted() {
		return nil
	}

	action, err := ex.decryptOrderAction(ctx, s.Data)
	if err != nil {
		return ex.refundSnapshot(ctx, s)
	}
	if len(action.U) > 16 {
		return ex.persist.UpdateUserPublicKey(ctx, s.OpponentId, hex.EncodeToString(action.U))
	}
	if action.O.String() != uuid.Nil.String() {
		return ex.persist.CancelOrderAction(ctx, action.O.String(), s.CreatedAt, s.OpponentId)
	}

	order, err := ParseOrderAction(action, s.Asset.AssetId, s.Amount)
	if err != nil {
		return ex.refundSnapshot(ctx, s)
	}

	order.Id = s.TraceId
	return ex.persist.CreateOrderAction(ctx, order, s.OpponentId, s.UserId, s.CreatedAt)
}

func ParseOrderAction(action *OrderAction, assetID string, amountStr string) (*engine.Order, error) {
	if action.A.String() == assetID {
		return nil, fmt.Errorf("same base / quote asset id")
	}
	orderType := action.T
	switch orderType {
	case "L":
		orderType = engine.OrderTypeLimit
	case "M":
		orderType = engine.OrderTypeMarket
	}

	if orderType != engine.OrderTypeLimit && orderType != engine.OrderTypeMarket {
		return nil, fmt.Errorf("invalid order type")
	}

	side := action.S
	switch side {
	case "A":
		side = engine.PageSideAsk
	case "B":
		side = engine.PageSideBid
	}

	quote, base := getQuoteBasePair(assetID, action.A.String(), side)
	if quote == "" {
		return nil, fmt.Errorf("invalid base / quote asset id")
	}

	priceDecimal := number.FromString(action.P)
	maxPrice := number.NewDecimal(MaxPrice, int32(QuotePrecision(quote)))
	if priceDecimal.Cmp(maxPrice) > 0 {
		return nil, fmt.Errorf("price too high")
	}
	price := priceDecimal.Integer(QuotePrecision(quote))
	if orderType == engine.OrderTypeLimit {
		if price.IsZero() {
			return nil, fmt.Errorf("price too low")
		}
	} else if !price.IsZero() {
		return nil, fmt.Errorf("Market Order price must be 0")
	}

	fundsPrecision := AmountPrecision + QuotePrecision(quote)
	funds := number.NewInteger(0, fundsPrecision)
	amount := number.NewInteger(0, AmountPrecision)

	assetDecimal := number.FromString(amountStr)
	if side == engine.PageSideBid {
		maxFunds := number.NewDecimal(MaxFunds, int32(fundsPrecision))
		if assetDecimal.Cmp(maxFunds) > 0 {
			return nil, fmt.Errorf("amount too high")
		}
		funds = assetDecimal.Integer(fundsPrecision)
		if funds.Decimal().Cmp(QuoteMinimum(quote)) < 0 {
			return nil, fmt.Errorf("amount to low")
		}
		if orderType == engine.OrderTypeLimit && !funds.Div(price).Decimal().Round(AmountPrecision).IsPositive() {
			return nil, fmt.Errorf("amount to low")
		}
	} else {
		maxAmount := number.NewDecimal(MaxAmount, AmountPrecision)
		if assetDecimal.Cmp(maxAmount) > 0 {
			return nil, fmt.Errorf("amount to high")
		}
		amount = assetDecimal.Integer(AmountPrecision)
		if orderType == engine.OrderTypeLimit && price.Mul(amount).Decimal().Cmp(QuoteMinimum(quote)) < 0 {
			return nil, fmt.Errorf("amount to low")
		}
	}

	return &engine.Order{
		Type:            orderType,
		Side:            side,
		Quote:           quote,
		Base:            base,
		Price:           price,
		RemainingAmount: amount,
		FilledAmount:    amount.Zero(),
		RemainingFunds:  funds,
		FilledFunds:     funds.Zero(),
	}, nil
}

func getQuoteBasePair(assetID, actionAssetID, side string) (string, string) {
	var quote, base string
	if side == engine.PageSideAsk {
		quote, base = actionAssetID, assetID
	} else if side == engine.PageSideBid {
		quote, base = assetID, actionAssetID
	} else {
		return "", ""
	}
	if quote == base {
		return "", ""
	}
	if quote != BitcoinAssetId && quote != USDTAssetId && quote != MixinAssetId {
		return "", ""
	}
	if quote == BitcoinAssetId && base == USDTAssetId {
		return "", ""
	}
	if quote == MixinAssetId && base == USDTAssetId {
		return "", ""
	}
	if quote == MixinAssetId && base == BitcoinAssetId {
		return "", ""
	}
	return quote, base
}

func (ex *Exchange) refundSnapshot(ctx context.Context, s *Snapshot) error {
	amount := number.FromString(s.Amount).Mul(number.FromString("0.999"))
	if amount.Exhausted() {
		return nil
	}

	return ex.persist.CreateRefundTransfer(ctx, s.UserId, s.OpponentId, s.Asset.AssetId, amount, s.TraceId)
}

func (ex *Exchange) decryptOrderAction(ctx context.Context, data string) (*OrderAction, error) {
	payload, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		payload, err = base64.URLEncoding.DecodeString(data)
		if err != nil {
			return nil, err
		}
	}
	var action OrderAction
	decoder := codec.NewDecoderBytes(payload, ex.codec)
	err = decoder.Decode(&action)
	if err != nil {
		return nil, err
	}
	switch action.T {
	case "L":
		action.T = engine.OrderTypeLimit
	case "M":
		action.T = engine.OrderTypeMarket
	}
	switch action.S {
	case "A":
		action.S = engine.PageSideAsk
	case "B":
		action.S = engine.PageSideBid
	}
	return &action, nil
}

func (ex *Exchange) requestMixinNetwork(ctx context.Context, checkpoint time.Time, limit int) ([]*Snapshot, error) {
	uri := fmt.Sprintf("/network/snapshots?offset=%s&order=ASC&limit=%d", checkpoint.Format(time.RFC3339Nano), limit)
	body, err := ex.mixinClient.SendRequest(ctx, "GET", uri, nil)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Data  []*Snapshot `json:"data"`
		Error string      `json:"error"`
	}
	err = json.Unmarshal(body, &resp)
	if err != nil {
		return nil, err
	}
	if resp.Error != "" {
		return nil, errors.New(resp.Error)
	}
	return resp.Data, nil
}

func (ex *Exchange) sendTransfer(ctx context.Context, brokerId, recipientId, assetId string, amount number.Decimal, traceId, memo string) error {
	mutex := ex.mutexes.fetch(recipientId, assetId)
	mutex.Lock()
	defer mutex.Unlock()

	broker := ex.brokers[brokerId]
	return bot.CreateTransfer(ctx, &bot.TransferInput{
		AssetId:     assetId,
		RecipientId: recipientId,
		Amount:      amount,
		TraceId:     traceId,
		Memo:        memo,
	}, broker.BrokerId, broker.SessionId, broker.SessionKey, broker.DecryptedPIN, broker.PINToken)
}

type tmap struct {
	sync.Map
}

func newTmap() *tmap {
	return &tmap{
		Map: sync.Map{},
	}
}

func (m *tmap) fetch(user, asset string) *sync.Mutex {
	uu, err := uuid.FromString(user)
	if err != nil {
		panic(user)
	}
	u := new(big.Int).SetBytes(uu.Bytes())
	au, err := uuid.FromString(asset)
	if err != nil {
		panic(asset)
	}
	a := new(big.Int).SetBytes(au.Bytes())
	s := new(big.Int).Add(u, a)
	key := new(big.Int).Mod(s, big.NewInt(100)).String()
	if _, found := m.Load(key); !found {
		m.Store(key, new(sync.Mutex))
	}
	val, _ := m.Load(key)
	return val.(*sync.Mutex)
}
