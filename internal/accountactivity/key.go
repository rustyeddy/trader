package accountactivity

// Key is an Activity's durable, provider-native identity:
//
//	Provider + Environment + ProviderAccountID + ProviderActivityID
//
// It is what makes re-importing the same provider activity idempotent and
// reconciliation trustworthy. It is comparable, so it can be a map key. It
// deliberately excludes ImportedAt and every value field: importing the
// same activity again later is the same record.
type Key struct {
	Provider           string
	Environment        Environment
	ProviderAccountID  string
	ProviderActivityID string
}

// String renders the key as provider/environment/account/activity.
func (k Key) String() string {
	return k.Provider + "/" + k.Environment.String() + "/" + k.ProviderAccountID + "/" + k.ProviderActivityID
}
