package main

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"flag"
	"log"
	"time"

	"cloud.google.com/go/spanner"
	"github.com/fox-one/ocean.one/cache"
	"github.com/fox-one/ocean.one/config"
	"github.com/fox-one/ocean.one/exchange"
	"github.com/fox-one/ocean.one/mixin"
	"github.com/fox-one/ocean.one/persistence"
	"github.com/go-redis/redis"
)

func main() {
	service := flag.String("service", "http", "run a service")
	flag.Parse()

	ctx := context.Background()
	spannerClient, err := spanner.NewClientWithConfig(ctx, config.GoogleCloudSpanner, spanner.ClientConfig{NumChannels: 4,
		SessionPoolConfig: spanner.SessionPoolConfig{
			HealthCheckInterval: 5 * time.Second,
		},
	})
	if err != nil {
		log.Panicln(err)
	}

	persist := persistence.CreateSpanner(spannerClient)

	block, _ := pem.Decode([]byte(config.SessionKey))
	privateKey, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		log.Panicln(err)
	}

	mixinClient := mixin.CreateMixinClient(config.ClientId, config.SessionId, config.PinToken, config.SessionAssetPIN, privateKey)

	redisClient := redis.NewClient(&redis.Options{
		Addr:         config.RedisEngineCacheAddress,
		DB:           config.RedisEngineCacheDatabase,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
		PoolTimeout:  4 * time.Second,
		IdleTimeout:  60 * time.Second,
		PoolSize:     1024,
	})
	err = redisClient.Ping().Err()
	if err != nil {
		log.Panicln(err)
	}

	ctx = cache.SetupRedis(ctx, redisClient)

	switch *service {
	case "engine":
		exchange.NewExchange(persist, mixinClient).Run(ctx)
	case "http":
		StartHTTP(ctx, persist)
	}
}
