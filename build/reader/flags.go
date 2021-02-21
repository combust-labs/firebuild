package reader

import "strings"

type dockerfileFlags interface {
	get(string) (string, bool)
	getOrDefault(string, string) string
}

type dockerfileDefaultFlags struct {
	kvs map[string]string
}

func (fs dockerfileDefaultFlags) get(key string) (string, bool) {
	v, ok := fs.kvs[key]
	return v, ok
}

func (fs dockerfileDefaultFlags) getOrDefault(key, def string) string {
	if v, ok := fs.kvs[key]; ok {
		return v
	}
	return def
}

func readFlags(input []string) dockerfileFlags {
	output := &dockerfileDefaultFlags{
		kvs: map[string]string{},
	}
	for _, f := range input {
		if f[0:2] == "--" {
			parts := strings.Split(f, "=")
			if len(parts) > 1 {
				output.kvs[parts[0]] = strings.Join(parts[1:], "=")
			}
		}
	}
	return output
}
