package types

import "fmt"

type Units int64

func (u Units) Int64() int64 {
	return int64(u)
}

func (u Units) String() string {
	return fmt.Sprintf("%d", u)
}
