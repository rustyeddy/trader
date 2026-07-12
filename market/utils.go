package market

// BitIsSet reports whether bit i is set in the bitset.
func BitIsSet(bits []uint64, i int) bool {
	return (bits[i>>6] & (uint64(1) << uint(i&63))) != 0
}

// BitSet sets bit i in the bitset.
func BitSet(bits []uint64, i int) {
	bits[i>>6] |= (uint64(1) << uint(i&63))
}
