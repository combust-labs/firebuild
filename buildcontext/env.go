package buildcontext

// --

type buildEnv interface {
	expand(string) string
	put(string, string) (string, string)
}

func newBuildEnv() buildEnv {
	return &defaultBuildEnv{env: map[string]string{}}
}

type defaultBuildEnv struct {
	env map[string]string
}

func (exp *defaultBuildEnv) expand(value string) string {
	return expand(value, exp.lookup)
}

func (exp *defaultBuildEnv) lookup(key string) (string, bool) {
	if value, ok := exp.env[key]; ok {
		return value, true
	}
	return "", false
}

func (exp *defaultBuildEnv) put(key, value string) (string, string) {
	value = exp.expand(value)
	exp.env[key] = value
	return key, value
}
