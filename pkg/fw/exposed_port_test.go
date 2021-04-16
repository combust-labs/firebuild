package fw

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestExposedPortExtractFail(t *testing.T) {

	_, err1 := ExposedPortFromString("aaa:aaa:aaa:aaa:aaa:aaaa:16686a:16686/tcp")
	assert.NotNil(t, err1)

	_, err2 := ExposedPortFromString("eno1:16686:166867/tcp")
	assert.NotNil(t, err2)

	_, err3 := ExposedPortFromString("eno1:16686a:16686/tcp")
	assert.NotNil(t, err3)

	_, err4 := ExposedPortFromString("eno1:166867:16686/tcp")
	assert.NotNil(t, err4)

	_, err5 := ExposedPortFromString("eno1:16686:16686/definitelynot")
	assert.NotNil(t, err5)
}

func TestExposedPortExtractSuccess(t *testing.T) {

	ep, err1 := ExposedPortFromString("eno1:16686:16686/tcp")
	assert.Nil(t, err1)
	assert.NotNil(t, ep.Interface())
	assert.Equal(t, ep.HostPort(), 16686)
	assert.Equal(t, ep.DestinationPort(), 16686)
	assert.Equal(t, ep.Protocol(), defaultProtocol)

	assert.Equal(t, []string{"-m", "comment", "--comment", "firebuild:eno1:16686:16686:/tcp",
		"-p", "tcp", "-i", "eno1", "-d", "127.0.0.1", "--dport", "16686",
		"-m", "state", "--state", "NEW,ESTABLISHED,RELATED", "-j", "ACCEPT"}, ep.ToForwardRulespec("127.0.0.1"))
	assert.Equal(t, []string{"-m", "comment", "--comment", "firebuild:eno1:16686:16686:/tcp",
		"-p", "tcp", "-i", "eno1", "--dport", "16686",
		"-j", "DNAT", "--to-destination", "127.0.0.1:16686"}, ep.ToNATRulespec("127.0.0.1"))

	ep, err2 := ExposedPortFromString("eno1:16687:16686/tcp")
	assert.Nil(t, err2)
	assert.NotNil(t, ep.Interface())
	assert.Equal(t, ep.HostPort(), 16687)
	assert.Equal(t, ep.DestinationPort(), 16686)
	assert.Equal(t, ep.Protocol(), defaultProtocol)

	ep, err3 := ExposedPortFromString("eno1:16686:16687/udp")
	assert.Nil(t, err3)
	assert.NotNil(t, ep.Interface())
	assert.Equal(t, ep.HostPort(), 16686)
	assert.Equal(t, ep.DestinationPort(), 16687)
	assert.Equal(t, ep.Protocol(), "udp")

	ep, err4 := ExposedPortFromString("eno1:16686:16686")
	assert.Nil(t, err4)
	assert.NotNil(t, ep.Interface())
	assert.Equal(t, ep.HostPort(), 16686)
	assert.Equal(t, ep.DestinationPort(), 16686)
	assert.Equal(t, ep.Protocol(), "tcp")

	ep, err5 := ExposedPortFromString("16687:16686/udp")
	assert.Nil(t, err5)
	assert.Nil(t, ep.Interface())
	assert.Equal(t, ep.HostPort(), 16687)
	assert.Equal(t, ep.DestinationPort(), 16686)
	assert.Equal(t, ep.Protocol(), "udp")

	assert.Equal(t, []string{"-m", "comment", "--comment", "firebuild:*:16687:16686:/udp",
		"-p", "udp", "-d", "127.0.0.1", "--dport", "16687",
		"-m", "state", "--state", "NEW,ESTABLISHED,RELATED", "-j", "ACCEPT"}, ep.ToForwardRulespec("127.0.0.1"))
	assert.Equal(t, []string{"-m", "comment", "--comment", "firebuild:*:16687:16686:/udp",
		"-p", "udp", "--dport", "16687",
		"-j", "DNAT", "--to-destination", "127.0.0.1:16686"}, ep.ToNATRulespec("127.0.0.1"))

	ep, err6 := ExposedPortFromString("eno1:16686")
	assert.Nil(t, err6)
	assert.NotNil(t, ep.Interface())
	assert.Equal(t, ep.HostPort(), 16686)
	assert.Equal(t, ep.DestinationPort(), 16686)
	assert.Equal(t, ep.Protocol(), defaultProtocol)

	ep, err7 := ExposedPortFromString("16687:16686")
	assert.Nil(t, err7)
	assert.Nil(t, ep.Interface())
	assert.Equal(t, ep.HostPort(), 16687)
	assert.Equal(t, ep.DestinationPort(), 16686)
	assert.Equal(t, ep.Protocol(), defaultProtocol)

	ep, err8 := ExposedPortFromString("16686/udp")
	assert.Nil(t, err8)
	assert.Nil(t, ep.Interface())
	assert.Equal(t, ep.HostPort(), 16686)
	assert.Equal(t, ep.DestinationPort(), 16686)
	assert.Equal(t, ep.Protocol(), "udp")

	ep, err9 := ExposedPortFromString("16686")
	assert.Nil(t, err9)
	assert.Nil(t, ep.Interface())
	assert.Equal(t, ep.HostPort(), 16686)
	assert.Equal(t, ep.DestinationPort(), 16686)
	assert.Equal(t, ep.Protocol(), defaultProtocol)

}
