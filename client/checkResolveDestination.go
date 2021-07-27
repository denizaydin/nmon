package client

import (
	"context"
	"net"
	"sync"
	"time"

	proto "dnzydn.com/nmon/api"
)

//CheckResolveDestination - Send DNS Responce queries for the specified object with specified interval.
func CheckResolveDestination(resolvedest *MonObject, c *NmonClient) {
	log := c.Logging
	intstatschannel := make(chan *proto.StatsObject, 100)
	done := make(chan bool, 2)
	var waitGroup sync.WaitGroup
	var stream proto.Stats_RecordStatsClient
	c.Logging.Infof("resolver:%v: start with values:%v", resolvedest.Object.GetResolvedest(), resolvedest.Object)
	go func() {
		defer waitGroup.Done()
		waitGroup.Add(1)
		var lastStatTime int64
		for {
			select {
			case <-done:
				c.Logging.Tracef("resolver:%v: out from stats loop", resolvedest.Object.GetResolvedest().GetDestination())
				return
			default:
				stat := <-intstatschannel
				if lastStatTime == stat.GetTimestamp() {
					continue
				}
				lastStatTime = stat.GetTimestamp()
				c.Logging.Tracef("resolver:%v received stats:%v for resolve destination:%v", resolvedest.Object.GetResolvedest().GetDestination(), stat)
				if !c.IsStatsClientConnected {
					time.Sleep(1 * time.Second)
					c.Logging.Tracef("resolver:%v: stats server is not ready skipping", resolvedest.Object.GetResolvedest().GetDestination())
					continue
				}
				var streamerr error
				stream, streamerr = c.StatsConnClient.RecordStats(context.Background())
				if streamerr != nil {
					c.Logging.Errorf("resolver:%v: grpc stream failed while sending stats:%v", resolvedest.Object.GetResolvedest().GetDestination(), streamerr)
					time.Sleep(1 * time.Second)
					continue
				}
				if err := stream.Send(stat); err != nil {
					c.Logging.Errorf("resolver:%v: can not send client stats:%v, err:%v", resolvedest.Object.GetResolvedest().GetDestination(), stream, err)
					break
				}
				c.Logging.Debugf("resolver:%v: send stats:%v", resolvedest.Object.GetResolvedest().GetDestination(), stat)

			}
		}
	}()
loop:
	for {
		select {
		case <-resolvedest.Notify:
			log.Infof("resolver:%v: received stop request", resolvedest.Object.GetResolvedest().GetDestination())
			close(intstatschannel)
			break loop // exit
		default:
			if resolvedest.Object.GetResolvedest().ResolveServer != "" {
				log.Tracef("resolver:%v: starting resolve using resolver:%v ", resolvedest.Object.GetResolvedest().GetDestination(), resolvedest.Object.GetResolvedest().ResolveServer)
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
				log.Tracef("resolver:%v: sending req to resolver:%v", resolvedest.Object.GetResolvedest().GetDestination(), resolvedest.Object.GetResolvedest().GetResolveServer())
				ips, err := r.LookupHost(context.Background(), resolvedest.Object.GetResolvedest().GetDestination())
				diff := int64(-1)
				if err == nil {
					diff = time.Now().Sub(st).Milliseconds()
					log.Debugf("resolver:%v: received response:%v from resolver:%v in:%v", resolvedest.Object.GetResolvedest().GetDestination(), ips[0], resolvedest.Object.GetResolvedest().ResolveServer, diff)

				} else {
					log.Debugf("resolver:%v: no reponse received from resolver:%v, err:%v", resolvedest.Object.GetResolvedest().GetDestination(), resolvedest.Object.GetResolvedest().GetResolveServer(), err)
				}
				stat := &proto.StatsObject{
					Client:    c.StatsClient,
					Timestamp: time.Now().UnixNano(),
					Object: &proto.StatsObject_Resolvestat{
						Resolvestat: &proto.ResolveStat{
							Destination: resolvedest.Object.GetResolvedest().GetDestination(),
							Rtt:         int32(diff),
							Resolvedip:  ips[0],
							Resolver:    resolvedest.Object.GetResolvedest().GetResolveServer(),
						},
					},
				}
				intstatschannel <- stat
			} else {
				st := time.Now()
				log.Tracef("resolver:%v: sending req to resolver:%v", resolvedest.Object.GetResolvedest().GetDestination(), resolvedest.Object.GetResolvedest().GetResolveServer())
				ips, err := net.LookupHost(resolvedest.Object.GetResolvedest().GetDestination())
				diff := int64(-1)
				if err == nil {
					diff = time.Now().Sub(st).Milliseconds()
					log.Debugf("resolver:%v: received response:%v from resolver:%v in:%v", resolvedest.Object.GetResolvedest().GetDestination(), ips[0], resolvedest.Object.GetResolvedest().GetResolveServer(), diff)
				} else {
					log.Debugf("resolver:%v: no reponse received from resolver:%v, err:%v", resolvedest.Object.GetResolvedest().GetDestination(), resolvedest.Object.GetResolvedest().GetResolveServer(), err)
				}
				stat := &proto.StatsObject{
					Client:    c.StatsClient,
					Timestamp: time.Now().UnixNano(),
					Object: &proto.StatsObject_Resolvestat{
						Resolvestat: &proto.ResolveStat{
							Destination: resolvedest.Object.GetResolvedest().GetDestination(),
							Rtt:         int32(diff),
							Resolver:    "localhost",
						},
					},
				}
				log.Tracef("resolver:%v: stats:%v", resolvedest.Object.GetResolvedest().GetDestination(), stat)
				intstatschannel <- stat
			}
		}
		log.Tracef("resolver:%v: sleeping for:%v", resolvedest.Object.GetResolvedest().GetDestination(), time.Duration(resolvedest.Object.GetResolvedest().Interval)*time.Millisecond)
		time.Sleep(time.Duration(resolvedest.Object.GetResolvedest().Interval) * time.Millisecond)
		log.Tracef("resolver:%v: waked from sleeping for:%v", resolvedest.Object.GetResolvedest().GetDestination(), time.Duration(resolvedest.Object.GetResolvedest().Interval)*time.Millisecond)
	}
	done <- true
	waitGroup.Wait()
	close(done)
	log.Infof("resolver:%v: stopped", resolvedest.Object.GetResolvedest().GetDestination())
}
