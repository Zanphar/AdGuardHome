//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris
// +build aix darwin dragonfly freebsd linux netbsd openbsd solaris

package dhcpd

import (
	"bytes"
	"net"
	"testing"

	"github.com/AdguardTeam/golibs/netutil"
	"github.com/insomniacslk/dhcp/dhcpv4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func notify4(flags uint32) {
}

func TestV4_AddRemove_static(t *testing.T) {
	s, err := v4Create(V4ServerConf{
		Enabled:    true,
		RangeStart: net.IP{192, 168, 10, 100},
		RangeEnd:   net.IP{192, 168, 10, 200},
		GatewayIP:  net.IP{192, 168, 10, 1},
		SubnetMask: net.IP{255, 255, 255, 0},
		notify:     notify4,
	})
	require.NoError(t, err)

	ls := s.GetLeases(LeasesStatic)
	assert.Empty(t, ls)

	// Add static lease.
	l := &Lease{
		Hostname: "static-1.local",
		HWAddr:   net.HardwareAddr{0xAA, 0xAA, 0xAA, 0xAA, 0xAA, 0xAA},
		IP:       net.IP{192, 168, 10, 150},
	}

	err = s.AddStaticLease(l)
	require.NoError(t, err)

	err = s.AddStaticLease(l)
	assert.Error(t, err)

	ls = s.GetLeases(LeasesStatic)
	require.Len(t, ls, 1)

	assert.True(t, l.IP.Equal(ls[0].IP))
	assert.Equal(t, l.HWAddr, ls[0].HWAddr)
	assert.True(t, ls[0].IsStatic())

	// Try to remove static lease.
	err = s.RemoveStaticLease(&Lease{
		IP:     net.IP{192, 168, 10, 110},
		HWAddr: net.HardwareAddr{0xAA, 0xAA, 0xAA, 0xAA, 0xAA, 0xAA},
	})
	assert.Error(t, err)

	// Remove static lease.
	err = s.RemoveStaticLease(l)
	require.NoError(t, err)
	ls = s.GetLeases(LeasesStatic)
	assert.Empty(t, ls)
}

func TestV4_AddReplace(t *testing.T) {
	sIface, err := v4Create(V4ServerConf{
		Enabled:    true,
		RangeStart: net.IP{192, 168, 10, 100},
		RangeEnd:   net.IP{192, 168, 10, 200},
		GatewayIP:  net.IP{192, 168, 10, 1},
		SubnetMask: net.IP{255, 255, 255, 0},
		notify:     notify4,
	})
	require.NoError(t, err)

	s, ok := sIface.(*v4Server)
	require.True(t, ok)

	dynLeases := []Lease{{
		Hostname: "dynamic-1.local",
		HWAddr:   net.HardwareAddr{0x11, 0xAA, 0xAA, 0xAA, 0xAA, 0xAA},
		IP:       net.IP{192, 168, 10, 150},
	}, {
		Hostname: "dynamic-2.local",
		HWAddr:   net.HardwareAddr{0x22, 0xAA, 0xAA, 0xAA, 0xAA, 0xAA},
		IP:       net.IP{192, 168, 10, 151},
	}}

	for i := range dynLeases {
		err = s.addLease(&dynLeases[i])
		require.NoError(t, err)
	}

	stLeases := []*Lease{{
		Hostname: "static-1.local",
		HWAddr:   net.HardwareAddr{0x33, 0xAA, 0xAA, 0xAA, 0xAA, 0xAA},
		IP:       net.IP{192, 168, 10, 150},
	}, {
		Hostname: "static-2.local",
		HWAddr:   net.HardwareAddr{0x22, 0xAA, 0xAA, 0xAA, 0xAA, 0xAA},
		IP:       net.IP{192, 168, 10, 152},
	}}

	for _, l := range stLeases {
		err = s.AddStaticLease(l)
		require.NoError(t, err)
	}

	ls := s.GetLeases(LeasesStatic)
	require.Len(t, ls, 2)

	for i, l := range ls {
		assert.True(t, stLeases[i].IP.Equal(l.IP))
		assert.Equal(t, stLeases[i].HWAddr, l.HWAddr)
		assert.True(t, l.IsStatic())
	}
}

