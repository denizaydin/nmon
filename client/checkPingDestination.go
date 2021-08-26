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
	c.Logging.Debugf("pinger:%v start with values:%v", pingdest.Object.GetPingdest().GetName(), pingdest.Object.GetPingdest())
	pinger, err := ping.NewPinger(pingdest.Object.GetPingdest().Destination)
	//TODO: as we are returning from ping method, this object should be removed from monobjectlist which is  not implemented yet.
	if err != nil {
		c.Logging.Errorf("pinger:%v, can not created pinger, err:%v,", pingdest.Object.GetPingdest().GetName(), err.Error())
		return
	}
	pinger.SetLogger(c.Logging)
	pinger.OnSend = func(pkt *ping.Packet) {
		pingdest.ThreadupdateTime = time.Now().UnixNano()
		c.Logging.Tracef("pinger:%v threadupdatetime:%v", pingdest.Object.GetPingdest().GetName(), pingdest.ThreadupdateTime)
	}
	pinger.OnRecv = func(pkt *ping.Packet) {
		pingdest.ThreadupdateTime = time.Now().UnixNano()
		c.Logging.Tracef("pinger:%v threadupdatetime:%v", pingdest.Object.GetPingdest().GetName(), pingdest.ThreadupdateTime)
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
			c.Logging.Tracef("pinger:%v stats server is not ready, skipping", pingdest.Object.GetPingdest().GetName())
		} else {
			c.Logging.Tracef("pinger:%v received stats:%v for ping destination:%v", pingdest.Object.GetPingdest().GetName(), stat, pingdest.Object.GetPingdest().GetDestination())
			stream, err := c.StatsConnClient.RecordStats(context.Background())
			if err != nil {
				c.Logging.Errorf("pinger:%v grpc stream failed while sending stats:%v", pingdest.Object.GetPingdest().GetName(), err)
			} else {
				if err := stream.Send(stat); err != nil {
					c.Logging.Errorf("pinger:%v can not send client stats:%v, err:%v", pingdest.Object.GetPingdest().GetName(), stream, err)
				}
				c.Logging.Tracef("pinger:%v sent stats:%v", pingdest.Object.GetPingdest().GetName(), stat)
			}
		}
		c.Logging.Debugf("pinger:%v -> %d bytes from %s: icmp_seq=%d time=%v ttl=%v\n", pingdest.Object.GetPingdest().GetName(),
			pkt.Nbytes, pkt.IPAddr, pkt.Seq, pkt.Rtt, pkt.Ttl)
	}
	//Setting count to -1, contionous ping.
	//pinger.Count = -1 time.Duration(pingdest.Timeout) also we need to set timeout
	//Do we need to check packet size for more than jumbo?
	packetsize := int(pingdest.Object.GetPingdest().PacketSize)
	if packetsize < 64 {
		c.Logging.Warnf("pinger:%v packetsize:%v is lower than 64byte, setting it to 64byte", pingdest.Object.GetPingdest().GetName(), packetsize)
		packetsize = 64
	}
	//pinger.Size = &packetsize
	pinger.Interval = &pingdest.Object.GetPingdest().Interval
	pinger.SetPrivileged(true)
	c.Logging.Debugf("pinger:%v host:%s with size %s and interval %s", pingdest.Object.GetPingdest().GetName(), packetsize, pingdest.Object.GetPingdest().Interval)
	exit := false
	go func() {
		for !exit {
			select {
			case <-pingdest.Notify:
				c.Logging.Debugf("pinger:%v is stopped", pingdest.Object.GetPingdest().GetName())
				pinger.Stop()
				exit = true
			}
		}
	}()
	err = pinger.Run()
	if err != nil {
		c.Logging.Errorf("pinger:%v failed start, err:%s", pingdest.Object.GetPingdest().GetName(), err)
	}
	c.Logging.Debugf("pinger:%v exiting", pingdest.Object.GetPingdest().GetName())
}
