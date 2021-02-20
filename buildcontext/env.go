package buildcontext

import "os"

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
	return os.Expand(value, exp.get)
}

func (exp *defaultBuildEnv) get(key string) string {
	if value, ok := exp.env[key]; ok {
		return value
	}
	return ""
}

func (exp *defaultBuildEnv) put(key, value string) (string, string) {
	value = exp.expand(value)
	exp.env[key] = value
	return key, value
}
