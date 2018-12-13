package persistence

import (
	"context"
	"time"

	"github.com/MixinNetwork/go-number"
	"github.com/fox-one/ocean.one/engine"
)

type Persist interface {
	ReadProperty(ctx context.Context, key string) (string, error)
	WriteProperty(ctx context.Context, key, value string) error
	ReadPropertyAsTime(ctx context.Context, key string) (time.Time, error)
	WriteTimeProperty(ctx context.Context, key string, value time.Time) error

	CountPendingActions(ctx context.Context) (int64, error)
	ListPendingActions(ctx context.Context, checkpoint time.Time, limit int) ([]*Action, error)
	CreateOrderAction(ctx context.Context, o *engine.Order, userId, brokerId string, createdAt time.Time) error
	CancelOrderAction(ctx context.Context, orderId string, createdAt time.Time, userId string) error
	ReadOrder(ctx context.Context, orderId string) (*Order, error)

	LastTrade(ctx context.Context, market string) (*Trade, error)
	MarketTrades(ctx context.Context, market string, offset interface{}, order string, limit int) ([]*Trade, error)

	Transact(ctx context.Context, taker, maker *engine.Order, amount number.Integer) (string, error)
	CancelOrder(ctx context.Context, order *engine.Order) error

	CountPendingTransfers(ctx context.Context) (int64, error)
	ListPendingTransfers(ctx context.Context, broker string, limit int) ([]*Transfer, error)
	ExpireTransfers(ctx context.Context, transfers []*Transfer) error
	ReadTransferTrade(ctx context.Context, tradeId, assetId string) (*Trade, error)
	CreateRefundTransfer(ctx context.Context, brokerId, userId, assetId string, amount number.Decimal, trace string) error

	UpdateUserPublicKey(ctx context.Context, userId, publicKey string) error
	Authenticate(ctx context.Context, jwtToken string) (string, error)
	UserOrders(ctx context.Context, userId string, market, state string, offset time.Time, order string, limit int) ([]*Order, error)
	OrderTrades(ctx context.Context, userId, orderId string) ([]*Trade, error)

	AllBrokers(ctx context.Context, decryptPIN bool) ([]*Broker, error)
	AddBroker(ctx context.Context) (*Broker, error)
}
