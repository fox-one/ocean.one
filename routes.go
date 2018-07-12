package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/bugsnag/bugsnag-go/errors"
	"github.com/dimfeld/httptreemux"
	"github.com/fox-one/ocean.one/cache"
	"github.com/fox-one/ocean.one/persistence"
	"github.com/unrolled/render"
)

type R struct {
	persist persistence.Persist
}

func NewRouter(persist persistence.Persist) *httptreemux.TreeMux {
	router, impl := httptreemux.New(), &R{persist: persist}
	router.GET("/markets/:id/ticker", impl.marketTicker)
	router.GET("/markets/:id/book", impl.marketBook)
	router.GET("/markets/:id/trades", impl.marketTrades)
	router.GET("/orders", impl.orders)
	registerHanders(router)
	return router
}

func (impl *R) marketTicker(w http.ResponseWriter, r *http.Request, params map[string]string) {
	t, err := impl.persist.LastTrade(r.Context(), params["id"])
	if err != nil {
		render.New().JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	if t == nil {
		render.New().JSON(w, http.StatusOK, map[string]interface{}{})
		return
	}
	b, err := cache.Book(r.Context(), params["id"], 1)
	if err != nil {
		render.New().JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	ticker := map[string]interface{}{
		"trade_id":  t.TradeId,
		"amount":    t.Amount,
		"price":     t.Price,
		"sequence":  b.Sequence,
		"timestamp": b.Timestamp,
		"ask":       "0",
		"bid":       "0",
	}
	data, _ := json.Marshal(b.Data)
	var best struct {
		Asks []struct {
			Price string `json:"price"`
		} `json:"asks"`
		Bids []struct {
			Price string `json:"price"`
		} `json:"bids"`
	}
	json.Unmarshal(data, &best)
	if len(best.Asks) > 0 {
		ticker["ask"] = best.Asks[0].Price
	}
	if len(best.Bids) > 0 {
		ticker["bid"] = best.Bids[0].Price
	}
	render.New().JSON(w, http.StatusOK, ticker)
}

func (impl *R) marketBook(w http.ResponseWriter, r *http.Request, params map[string]string) {
	book, err := cache.Book(r.Context(), params["id"], 0)
	if err != nil {
		render.New().JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
	} else {
		render.New().JSON(w, http.StatusOK, book)
	}
}

func (impl *R) marketTrades(w http.ResponseWriter, r *http.Request, params map[string]string) {
	trades, err := impl.persist.MarketTrades(r.Context(), params["id"], time.Now(), 100)
	if err != nil {
		render.New().JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}

	data := make([]map[string]interface{}, 0)
	for _, t := range trades {
		data = append(data, map[string]interface{}{
			"trade_id":   t.TradeId,
			"base":       t.BaseAssetId,
			"quote":      t.QuoteAssetId,
			"side":       t.Side,
			"price":      t.Price,
			"amount":     t.Amount,
			"created_at": t.CreatedAt,
		})
	}
	render.New().JSON(w, http.StatusOK, data)
}

func (impl *R) orders(w http.ResponseWriter, r *http.Request, params map[string]string) {
	userId, err := impl.authenticateUser(r)
	if err != nil {
		render.New().JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	if userId == "" {
		render.New().JSON(w, http.StatusUnauthorized, map[string]interface{}{})
		return
	}

	market := r.URL.Query().Get("market")
	state := r.URL.Query().Get("state")
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := time.Parse(time.RFC3339Nano, r.URL.Query().Get("offset"))
	orders, err := impl.persist.UserOrders(r.Context(), userId, market, state, offset, limit)
	if err != nil {
		render.New().JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}

	data := make([]map[string]interface{}, 0)
	for _, o := range orders {
		data = append(data, map[string]interface{}{
			"order_id":         o.OrderId,
			"order_type":       o.OrderType,
			"base":             o.BaseAssetId,
			"quote":            o.QuoteAssetId,
			"side":             o.Side,
			"price":            o.Price,
			"remaining_amount": o.RemainingAmount,
			"filled_amount":    o.FilledAmount,
			"remaining_funds":  o.RemainingFunds,
			"filled_funds":     o.FilledFunds,
			"state":            o.State,
			"created_at":       o.CreatedAt,
		})
	}
	render.New().JSON(w, http.StatusOK, data)
}

func (impl *R) authenticateUser(r *http.Request) (string, error) {
	header := r.Header.Get("Authorization")
	if !strings.HasPrefix(header, "Bearer ") {
		return "", nil
	}
	return impl.persist.Authenticate(r.Context(), header[7:])
}

func registerHanders(router *httptreemux.TreeMux) {
	router.MethodNotAllowedHandler = func(w http.ResponseWriter, r *http.Request, _ map[string]httptreemux.HandlerFunc) {
		render.New().JSON(w, http.StatusNotFound, map[string]interface{}{})
	}
	router.NotFoundHandler = func(w http.ResponseWriter, r *http.Request) {
		render.New().JSON(w, http.StatusNotFound, map[string]interface{}{})
	}
	router.PanicHandler = func(w http.ResponseWriter, r *http.Request, rcv interface{}) {
		err := fmt.Errorf(string(errors.New(rcv, 2).Stack()))
		render.New().JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
	}
}
