package client

import (
	"context"
	"net"
	"time"

	proto "github.com/denizaydin/nmon/api"
)

//CheckResolveDestination - Send DNS Responce queries for the specified object with specified interval.
func CheckResolveDestination(resolvedest *MonObject, c *NmonClient) {
	var stream proto.Stats_RecordStatsClient
	c.Logging.Debugf("resolver:%v start with values:%v", resolvedest.Object.GetResolvedest(), resolvedest.Object)
	interval := time.NewTimer(time.Duration(1 * time.Second))
	c.Logging.Tracef("resolver:%v will start with in 1sec", resolvedest.Object.GetResolvedest().GetDestination())
	exit := false
	for !exit {
		select {
		case <-resolvedest.Notify:
			c.Logging.Debugf("resolver:%v received stop request", resolvedest.Object.GetResolvedest().GetDestination())
			exit = true
		case <-interval.C:
			resolvedest.ThreadupdateTime = time.Now().UnixNano()
			c.Logging.Tracef("resolver:%v interval:%v", resolvedest.Object.GetResolvedest().GetDestination(), time.Duration(resolvedest.Object.GetResolvedest().Interval)*time.Millisecond)
			if resolvedest.Object.GetResolvedest().ResolveServer != "" {
				c.Logging.Tracef("resolver:%v starting resolve using resolver:%v ", resolvedest.Object.GetResolvedest().GetDestination(), resolvedest.Object.GetResolvedest().ResolveServer)
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
				c.Logging.Tracef("resolver:%v sending req to resolver:%v", resolvedest.Object.GetResolvedest().GetDestination(), resolvedest.Object.GetResolvedest().GetResolveServer())
				ips, err := r.LookupHost(context.Background(), resolvedest.Object.GetResolvedest().GetDestination())
				diff := int64(-1)
				resolvedip := "unresolved"
				if err == nil {
					diff = time.Now().Sub(st).Milliseconds()
					c.Logging.Debugf("resolver:%v received response:%v from resolver:%v in:%v", resolvedest.Object.GetResolvedest().GetDestination(), ips[0], resolvedest.Object.GetResolvedest().ResolveServer, diff)
					resolvedip = ips[0]
				} else {
					c.Logging.Debugf("resolver:%v no reponse received from resolver:%v, err:%v", resolvedest.Object.GetResolvedest().GetDestination(), resolvedest.Object.GetResolvedest().GetResolveServer(), err)
				}
				stat := &proto.StatsObject{
					Client:    c.StatsClient,
					Timestamp: time.Now().UnixNano(),
					Object: &proto.StatsObject_Resolvestat{
						Resolvestat: &proto.ResolveStat{
							Destination: resolvedest.Object.GetResolvedest().GetDestination(),
							Rtt:         int32(diff),
							Resolvedip:  resolvedip,
							Resolver:    resolvedest.Object.GetResolvedest().GetResolveServer(),
						},
					},
				}
				if !c.IsStatsClientConnected {
					c.Logging.Tracef("resolver:%v stats server is not ready skipping", resolvedest.Object.GetResolvedest().GetDestination())
				} else {
					c.Logging.Tracef("resolver:%v received stats:%v", resolvedest.Object.GetResolvedest().GetDestination(), stat)
					var streamerr error
					stream, streamerr = c.StatsConnClient.RecordStats(context.Background())
					if streamerr != nil {
						c.Logging.Errorf("resolver:%v grpc stream failed while sending stats:%v", resolvedest.Object.GetResolvedest().GetDestination(), streamerr)
					} else {
						if err := stream.Send(stat); err != nil {
							c.Logging.Errorf("resolver:%v can not send client stats:%v, err:%v", resolvedest.Object.GetResolvedest().GetDestination(), stream, err)
						} else {
							c.Logging.Debugf("resolver:%v send stats:%v", resolvedest.Object.GetResolvedest().GetDestination(), stat)
						}
					}
				}
			} else {
				st := time.Now()
				c.Logging.Tracef("resolver:%v sending req to resolver:%v", resolvedest.Object.GetResolvedest().GetDestination(), resolvedest.Object.GetResolvedest().GetResolveServer())
				ips, err := net.LookupHost(resolvedest.Object.GetResolvedest().GetDestination())
				diff := int64(-1)
				resolvedip := "unresolved"
				if err == nil {
					diff = time.Now().Sub(st).Milliseconds()
					c.Logging.Debugf("resolver:%v received response:%v from resolver:%v in:%v", resolvedest.Object.GetResolvedest().GetDestination(), ips[0], resolvedest.Object.GetResolvedest().GetResolveServer(), diff)
					resolvedip = ips[0]
				} else {
					c.Logging.Debugf("resolver:%v no reponse received from resolver:%v, err:%v", resolvedest.Object.GetResolvedest().GetDestination(), resolvedest.Object.GetResolvedest().GetResolveServer(), err)
				}
				stat := &proto.StatsObject{
					Client:    c.StatsClient,
					Timestamp: time.Now().UnixNano(),
					Object: &proto.StatsObject_Resolvestat{
						Resolvestat: &proto.ResolveStat{
							Destination: resolvedest.Object.GetResolvedest().GetDestination(),
							Rtt:         int32(diff),
							Resolvedip:  resolvedip,
							Resolver:    "localhost",
						},
					},
				}
				c.Logging.Tracef("resolver:%v stats:%v", resolvedest.Object.GetResolvedest().GetDestination(), stat)
				if !c.IsStatsClientConnected {
					c.Logging.Tracef("resolver:%v stats server is not ready skipping", resolvedest.Object.GetResolvedest().GetDestination())
				} else {
					c.Logging.Tracef("resolver:%v received stats:%v", resolvedest.Object.GetResolvedest().GetDestination(), stat)
					var streamerr error
					stream, streamerr = c.StatsConnClient.RecordStats(context.Background())
					if streamerr != nil {
						c.Logging.Errorf("resolver:%v grpc stream failed while sending stats:%v", resolvedest.Object.GetResolvedest().GetDestination(), streamerr)
					} else {
						if err := stream.Send(stat); err != nil {
							c.Logging.Errorf("resolver:%v can not send client stats:%v, err:%v", resolvedest.Object.GetResolvedest().GetDestination(), stream, err)
						} else {
							c.Logging.Debugf("resolver:%v send stats:%v", resolvedest.Object.GetResolvedest().GetDestination(), stat)
						}
					}
				}
			}
			c.Logging.Tracef("resolver:%v setting interval to:%v", resolvedest.Object.GetResolvedest().GetDestination(), time.Duration(resolvedest.Object.GetResolvedest().Interval)*time.Millisecond)
			interval = time.NewTimer(time.Duration(resolvedest.Object.GetResolvedest().Interval) * time.Millisecond)
		}
	}
	c.Logging.Debugf("resolver:%v exiting", resolvedest.Object.GetResolvedest().GetDestination())
}
