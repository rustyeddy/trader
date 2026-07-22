package brokers

// KnownBrokers lists the broker identifiers this build supports. Extend
// this (and the corresponding client-construction dispatch, wherever that
// ends up living once there's a second real broker to design it against)
// when adding one.
var KnownBrokers = []string{"oanda"}

// IsKnownBroker reports whether name is a supported broker identifier.
func IsKnownBroker(name string) bool {
	for _, b := range KnownBrokers {
		if b == name {
			return true
		}
	}
	return false
}
