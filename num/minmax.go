package num

// Ordered is implemented by the value types with a total order that cannot
// fail: Price, Quantity and Rate. Money is not Ordered, because ordering two
// Money values can fail on a currency mismatch; compare it with Money.Cmp.
type Ordered[T any] interface {
	Cmp(T) int
}

// Min returns the smallest of first and rest. When several values are equal
// the earliest one is returned. Min with no rest returns first.
func Min[T Ordered[T]](first T, rest ...T) T {
	for _, v := range rest {
		if v.Cmp(first) < 0 {
			first = v
		}
	}
	return first
}

// Max returns the largest of first and rest. When several values are equal the
// earliest one is returned. Max with no rest returns first.
func Max[T Ordered[T]](first T, rest ...T) T {
	for _, v := range rest {
		if v.Cmp(first) > 0 {
			first = v
		}
	}
	return first
}
