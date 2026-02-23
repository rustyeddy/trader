package types

import "fmt"

type Units int64

func (u Units) String() string {
	return fmt.Sprintf("%d", u)
}
