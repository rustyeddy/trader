package market

import (
	"fmt"
	"time"
)

type Price int32
type Money int64
type Cash float64
type Timestamp int64
type Units int64

func (p Price) Money() Money {
	return Money(p)
}

func (p Price) String() string {
	return fmt.Sprintf("%f", p)
}

func (m Money) Cash(sc int32) Cash {
	return Cash(m / Money(sc))
}

func (m Money) String() string {
	return fmt.Sprintf("%f", m)
}

func (c Cash) String() string {
	return fmt.Sprintf("%f", c)
}

func (t Timestamp) String() string {
	to := time.Unix(int64(t), 0)
	return to.Format(time.RFC3339)
}

func (u Units) String() string {
	return fmt.Sprintf("%d", u)
}
