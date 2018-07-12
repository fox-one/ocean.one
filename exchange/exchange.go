package exchange

import (
	"context"
	"encoding/base64"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/MixinMessenger/bot-api-go-client"
	"github.com/MixinMessenger/go-number"
	"github.com/fox-one/ocean.one/config"
	"github.com/fox-one/ocean.one/engine"
	"github.com/fox-one/ocean.one/mixin"
	"github.com/fox-one/ocean.one/persistence"
	"github.com/satori/go.uuid"
	"github.com/ugorji/go/codec"
)

const (
	PollInterval                    = 100 * time.Millisecond
	CheckpointMixinNetworkSnapshots = "exchange-checkpoint-mixin-network-snapshots"
)

type Exchange struct {
	books     map[string]*engine.Book
	codec     codec.Handle
	snapshots map[string]bool

	persist     persistence.Persist
	mixinClient *mixin.Client
}

func QuotePrecision(assetId string) uint8 {
	switch assetId {
	case MixinAssetId:
		return 8
	case BitcoinAssetId:
		return 8
	case USDTAssetId:
		return 4
	default:
		log.Panicln("QuotePrecision", assetId)
	}
	return 0
}

func QuoteMinimum(assetId string) number.Decimal {
	switch assetId {
	case MixinAssetId:
		return number.FromString("0.0001")
	case BitcoinAssetId:
		return number.FromString("0.0001")
	case USDTAssetId:
		return number.FromString("1")
	default:
		log.Panicln("QuoteMinimum", assetId)
	}
	return number.Zero()
}

func NewExchange(persist persistence.Persist, mixinClient *mixin.Client) *Exchange {
	return &Exchange{
		codec:     new(codec.MsgpackHandle),
		books:     make(map[string]*engine.Book),
		snapshots: make(map[string]bool),

		persist:     persist,
		mixinClient: mixinClient,
	}
}

func (ex *Exchange) Run(ctx context.Context) {
	go ex.PollMixinMessages(ctx)
	go ex.PollMixinNetwork(ctx)
	go ex.PollTransfers(ctx)
	ex.PollOrderActions(ctx)
}

func (ex *Exchange) Orderbooks(baseAssetId, quoteAssetid string, limit int) ([]*engine.Entry, []*engine.Entry) {
	market := baseAssetId + "-" + quoteAssetid
	book := ex.books[market]
	if book != nil {
		return book.Orderbooks(limit)
	}
	return []*engine.Entry{}, []*engine.Entry{}
}

func (ex *Exchange) PollOrderActions(ctx context.Context) {
	checkpoint, limit := time.Time{}, 500
	for {
		actions, err := ex.persist.ListPendingActions(ctx, checkpoint, limit)
		if err != nil {
			log.Println("ListPendingActions", err)
			time.Sleep(PollInterval)
			continue
		}
		for _, a := range actions {
			ex.ensureProcessOrderAction(ctx, a)
			checkpoint = a.CreatedAt
		}
		if len(actions) < limit {
			time.Sleep(PollInterval)
		}
	}
}

func (ex *Exchange) PollTransfers(ctx context.Context) {
	limit := 500
	for {
		transfers, err := ex.persist.ListPendingTransfers(ctx, limit)
		if err != nil {
			log.Println("ListPendingTransfers", err)
			time.Sleep(PollInterval)
			continue
		}
		for _, t := range transfers {
			ex.ensureProcessTransfer(ctx, t)
		}
		for {
			err = ex.persist.ExpireTransfers(ctx, transfers)
			if err == nil {
				break
			}
			log.Println("ExpireTransfers", err)
			time.Sleep(PollInterval)
		}
		if len(transfers) < limit {
			time.Sleep(PollInterval)
		}
	}
}

type TransferAction struct {
	S string    // source
	O uuid.UUID // cancelled order
	A uuid.UUID // matched ask order
	B uuid.UUID // matched bid order
}

