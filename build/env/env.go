package env

// BuildEnv is a build time environment representation.
type BuildEnv interface {
	Expand(string) string
	Put(string, string) (string, string)
	Snapshot() map[string]string
}

// NewBuildEnv returns an instance of the build environment.
func NewBuildEnv() BuildEnv {
	return &defaultBuildEnv{env: map[string]string{}}
}

type defaultBuildEnv struct {
	env map[string]string
}

func (exp *defaultBuildEnv) Expand(value string) string {
	return expand(value, exp.lookup)
}

func (exp *defaultBuildEnv) lookup(key string) (string, bool) {
	if value, ok := exp.env[key]; ok {
		return value, true
	}
	return "", false
}

func (exp *defaultBuildEnv) Put(key, value string) (string, string) {
	value = exp.Expand(value)
	exp.env[key] = value
	return key, value
}

func (exp *defaultBuildEnv) Snapshot() map[string]string {
	return exp.env
}
