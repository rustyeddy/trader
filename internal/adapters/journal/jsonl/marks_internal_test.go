package jsonl

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFromMarkWiresRejectsMarkWithNoPosition(t *testing.T) {
	_, err := fromMarkWires([]markWire{{InstrumentID: "fx:EUR/USD", Provider: "sim"}}, nil)
	assert.ErrorIs(t, err, ErrCorruptEntry)
}

func TestFromMarkWiresEmpty(t *testing.T) {
	marks, err := fromMarkWires(nil, nil)
	assert.NoError(t, err)
	assert.Nil(t, marks)
}
