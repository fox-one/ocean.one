package persistence

import (
	"cloud.google.com/go/spanner"
)

type Spanner struct {
	spanner *spanner.Client
	dapp    *Broker
}

func CreateSpanner(client *spanner.Client, dapp *Broker) Persist {
	return &Spanner{
		spanner: client,
		dapp:    dapp,
	}
}