func (ex *Exchange) ensureProcessTransfer(ctx context.Context, transfer *persistence.Transfer) {
	for {
		err := ex.processTransfer(ctx, transfer)
		if err == nil {
			break
		}
		log.Println("processTransfer", err)
		time.Sleep(PollInterval)
	}
}

func (ex *Exchange) processTransfer(ctx context.Context, transfer *persistence.Transfer) error {
	var data *TransferAction
	switch transfer.Source {
	case persistence.TransferSourceOrderFilled:
		data = &TransferAction{S: "FILL", O: uuid.FromStringOrNil(transfer.Detail)}
	case persistence.TransferSourceOrderCancelled:
		data = &TransferAction{S: "CANCEL", O: uuid.FromStringOrNil(transfer.Detail)}
	case persistence.TransferSourceOrderInvalid:
		data = &TransferAction{S: "REFUND", O: uuid.FromStringOrNil(transfer.Detail)}
	case persistence.TransferSourceTradeConfirmed:
		trade, err := ex.persist.ReadTransferTrade(ctx, transfer.Detail, transfer.AssetId)
		if err != nil {
			return err
		}
		if trade == nil {
			log.Panicln(transfer)
		}
		data = &TransferAction{S: "MATCH", A: uuid.FromStringOrNil(trade.AskOrderId), B: uuid.FromStringOrNil(trade.BidOrderId)}
	default:
		log.Panicln(transfer)
	}
	out := make([]byte, 140)
	encoder := codec.NewEncoderBytes(&out, ex.codec)
	err := encoder.Encode(data)
	if err != nil {
		log.Panicln(err)
	}
	memo := base64.StdEncoding.EncodeToString(out)
	if len(memo) > 140 {
		log.Panicln(transfer, memo)
	}
	return ex.sendTransfer(ctx, transfer.UserId, transfer.AssetId, number.FromString(transfer.Amount), transfer.TransferId, memo)
}

func (ex *Exchange) buildBook(ctx context.Context, market string) *engine.Book {
	return engine.NewBook(ctx, market, func(taker, maker *engine.Order, amount, funds number.Integer) string {
		for {
			tradeId, err := ex.persist.Transact(ctx, taker, maker, amount, funds)
			if err == nil {
				return tradeId
			}
			log.Println("Engine Transact CALLBACK", err)
			time.Sleep(PollInterval)
		}
	}, func(order *engine.Order) {
		for {
			err := ex.persist.CancelOrder(ctx, order)
			if err == nil {
				break
			}
			log.Println("Engine Cancel CALLBACK", err)
			time.Sleep(PollInterval)
		}
	})
}

func (ex *Exchange) ensureProcessOrderAction(ctx context.Context, action *persistence.Action) {
	order := action.Order
	market := order.BaseAssetId + "-" + order.QuoteAssetId
	book := ex.books[market]
	if book == nil {
		book = ex.buildBook(ctx, market)
		go book.Run(ctx)
		ex.books[market] = book
	}
	pricePrecision := QuotePrecision(order.QuoteAssetId)
	fundsPrecision := pricePrecision + AmountPrecision
	price := number.FromString(order.Price).Integer(pricePrecision)
	remainingAmount := number.FromString(order.RemainingAmount).Integer(AmountPrecision)
	filledAmount := number.FromString(order.FilledAmount).Integer(AmountPrecision)
	remainingFunds := number.FromString(order.RemainingFunds).Integer(fundsPrecision)
	filledFunds := number.FromString(order.FilledFunds).Integer(fundsPrecision)
	book.AttachOrderEvent(ctx, &engine.Order{
		Id:              order.OrderId,
		Side:            order.Side,
		Type:            order.OrderType,
		Price:           price,
		RemainingAmount: remainingAmount,
		FilledAmount:    filledAmount,
		RemainingFunds:  remainingFunds,
		FilledFunds:     filledFunds,
		Quote:           order.QuoteAssetId,
		Base:            order.BaseAssetId,
		UserId:          order.UserId,
	}, action.Action)
}

