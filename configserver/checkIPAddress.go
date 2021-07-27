package configserver

import "net"

// CheckIPAddress - This function implements net.ParseIP method may be overwritten to be faster and to include more checks like, multicast e.t.c
func CheckIPAddress(ip string) (r bool) {
	if net.ParseIP(ip) == nil {
		return true
	}
	return false
}
