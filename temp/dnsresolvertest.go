package main

import (
	"context"
	"fmt"
	"net"
	"time"
)

func main() {
	r := &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			d := net.Dialer{
				Timeout: time.Millisecond * time.Duration(10000),
			}
			return d.DialContext(ctx, network, "8.8.8.8:53")
		},
	}
	st := time.Now()
	ips, _ := r.LookupHost(context.Background(), "www.trendyol.com")
	rt := time.Now()
	diff := rt.Sub(st).Milliseconds()

	fmt.Printf("ip:%v, s:%v r:%v, d:%v\n", ips[0], st, rt, diff)

	st = time.Now()

	ips, _ = net.LookupHost("www.trendyol.com")
	rt = time.Now()
	diff = rt.Sub(st).Milliseconds()

	fmt.Printf("ip:%v, s:%v r:%v, d:%v\n", ips[0], st, rt, diff)

}
