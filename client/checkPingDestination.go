//Package client - includes all required monitoring objects for the client
package client

import (
	"context"
	"time"

	proto "github.com/denizaydin/nmon/api"
	"github.com/denizaydin/ping"
)

//CheckPingDestination - send continous pings
func CheckPingDestination(pingdest *MonObject, c *NmonClient) {
	log := c.Logging
	pinger, err := ping.NewPinger(pingdest.Object.GetPingdest().Destination)
	c.Logging.Infof("pinger:%v start with values:%v", pingdest.Object.GetPingdest().GetDestination(), pingdest.Object.GetPingdest())
	if err != nil {
		log.Errorf("pinger:%v, can not created pinger, err:%v", pingdest.Object.GetPingdest().GetDestination(), err.Error())
		return
	}
	pinger.SetLogger(log)
	pinger.OnRecv = func(pkt *ping.Packet) {
		stat := &proto.StatsObject{
			Client:    c.StatsClient,
			Timestamp: time.Now().UnixNano(),
			Object: &proto.StatsObject_Pingstat{
				Pingstat: &proto.PingStat{
					Destination: pingdest.Object.GetPingdest().GetDestination(),
					Rtt:         int32(pkt.Rtt.Milliseconds()),
				},
			},
		}
		if !c.IsStatsClientConnected {
			c.Logging.Tracef("pinger:stats server is not ready, skipping")
		} else {
			c.Logging.Tracef("pinger:received stats:%v for ping destination:%v", stat, pingdest.Object.GetPingdest().GetDestination())
			stream, err := c.StatsConnClient.RecordStats(context.Background())
			if err != nil {
				c.Logging.Errorf("pinger:grpc stream failed while sending stats:%v", err)
			} else {
				if err := stream.Send(stat); err != nil {
					c.Logging.Errorf("pinger:can not send client stats:%v, err:%v", stream, err)
				}
				c.Logging.Tracef("pinger:sent stats:%v for ping destination:%v", stat, pingdest.Object.GetPingdest().GetDestination())
			}
		}
		log.Tracef("pinger:ping:%v -> %d bytes from %s: icmp_seq=%d time=%v ttl=%v\n", pingdest.Object.GetPingdest().GetDestination(),
			pkt.Nbytes, pkt.IPAddr, pkt.Seq, pkt.Rtt, pkt.Ttl)
	}
	//Setting count to -1, contionous ping.
	//pinger.Count = -1 time.Duration(pingdest.Timeout) also we need to set timeout
	//Do we need to check packet size for more than jumbo?
	packetsize := int(pingdest.Object.GetPingdest().PacketSize)
	if packetsize < 64 {
		log.Warnf("pinger:packetsize:%v is lower than 64byte for dest:%v, setting it to 64byte", packetsize, pingdest.Object.GetPingdest().GetDestination())
		packetsize = 64
	}
	//pinger.Size = &packetsize
	pinger.Interval = &pingdest.Object.GetPingdest().Interval
	pinger.SetPrivileged(true)
	log.Infof("pinger:pinging to target host:%s with size %s and interval %s", pingdest.Object.GetPingdest().GetDestination(), packetsize, pingdest.Object.GetPingdest().Interval)
	go func() {
		for {
			select {
			case <-pingdest.Notify:
				log.Infof("pinger:%v is stopped", pingdest.Object.GetPingdest().GetDestination())
				pinger.Stop()
			}
			time.Sleep(time.Duration(pingdest.Object.GetPingdest().Interval) * time.Millisecond)
		}
	}()
	err = pinger.Run()
	if err != nil {
		log.Errorf("pinger:failed start ping to target host:%s, err:%s", pingdest.Object.GetPingdest().GetDestination(), err)
	}
	log.Infof("pinger:exiting from pinging to target host:%s with size %s and interval %s", pingdest.Object.GetPingdest().GetDestination(), packetsize, pingdest.Object.GetPingdest().Interval)
}
