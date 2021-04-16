package fw

import (
	"fmt"
	"regexp"
	"strconv"

	"github.com/pkg/errors"
)

const defaultProtocol = "tcp"

// ExposedPort represents exposed port data used for iptables port publishing.
type ExposedPort interface {
	Interface() *string
	HostPort() int
	DestinationPort() int
	Protocol() string

	ToForwardRulespec(targetAddress string) []string
	ToNATRulespec(targetAddress string) []string
}

type defaultExposedPort struct {
	iface           *string
	hostPort        int
	destinationPort int
	protocol        string
}

// Interface returns the exposed interface or nil, if port should be exposed on all interfaces.
func (p *defaultExposedPort) Interface() *string {
	return p.iface
}

// HostPort returns the host port value.
func (p *defaultExposedPort) HostPort() int {
	return p.hostPort
}

// DestinationPort returns the guest destination port value.
func (p *defaultExposedPort) DestinationPort() int {
	return p.destinationPort
}

// Protocol returns the protocol value: tcp or udp.
func (p *defaultExposedPort) Protocol() string {
	return p.protocol
}

func (p *defaultExposedPort) toCommentValue() string {
	return fmt.Sprintf("firebuild:%s:%d:%d:/%s", func() string {
		if p.Interface() == nil {
			return "*"
		}
		return *p.Interface()
	}(), p.HostPort(), p.DestinationPort(), p.Protocol())
}

// ToForwardRulespec returns the forward chain rulespec for this port.
func (p *defaultExposedPort) ToForwardRulespec(targetAddress string) []string {
	rulespec := []string{"-m", "comment", "--comment", p.toCommentValue(), "-p", p.Protocol()}
	if p.Interface() != nil {
		rulespec = append(rulespec, "-i", *p.Interface())
	}
	return append(rulespec, "-d", targetAddress, "--dport", fmt.Sprintf("%d", p.HostPort()), "-m", "state", "--state", "NEW,ESTABLISHED,RELATED", "-j", "ACCEPT")
}

// ToNATRulespec returns the forward chain rulespec for this port.
func (p *defaultExposedPort) ToNATRulespec(targetAddress string) []string {
	rulespec := []string{"-m", "comment", "--comment", p.toCommentValue(), "-p", p.Protocol()}
	if p.Interface() != nil {
		rulespec = append(rulespec, "-i", *p.Interface())
	}
	return append(rulespec, "--dport", fmt.Sprintf("%d", p.HostPort()), "-j", "DNAT", "--to-destination", fmt.Sprintf("%s:%d", targetAddress, p.DestinationPort()))
}

var (
	extractionRegex = regexp.MustCompile("^((.[^:]*):)?((\\d{2,5}):)?(\\d{2,5})(\\/[a-z]{3})?$")
)

// ExposedPortFromString attempts to parse the input as an exposed port.
func ExposedPortFromString(input string) (ExposedPort, error) {
	matches := extractionRegex.FindAllStringSubmatch(input, -1)
	if len(matches) == 0 {
		return nil, fmt.Errorf("string is not a valid exposed port")
	}
	if len(matches[0]) == 0 {
		return nil, fmt.Errorf("string is not a valid exposed port")
	}

	values := matches[0]
	// skip first item because it's the full match:
	values = values[1:]
	// remove values ending with : and empty
	newValues := []string{}
	for _, v := range values {
		if v == "" || v[len(v)-1] == ':' {
			continue
		}
		newValues = append(newValues, v)
	}

	if len(newValues) == 4 {
		// interface:host-port:dest-port:protocol format
		intVal1, parseErr1 := parsedPortOrError(newValues[1])
		intVal2, parseErr2 := parsedPortOrError(newValues[2])
		if parseErr1 != nil || parseErr2 != nil { // both middle values must be valid port numbers
			return nil, fmt.Errorf("input %q cannot be parsed as exposed value", input)
		}
		if !validProtocol(newValues[3]) {
			return nil, fmt.Errorf("value %q is not a valid protocol", newValues[3])
		}
		return &defaultExposedPort{iface: pstring(newValues[0]), hostPort: intVal1, destinationPort: intVal2, protocol: newValues[3][1:]}, nil
	}

	if len(newValues) == 3 {
		// interface:host-port:dest-port format
		// or
		// host-port:dest-port:protocol format
		intVal1, parseErr1 := parsedPortOrError(newValues[0])
		intVal2, parseErr2 := parsedPortOrError(newValues[1])
		intVal3, parseErr3 := parsedPortOrError(newValues[2])

		if parseErr1 != nil { // expected interface:host-port:dest-port format
			if parseErr2 != nil || parseErr3 != nil { // but 1 and 2 failed as port values, no match
				return nil, fmt.Errorf("input %q cannot be parsed as exposed value", input)
			}
			return &defaultExposedPort{iface: pstring(newValues[0]), hostPort: intVal2, destinationPort: intVal3, protocol: defaultProtocol}, nil
		}

		if parseErr3 != nil { // expected host-port:dest-port:protocol format
			if parseErr1 != nil || parseErr2 != nil { // but 0 and 1 failed as port values, no match
				return nil, fmt.Errorf("input %q cannot be parsed as exposed value", input)
			}
			return &defaultExposedPort{iface: nil, hostPort: intVal1, destinationPort: intVal2, protocol: newValues[2][1:]}, nil
		}

		return nil, fmt.Errorf("input %q cannot be parsed as exposed value", input)

	}

	if len(newValues) == 2 {
		// interface:dest-port format
		// or
		// host-port:dest-port format
		// or
		// dest-port:protocol format

		intVal1, parseErr1 := parsedPortOrError(newValues[0])
		intVal2, parseErr2 := parsedPortOrError(newValues[1])

		if parseErr1 == nil && parseErr2 == nil { // host-port:dest-port format
			return &defaultExposedPort{iface: nil, hostPort: intVal1, destinationPort: intVal2, protocol: defaultProtocol}, nil
		}

		if parseErr1 != nil && parseErr2 == nil { // interface:dest-port format
			return &defaultExposedPort{iface: pstring(newValues[0]), hostPort: intVal2, destinationPort: intVal2, protocol: defaultProtocol}, nil
		}

		if parseErr1 == nil && parseErr2 != nil { // dest-port:protocol format
			if !validProtocol(newValues[1]) {
				return nil, fmt.Errorf("value %q is not a valid protocol", newValues[1])
			}
			return &defaultExposedPort{iface: nil, hostPort: intVal1, destinationPort: intVal1, protocol: newValues[1][1:]}, nil
		}

		// both errors are not nil, invalid state:
		return nil, fmt.Errorf("input %q cannot be parsed as exposed value", input)
	}

	if len(newValues) == 1 {
		// dest-port only:
		intVal, parseErr := parsedPortOrError(newValues[0])
		if parseErr != nil {
			return nil, parseErr
		}
		return &defaultExposedPort{iface: nil, hostPort: intVal, destinationPort: intVal, protocol: defaultProtocol}, nil
	}

	return nil, fmt.Errorf("input not valid")
}

func pstring(input string) *string {
	return &input
}

func parsedPortOrError(input string) (int, error) {
	intVal, parseErr := strconv.Atoi(input)
	if parseErr != nil {
		return 0, errors.Wrap(parseErr, "string is not a valid exposed port")
	}
	if !validPort(intVal) {
		return 0, fmt.Errorf("value %d is not a valid port", intVal)
	}
	return intVal, nil
}
func validPort(v int) bool {
	return v > 0 && v < 65535
}
func validProtocol(v string) bool {
	return v == "/tcp" || v == "/udp"
}
