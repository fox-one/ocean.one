package persistence

import (
	"context"
	"time"

	"cloud.google.com/go/spanner"
	"google.golang.org/api/iterator"
)

type Property struct {
	Key       string
	Value     string
	UpdatedAt time.Time
}

func (p *Spanner) ReadProperty(ctx context.Context, key string) (string, error) {
	it := p.spanner.Single().Read(ctx, "properties", spanner.Key{key}, []string{"value"})
	defer it.Stop()

	row, err := it.Next()
	if err == iterator.Done {
		return "", nil
	} else if err != nil {
		return "", err
	}

	var value string
	err = row.Column(0, &value)
	return value, err
}

func (p *Spanner) WriteProperty(ctx context.Context, key, value string) error {
	_, err := p.spanner.Apply(ctx, []*spanner.Mutation{
		spanner.InsertOrUpdate("properties", []string{"key", "value", "updated_at"}, []interface{}{key, value, time.Now()}),
	})
	return err
}

func (p *Spanner) ReadPropertyAsTime(ctx context.Context, key string) (time.Time, error) {
	var offset time.Time
	timestamp, err := p.ReadProperty(ctx, key)
	if err != nil {
		return offset, err
	}
	if timestamp != "" {
		return time.Parse(time.RFC3339Nano, timestamp)
	}
	return offset, nil
}

func (p *Spanner) WriteTimeProperty(ctx context.Context, key string, value time.Time) error {
	return p.WriteProperty(ctx, key, value.UTC().Format(time.RFC3339Nano))
}