func TestV4StaticLease_Get(t *testing.T) {
	var err error
	sIface, err := v4Create(V4ServerConf{
		Enabled:    true,
		RangeStart: net.IP{192, 168, 10, 100},
		RangeEnd:   net.IP{192, 168, 10, 200},
		GatewayIP:  net.IP{192, 168, 10, 1},
		SubnetMask: net.IP{255, 255, 255, 0},
		notify:     notify4,
	})
	require.NoError(t, err)

	s, ok := sIface.(*v4Server)
	require.True(t, ok)

	s.conf.dnsIPAddrs = []net.IP{{192, 168, 10, 1}}

	l := &Lease{
		Hostname: "static-1.local",
		HWAddr:   net.HardwareAddr{0xAA, 0xAA, 0xAA, 0xAA, 0xAA, 0xAA},
		IP:       net.IP{192, 168, 10, 150},
	}
	err = s.AddStaticLease(l)
	require.NoError(t, err)

	var req, resp *dhcpv4.DHCPv4
	mac := net.HardwareAddr{0xAA, 0xAA, 0xAA, 0xAA, 0xAA, 0xAA}

	t.Run("discover", func(t *testing.T) {
		req, err = dhcpv4.NewDiscovery(mac, dhcpv4.WithRequestedOptions(
			dhcpv4.OptionDomainNameServer,
		))
		require.NoError(t, err)

		resp, err = dhcpv4.NewReplyFromRequest(req)
		require.NoError(t, err)

		assert.Equal(t, 1, s.process(req, resp))
	})

	// Don't continue if we got any errors in the previous subtest.
	require.NoError(t, err)

	t.Run("offer", func(t *testing.T) {
		assert.Equal(t, dhcpv4.MessageTypeOffer, resp.MessageType())
		assert.Equal(t, mac, resp.ClientHWAddr)
		assert.True(t, l.IP.Equal(resp.YourIPAddr))
		assert.True(t, s.conf.GatewayIP.Equal(resp.Router()[0]))
		assert.True(t, s.conf.GatewayIP.Equal(resp.ServerIdentifier()))
		assert.Equal(t, s.conf.subnet.Mask, resp.SubnetMask())
		assert.Equal(t, s.conf.leaseTime.Seconds(), resp.IPAddressLeaseTime(-1).Seconds())
	})

	t.Run("request", func(t *testing.T) {
		req, err = dhcpv4.NewRequestFromOffer(resp)
		require.NoError(t, err)

		resp, err = dhcpv4.NewReplyFromRequest(req)
		require.NoError(t, err)

		assert.Equal(t, 1, s.process(req, resp))
	})

	require.NoError(t, err)

	t.Run("ack", func(t *testing.T) {
		assert.Equal(t, dhcpv4.MessageTypeAck, resp.MessageType())
		assert.Equal(t, mac, resp.ClientHWAddr)
		assert.True(t, l.IP.Equal(resp.YourIPAddr))
		assert.True(t, s.conf.GatewayIP.Equal(resp.Router()[0]))
		assert.True(t, s.conf.GatewayIP.Equal(resp.ServerIdentifier()))
		assert.Equal(t, s.conf.subnet.Mask, resp.SubnetMask())
		assert.Equal(t, s.conf.leaseTime.Seconds(), resp.IPAddressLeaseTime(-1).Seconds())
	})

	dnsAddrs := resp.DNS()
	require.Len(t, dnsAddrs, 1)

	assert.True(t, s.conf.GatewayIP.Equal(dnsAddrs[0]))

	t.Run("check_lease", func(t *testing.T) {
		ls := s.GetLeases(LeasesStatic)
		require.Len(t, ls, 1)

		assert.True(t, l.IP.Equal(ls[0].IP))
		assert.Equal(t, mac, ls[0].HWAddr)
	})
}

