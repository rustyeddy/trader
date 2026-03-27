package strategies

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNoopStrategyUpdate(t *testing.T) {
	strat := NoopStrategy{}
	ctx := context.Background()

	// NoopStrategy should do nothing and return no error
	err := strat.Update(ctx, nil)
	assert.NoError(t, err)
}
