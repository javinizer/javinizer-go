package ssrf

import "net"

func SetLookupIPForTest(fn func(string) ([]net.IP, error)) func() {
	return setLookupIPForTest(fn)
}
