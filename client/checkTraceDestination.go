//Package client - includes all required monitoring objects for the client
package client

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/aeden/traceroute"
	proto "github.com/denizaydin/nmon/api"
)

func CheckTraceDestination(tracedest *MonObject, c *NmonClient) {
	log := c.Logging
	log.Infof("tracer:%v starting with initial values:%v", tracedest.Object.GetTracedest(), tracedest.Object)
	options := traceroute.TracerouteOptions{}
	options.SetRetries(1)
	options.SetMaxHops(20)
	options.SetFirstHop(1) // Start from the default gw
	var ipAddr *net.IPAddr
	var err error
	for {
		ipAddr, err = net.ResolveIPAddr("ip", tracedest.Object.GetTracedest().GetDestination())
		log.Debugf("tracer:%v resolving destination", tracedest.Object.GetTracedest().GetDestination())
		if err != nil {
			log.Errorf("tracer:%v resolve error for tracedest:%v, retring in 3sec", err, tracedest.Object.GetTracedest().GetDestination())
			time.Sleep(3 * time.Second)
		} else {
			break
		}
	}
	interval := time.NewTimer(time.Duration(1 * time.Second))
	log.Debugf("tracer:%v: will start with in 1sec", tracedest.Object.GetTracedest().GetDestination())
	done := make(chan bool, 2)
	var waitGroup sync.WaitGroup
	var stream proto.Stats_RecordStatsClient
	log.Debugf("tracer:starting %v (%v), %v hops max, %v byte packets\n", tracedest.Object.GetTracedest().GetDestination(), ipAddr, options.MaxHops(), options.PacketSize())
	intstatschannel := make(chan traceroute.TracerouteHop, 0)
	go func() {
		defer waitGroup.Done()
		waitGroup.Add(1)
		for {
			select {
			case <-done:
				log.Tracef("tracer:%v: out from stats loop", tracedest.Object.GetTracedest().GetDestination())
				return
			case hop, ok := <-intstatschannel:
				if ok {
					log.Tracef("tracer:%v hop:%v", tracedest.Object.GetTracedest().GetDestination(), hop)
					if !c.IsStatsClientConnected {
						time.Sleep(1 * time.Second)
						log.Tracef("tracer:%v: stats server is not ready skipping", tracedest.Object.GetTracedest().GetDestination())
					} else {
						var streamerr error
						stream, streamerr = c.StatsConnClient.RecordStats(context.Background())
						if streamerr != nil {
							log.Errorf("tracer:%v: grpc stream failed while sending stats:%v", tracedest.Object.GetTracedest().GetDestination(), streamerr)
							time.Sleep(1 * time.Second)
						} else {
							stat := &proto.StatsObject{
								Client:    c.StatsClient,
								Timestamp: time.Now().UnixNano(),
								Object: &proto.StatsObject_Tracestat{
									Tracestat: &proto.TraceStat{
										Destination: tracedest.Object.GetTracedest().GetDestination(),
										HopIP:       fmt.Sprintf("%v.%v.%v.%v", hop.Address[0], hop.Address[1], hop.Address[2], hop.Address[3]),
										HopTTL:      int32(hop.TTL),
										HopRTT:      int32(hop.ElapsedTime),
									},
								},
							}
							log.Tracef("tracer:%v received stats:%v for resolve destination:%v", tracedest.Object.GetTracedest().GetDestination(), stat)
							if err := stream.Send(stat); err != nil {
								log.Errorf("tracer:%v: can not send client stats:%v, err:%v", tracedest.Object.GetTracedest().GetDestination(), stream, err)
							} else {
								log.Debugf("tracer:%v: send stats:%v", tracedest.Object.GetTracedest().GetDestination(), stat)
							}
						}
					}
				}
			}
		}
	}()
	exit := false
	for !exit {
		select {
		case <-tracedest.Notify:
			log.Infof("tracer:%v: tracer stop request", tracedest.Object.GetTracedest().GetDestination())
			close(intstatschannel)
			exit = true
		case <-interval.C:
			_, err = traceroute.Traceroute(tracedest.Object.GetTracedest().GetDestination(), &options, intstatschannel)
			if err != nil {
				log.Errorf("tracer:%v error for tracedest:%v", err, tracedest.Object.GetTracedest().GetDestination())
			}
			intstatschannel = make(chan traceroute.TracerouteHop, 0)
			interval = time.NewTimer(time.Duration(tracedest.Object.GetTracedest().Interval) * time.Millisecond)
		}
	}
	done <- true
	waitGroup.Wait()
	close(done)
	log.Infof("tracer:%v exiting", tracedest.Object.GetTracedest().GetDestination())
}
