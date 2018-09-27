package cache

import (
	"encoding/json"

	"github.com/MixinNetwork/go-number"
	"github.com/emirpasic/gods/trees/redblacktree"
	"github.com/fox-one/f1ex-ocean/models"
)

const (
	PageSideAsk = "ASK"
	PageSideBid = "BID"
)

type OrderBook struct {
	Asks *redblacktree.Tree
	Bids *redblacktree.Tree
}

var books = map[string]*OrderBook{}

func newPage(side string) *redblacktree.Tree {
	var entryCompare func(a, b interface{}) int

	if side == PageSideBid {
		entryCompare = func(a, b interface{}) int {
			return a.(number.Decimal).Cmp(b.(number.Decimal))
		}
	} else if side == PageSideAsk {
		entryCompare = func(a, b interface{}) int {
			return b.(number.Decimal).Cmp(a.(number.Decimal))
		}
	} else {
		return nil
	}

	return redblacktree.NewWith(entryCompare)
}

func getBook(market string) *OrderBook {
	if book, found := books[market]; found {
		return book
	}

	book := &OrderBook{
		Asks: newPage(PageSideAsk),
		Bids: newPage(PageSideBid),
	}
	books[market] = book
	return book
}

func addEntry(page *redblacktree.Tree, price, amount number.Decimal) {
	a, found := page.Get(price)
	if !found {
		if amount.IsPositive() {
			page.Put(price, amount)
		}
		return
	}

	amount = a.(number.Decimal).Add(amount)

	if amount.IsPositive() {
		page.Put(price, amount)
	} else {
		page.Remove(price)
	}
}

func cacheBookT0(market string, event []byte) {
	book := getBook(market)
	var data map[string]interface{}
	json.Unmarshal(event, &data)

	data = data["data"].(map[string]interface{})

	addFunc := func(page *redblacktree.Tree, arr []interface{}) {
		page.Clear()
		for _, item := range arr {
			data := item.(map[string]interface{})
			price, amount := number.FromString(data["price"].(string)), number.FromString(data["amount"].(string))
			page.Put(price, amount)
		}
	}

	addFunc(book.Asks, data["asks"].([]interface{}))
	addFunc(book.Bids, data["bids"].([]interface{}))
}

func cacheOrderEvent(market, eventType string, data map[string]interface{}) {
	book := getBook(market)

	side := data["side"].(string)
	price := data["price"].(number.Integer).Decimal()
	amount := data["amount"].(number.Integer).Decimal()

	var page = book.Bids
	if side == PageSideAsk {
		page = book.Asks
	}

	switch eventType {
	case EventTypeOrderOpen:
		break
	case EventTypeOrderMatch, EventTypeOrderCancel:
		amount = number.Zero().Sub(amount)
		break
	}

	addEntry(page, price, amount)
}

func Orderbooks(market string, count int) ([]*models.OrderBookReply_OrderBook, []*models.OrderBookReply_OrderBook) {
	listFunc := func(page *redblacktree.Tree, count int) []*models.OrderBookReply_OrderBook {
		entries := make([]*models.OrderBookReply_OrderBook, 0, count)
		for it := page.Iterator(); it.Next(); {
			price := it.Key().(number.Decimal)
			amount := it.Value().(number.Decimal)
			entry := &models.OrderBookReply_OrderBook{
				Price:  price.Persist(),
				Amount: amount.Persist(),
			}
			entries = append(entries, entry)
			if count = count - 1; count == 0 {
				it.End()
			}
		}
		return entries
	}

	book := getBook(market)
	return listFunc(book.Asks, count), listFunc(book.Bids, count)
}
