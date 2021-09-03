package client

import (
	"context"
	"net"
	"time"

	proto "github.com/denizaydin/nmon/api"
)

//CheckResolveDestination - Send DNS Responce queries for the specified object with specified interval.
func CheckResolveDestination(resolvedest *MonObject, c *NmonClient) {
	c.Logging.Debugf("resolver:%v start with values:%v", resolvedest.Object.GetResolvedest(), resolvedest.Object)
	c.Logging.Tracef("resolver:%v will start with in 1sec", resolvedest.Object.GetResolvedest().GetName())
	exit := false
	for !exit {
		select {
		case <-resolvedest.Notify:
			c.Logging.Debugf("resolver:%v received stop request", resolvedest.Object.GetResolvedest().GetName())
			exit = true
		default:
			resolvedest.ThreadupdateTime = time.Now().UnixNano()
			c.Logging.Tracef("resolver:%v interval:%v, threadupdatetime:%v", resolvedest.Object.GetResolvedest().GetName(), time.Duration(resolvedest.Object.GetResolvedest().Interval)*time.Millisecond, resolvedest.ThreadupdateTime)
			if resolvedest.Object.GetResolvedest().ResolveServer != "" {
				c.Logging.Tracef("resolver:%v starting resolve using resolver:%v ", resolvedest.Object.GetResolvedest().GetName(), resolvedest.Object.GetResolvedest().ResolveServer)
				r := &net.Resolver{
					PreferGo: true,
					Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
						d := net.Dialer{
							Timeout: time.Millisecond * time.Duration(10000),
						}
						return d.DialContext(ctx, network, resolvedest.Object.GetResolvedest().ResolveServer)
					},
				}
				st := time.Now()
				c.Logging.Tracef("resolver:%v sending req to resolver:%v", resolvedest.Object.GetResolvedest().GetName(), resolvedest.Object.GetResolvedest().GetResolveServer())
				ips, err := r.LookupHost(context.Background(), resolvedest.Object.GetResolvedest().GetName())
				diff := int64(-1)
				resolvedip := "unresolved"
				if err == nil {
					diff = time.Now().Sub(st).Milliseconds()
					c.Logging.Debugf("resolver:%v received response:%v from resolver:%v in:%v", resolvedest.Object.GetResolvedest().GetName(), ips[0], resolvedest.Object.GetResolvedest().ResolveServer, diff)
					resolvedip = ips[0]
				} else {
					c.Logging.Debugf("resolver:%v no reponse received from resolver:%v, err:%v", resolvedest.Object.GetResolvedest().GetName(), resolvedest.Object.GetResolvedest().GetResolveServer(), err)
				}
				stat := &proto.StatsObject{
					Client:    c.StatsClient,
					Timestamp: time.Now().UnixNano(),
					Object: &proto.StatsObject_Resolvestat{
						Resolvestat: &proto.ResolveStat{
							Destination: resolvedest.Object.GetResolvedest().GetName(),
							Rtt:         int32(diff),
							Resolvedip:  resolvedip,
							Resolver:    resolvedest.Object.GetResolvedest().GetResolveServer(),
						},
					},
				}
				c.Statschannel <- stat
				c.Logging.Debugf("resolver:%v send stats:%v", resolvedest.Object.GetResolvedest().GetName(), stat)
			} else {
				st := time.Now()
				c.Logging.Tracef("resolver:%v sending req to resolver:%v", resolvedest.Object.GetResolvedest().GetName(), resolvedest.Object.GetResolvedest().GetResolveServer())
				ips, err := net.LookupHost(resolvedest.Object.GetResolvedest().GetName())
				diff := int64(-1)
				resolvedip := "unresolved"
				if err == nil {
					diff = time.Now().Sub(st).Milliseconds()
					c.Logging.Debugf("resolver:%v received response:%v from resolver:%v in:%v", resolvedest.Object.GetResolvedest().GetName(), ips[0], resolvedest.Object.GetResolvedest().GetResolveServer(), diff)
					resolvedip = ips[0]
				} else {
					c.Logging.Debugf("resolver:%v no reponse received from resolver:%v, err:%v", resolvedest.Object.GetResolvedest().GetName(), resolvedest.Object.GetResolvedest().GetResolveServer(), err)
				}
				stat := &proto.StatsObject{
					Client:    c.StatsClient,
					Timestamp: time.Now().UnixNano(),
					Object: &proto.StatsObject_Resolvestat{
						Resolvestat: &proto.ResolveStat{
							Destination: resolvedest.Object.GetResolvedest().GetName(),
							Rtt:         int32(diff),
							Resolvedip:  resolvedip,
							Resolver:    "localhost",
						},
					},
				}
				c.Statschannel <- stat
				c.Logging.Debugf("resolver:%v send stats:%v", resolvedest.Object.GetResolvedest().GetName(), stat)

			}
			c.Logging.Tracef("resolver:%v setting interval to:%v", resolvedest.Object.GetResolvedest().GetName(), time.Duration(resolvedest.Object.GetResolvedest().Interval)*time.Millisecond)
			time.Sleep(time.Duration(resolvedest.Object.GetResolvedest().Interval) * time.Millisecond)
		}
	}
	c.Logging.Debugf("resolver:%v exiting", resolvedest.Object.GetResolvedest().GetName())
}