func TestV4DynamicLease_Get(t *testing.T) {
	var err error
	sIface, err := v4Create(V4ServerConf{
		Enabled:    true,
		RangeStart: net.IP{192, 168, 10, 100},
		RangeEnd:   net.IP{192, 168, 10, 200},
		GatewayIP:  net.IP{192, 168, 10, 1},
		SubnetMask: net.IP{255, 255, 255, 0},
		notify:     notify4,
		Options: []string{
			"81 hex 303132",
			"82 ip 1.2.3.4",
		},
	})
	require.NoError(t, err)

	s, ok := sIface.(*v4Server)
	require.True(t, ok)

	s.conf.dnsIPAddrs = []net.IP{{192, 168, 10, 1}}

	var req, resp *dhcpv4.DHCPv4
	mac := net.HardwareAddr{0xAA, 0xAA, 0xAA, 0xAA, 0xAA, 0xAA}

	t.Run("discover", func(t *testing.T) {
		req, err = dhcpv4.NewDiscovery(mac, dhcpv4.WithRequestedOptions(
			dhcpv4.OptionFQDN,
			dhcpv4.OptionRelayAgentInformation,
		))
		require.NoError(t, err)

		resp, err = dhcpv4.NewReplyFromRequest(req)
		require.NoError(t, err)

		assert.Equal(t, 1, s.process(req, resp))
	})

	// Don't continue if we got any errors in the previous subtest.
	require.NoError(t, err)

	t.Run("offer", func(t *testing.T) {
		assert.Equal(t, dhcpv4.MessageTypeOffer, resp.MessageType())
		assert.Equal(t, mac, resp.ClientHWAddr)

		assert.Equal(t, s.conf.RangeStart, resp.YourIPAddr)
		assert.Equal(t, s.conf.GatewayIP, resp.ServerIdentifier())

		router := resp.Router()
		require.Len(t, router, 1)

		assert.Equal(t, s.conf.GatewayIP, router[0])

		assert.Equal(t, s.conf.subnet.Mask, resp.SubnetMask())
		assert.Equal(t, s.conf.leaseTime.Seconds(), resp.IPAddressLeaseTime(-1).Seconds())
		assert.Equal(t, []byte("012"), resp.Options.Get(dhcpv4.OptionFQDN))

		rai := resp.RelayAgentInfo()
		require.NotNil(t, rai)
		assert.Equal(t, net.IP{1, 2, 3, 4}, net.IP(rai.ToBytes()))
	})

	t.Run("request", func(t *testing.T) {
		req, err = dhcpv4.NewRequestFromOffer(resp)
		require.NoError(t, err)

		resp, err = dhcpv4.NewReplyFromRequest(req)
		require.NoError(t, err)

		assert.Equal(t, 1, s.process(req, resp))
	})

	require.NoError(t, err)

	t.Run("ack", func(t *testing.T) {
		assert.Equal(t, dhcpv4.MessageTypeAck, resp.MessageType())
		assert.Equal(t, mac, resp.ClientHWAddr)
		assert.True(t, s.conf.RangeStart.Equal(resp.YourIPAddr))

		router := resp.Router()
		require.Len(t, router, 1)

		assert.Equal(t, s.conf.GatewayIP, router[0])

		assert.True(t, s.conf.GatewayIP.Equal(resp.ServerIdentifier()))
		assert.Equal(t, s.conf.subnet.Mask, resp.SubnetMask())
		assert.Equal(t, s.conf.leaseTime.Seconds(), resp.IPAddressLeaseTime(-1).Seconds())
	})

	dnsAddrs := resp.DNS()
	require.Len(t, dnsAddrs, 1)

	assert.True(t, net.IP{192, 168, 10, 1}.Equal(dnsAddrs[0]))

	// check lease
	t.Run("check_lease", func(t *testing.T) {
		ls := s.GetLeases(LeasesDynamic)
		require.Len(t, ls, 1)

		assert.True(t, net.IP{192, 168, 10, 100}.Equal(ls[0].IP))
		assert.Equal(t, mac, ls[0].HWAddr)
	})
}

