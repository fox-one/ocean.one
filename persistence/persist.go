package persistence

import (
	"context"
	"time"

	"github.com/MixinMessenger/go-number"
	"github.com/fox-one/ocean.one/engine"
)

type Persist interface {
	ReadProperty(ctx context.Context, key string) (string, error)
	WriteProperty(ctx context.Context, key, value string) error
	ReadPropertyAsTime(ctx context.Context, key string) (time.Time, error)
	WriteTimeProperty(ctx context.Context, key string, value time.Time) error

	ListPendingActions(ctx context.Context, checkpoint time.Time, limit int) ([]*Action, error)
	CreateOrderAction(ctx context.Context, o *engine.Order, userId string, createdAt time.Time) error
	CancelOrderAction(ctx context.Context, orderId string, createdAt time.Time, userId string) error

	LastTrade(ctx context.Context, market string) (*Trade, error)
	MarketTrades(ctx context.Context, market string, offset time.Time, limit int) ([]*Trade, error)

	Transact(ctx context.Context, taker, maker *engine.Order, amount, funds number.Integer) (string, error)
	CancelOrder(ctx context.Context, order *engine.Order) error

	ListPendingTransfers(ctx context.Context, limit int) ([]*Transfer, error)
	ExpireTransfers(ctx context.Context, transfers []*Transfer) error
	ReadTransferTrade(ctx context.Context, tradeId, assetId string) (*Trade, error)
	CreateRefundTransfer(ctx context.Context, userId, assetId string, amount number.Decimal, trace string) error

	UpdateUserPublicKey(ctx context.Context, userId, publicKey string) error
	Authenticate(ctx context.Context, jwtToken string) (string, error)
	UserOrders(ctx context.Context, userId string, market, state string, offset time.Time, limit int) ([]*Order, error)
}
