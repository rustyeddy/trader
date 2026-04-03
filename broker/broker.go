package broker

import (
	"context"

	"github.com/rustyeddy/trader/portfolio"
)

type Broker interface {
	SubmitOpen(ctx context.Context, req *portfolio.OpenRequest) error
	SubmitClose(ctx context.Context, req *portfolio.CloseRequest) error
	Events() <-chan *Event
}