func TestNormalizeHostname(t *testing.T) {
	testCases := []struct {
		name       string
		hostname   string
		wantErrMsg string
		want       string
	}{{
		name:       "success",
		hostname:   "example.com",
		wantErrMsg: "",
		want:       "example.com",
	}, {
		name:       "success_empty",
		hostname:   "",
		wantErrMsg: "",
		want:       "",
	}, {
		name:       "success_spaces",
		hostname:   "my device 01",
		wantErrMsg: "",
		want:       "my-device-01",
	}, {
		name:       "success_underscores",
		hostname:   "my_device_01",
		wantErrMsg: "",
		want:       "my-device-01",
	}, {
		name:       "error_part",
		hostname:   "device !!!",
		wantErrMsg: "",
		want:       "device",
	}, {
		name:       "error_part_spaces",
		hostname:   "device ! ! !",
		wantErrMsg: "",
		want:       "device",
	}, {
		name:       "error",
		hostname:   "!!!",
		wantErrMsg: `normalizing "!!!": no valid parts`,
		want:       "",
	}, {
		name:       "error_spaces",
		hostname:   "! ! !",
		wantErrMsg: `normalizing "! ! !": no valid parts`,
		want:       "",
	}}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := normalizeHostname(tc.hostname)
			if tc.wantErrMsg == "" {
				assert.NoError(t, err)
			} else {
				require.Error(t, err)

				assert.Equal(t, tc.wantErrMsg, err.Error())
			}

			assert.Equal(t, tc.want, got)
		})
	}
}

// fakePacketConn is a mock implementation of net.PacketConn to simplify
// testing.
type fakePacketConn struct {
	// writeTo is used to substitute net.PacketConn's WriteTo method.
	writeTo func(p []byte, addr net.Addr) (n int, err error)
	// net.PacketConn is embedded here simply to make *fakePacketConn a
	// net.PacketConn without actually implementing all methods.
	net.PacketConn
}

// WriteTo implements net.PacketConn interface for *fakePacketConn.
func (fc *fakePacketConn) WriteTo(p []byte, addr net.Addr) (n int, err error) {
	return fc.writeTo(p, addr)
}

func TestV4Server_Send_unicast(t *testing.T) {
	b := &bytes.Buffer{}
	var peer *net.UDPAddr

	conn := &fakePacketConn{
		writeTo: func(p []byte, addr net.Addr) (n int, err error) {
			udpPeer, ok := addr.(*net.UDPAddr)
			require.True(t, ok)

			peer = cloneUDPAddr(udpPeer)

			n, err = b.Write(p)
			require.NoError(t, err)

			return n, nil
		},
	}

	defaultPeer := &net.UDPAddr{
		IP: net.IP{1, 2, 3, 4},
		// Use neither client nor server port.
		Port: 1234,
	}
	defaultResp := &dhcpv4.DHCPv4{
		OpCode: dhcpv4.OpcodeBootReply,
	}
	s := &v4Server{}

	testCases := []struct {
		name     string
		req      *dhcpv4.DHCPv4
		wantPeer net.Addr
	}{{
		name: "relay_agent",
		req: &dhcpv4.DHCPv4{
			GatewayIPAddr: defaultPeer.IP,
		},
		wantPeer: &net.UDPAddr{
			IP:   defaultPeer.IP,
			Port: dhcpv4.ServerPort,
		},
	}, {
		name: "known_client",
		req: &dhcpv4.DHCPv4{
			GatewayIPAddr: netutil.IPv4Zero(),
			ClientIPAddr:  net.IP{2, 3, 4, 5},
		},
		wantPeer: &net.UDPAddr{
			IP:   net.IP{2, 3, 4, 5},
			Port: dhcpv4.ClientPort,
		},
	}}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			s.send(defaultPeer, conn, tc.req, defaultResp)
			assert.EqualValues(t, defaultResp.ToBytes(), b.Bytes())
			assert.Equal(t, tc.wantPeer, peer)
		})

		b.Reset()
		peer = nil
	}
}
