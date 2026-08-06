package config

// Options configures one Load call: which sources to consult, how to name
// them, and where their values come from. See the package doc comment for
// precedence and naming rules.
type Options struct {
	// EnvPrefix is prepended to every derived environment variable name. See
	// the package doc comment's naming-convention section. Empty means no
	// prefix.
	EnvPrefix string

	// Environ supplies the environment source as NAME=VALUE strings, in the
	// format os.Environ returns. Nil makes Load read the real process
	// environment via os.Environ. Tests should always set this explicitly —
	// including to an empty, non-nil slice, to disable the environment
	// source entirely — so results do not depend on the developer's
	// environment.
	Environ []string

	// FilePath, if non-empty, is read as a YAML file and used as the file
	// source. Ignored when FileContent is non-nil.
	FilePath string

	// FileContent, if non-nil, is parsed as YAML directly instead of reading
	// FilePath. Tests should set this instead of writing a file to disk.
	FileContent []byte

	// Overrides supplies the command-line override source as flag-name ->
	// value pairs, already parsed by whatever flag library the caller
	// chose. config never parses os.Args or imports a flag package itself.
	Overrides map[string]string
}

// environIsSet reports whether the caller supplied an explicit Environ,
// including an empty slice, as opposed to leaving it nil to request the real
// process environment.
func (o Options) environIsSet() bool {
	return o.Environ != nil
}