func (ex *Exchange) PollMixinNetwork(ctx context.Context) {
	const limit = 500
	for {
		checkpoint, err := ex.persist.ReadPropertyAsTime(ctx, CheckpointMixinNetworkSnapshots)
		if err != nil {
			log.Println("ReadPropertyAsTime CheckpointMixinNetworkSnapshots", err)
			time.Sleep(PollInterval)
			continue
		}
		if checkpoint.IsZero() {
			checkpoint = time.Now().UTC()
		}
		snapshots, err := ex.requestMixinNetwork(ctx, checkpoint, limit)
		if err != nil {
			log.Println("PollMixinNetwork ERROR", err)
			time.Sleep(PollInterval)
			continue
		}
		for _, s := range snapshots {
			if ex.snapshots[s.SnapshotId] {
				continue
			}
			ex.ensureProcessSnapshot(ctx, s)
			checkpoint = s.CreatedAt
			ex.snapshots[s.SnapshotId] = true
		}
		if len(snapshots) < limit {
			time.Sleep(PollInterval)
		}
		err = ex.persist.WriteTimeProperty(ctx, CheckpointMixinNetworkSnapshots, checkpoint)
		if err != nil {
			log.Println("WriteTimeProperty CheckpointMixinNetworkSnapshots", err)
		}
	}
}

func (ex *Exchange) PollMixinMessages(ctx context.Context) {
	for {
		err := bot.Loop(ctx, ex, config.ClientId, config.SessionId, config.SessionKey)
		if err != nil {
			log.Println("PollMixinMessages", err)
			time.Sleep(1 * time.Second)
		}
	}
}

func (ex *Exchange) OnMessage(ctx context.Context, mc *bot.MessageContext, msg bot.MessageView, userId string) error {
	log.Println(msg, userId)
	if msg.Category != "PLAIN_TEXT" {
		return nil
	}
	data, _ := base64.StdEncoding.DecodeString(msg.Data)
	action := strings.Split(string(data), ":")
	if len(action) < 2 {
		return nil
	}
	var memo *OrderAction
	var assetId, assetAmount string
	if action[0] == "CANCEL" {
		if len(action) != 2 {
			return nil
		}
		memo = &OrderAction{
			O: uuid.FromStringOrNil(action[1]),
		}
		assetId = "965e5c6e-434c-3fa9-b780-c50f43cd955c" // CNB
		assetAmount = "1"
	} else {
		if len(action) != 3 {
			return nil
		}
		price := number.FromString(action[1])
		if price.Exhausted() {
			return nil
		}
		amount := number.FromString(action[2])
		if amount.Exhausted() {
			return nil
		}
		memo = &OrderAction{
			T: "L",
			P: price.Persist(),
		}
		switch action[0] {
		case "XIN":
			memo.S = "A"
			memo.A, _ = uuid.FromString(BitcoinAssetId)
			assetId = "c94ac88f-4671-3976-b60a-09064f1811e8"
			assetAmount = amount.Persist()
		case "BTC":
			memo.S = "B"
			memo.A, _ = uuid.FromString("c94ac88f-4671-3976-b60a-09064f1811e8")
			assetId = BitcoinAssetId
			amount = price.Mul(amount)
			if amount.Exhausted() {
				return nil
			}
			assetAmount = amount.Persist()
		default:
			return nil
		}
	}
	out := make([]byte, 140)
	handle := new(codec.MsgpackHandle)
	encoder := codec.NewEncoderBytes(&out, handle)
	encoder.Encode(memo)
	traceId, _ := uuid.NewV4()
	bot.SendPlainText(ctx, mc, bot.MessageView{
		ConversationId: msg.ConversationId,
		UserId:         msg.UserId,
		MessageId:      traceId.String(),
		Category:       "PLAIN_TEXT",
	}, fmt.Sprintf("mixin://pay?recipient=%s&asset=%s&amount=%s&trace=%s&memo=%s", config.ClientId, assetId, assetAmount, traceId.String(), base64.StdEncoding.EncodeToString(out)))
	return nil
}
