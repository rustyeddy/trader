package types

import "time"

type Timestamp int64

func FromTime(t time.Time) Timestamp {
	return Timestamp(t.Unix())
}

func (t Timestamp) Int64() int64 {
	return int64(t)
}

func (t Timestamp) Time() time.Time {
	return time.Unix(t.Int64(), 0)
}

func (t Timestamp) IsZero() bool {
	return t == 0
}

func (t Timestamp) Before(ts Timestamp) bool {
	return ts < t
}

func (t Timestamp) After(ts Timestamp) bool {
	return t < ts
}

func (t Timestamp) Add(i time.Duration) Timestamp {
	return t + Timestamp(i)
}

func (t Timestamp) String() string {
	return time.Unix(t.Int64(), 0).
		UTC().
		Format(time.RFC3339)
}
