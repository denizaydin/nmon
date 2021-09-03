//Package client - includes all required monitoring objects for the client
package client

import (
	"time"

	proto "github.com/denizaydin/nmon/api"
	"github.com/denizaydin/ping"
)

//CheckPingDestination - send continous pings
func CheckPingDestination(pingdest *MonObject, client *NmonClient) {
	client.Logging.Debugf("pinger:%v start with values:%v", pingdest.Object.GetPingdest().GetName(), pingdest.Object.GetPingdest())
	pinger, err := ping.NewPinger(pingdest.Object.GetPingdest().Destination)
	//TODO: as we are returning from ping method, this object should be removed from monobjectlist which is  not implemented yet.
	if err != nil {
		client.Logging.Errorf("pinger:%v, can not created pinger, err:%v,", pingdest.Object.GetPingdest().GetName(), err.Error())
		return
	}
	pinger.SetLogger(client.Logging)
	pinger.OnSend = func(pkt *ping.Packet) {
		pingdest.ThreadupdateTime = time.Now().UnixNano()
		client.Logging.Tracef("pinger:%v setting threadupdatetime:%v", pingdest.Object.GetPingdest().GetName(), pingdest.ThreadupdateTime)
	}
	pinger.OnRecv = func(pkt *ping.Packet) {
		pingdest.ThreadupdateTime = time.Now().UnixNano()
		client.Logging.Debugf("pinger:%v -> %d bytes from %s: icmp_seq=%d time=%v ttl=%v\n", pingdest.Object.GetPingdest().GetName(),
			pkt.Nbytes, pkt.IPAddr, pkt.Seq, pkt.Rtt, pkt.Ttl)
		stat := &proto.StatsObject{
			Client:    client.StatsClient,
			Timestamp: time.Now().UnixNano(),
			Object: &proto.StatsObject_Pingstat{
				Pingstat: &proto.PingStat{
					Destination: pingdest.Object.GetPingdest().GetDestination(),
					Rtt:         int32(pkt.Rtt.Milliseconds()),
				},
			},
		}
		client.Statschannel <- stat
		client.Logging.Tracef("pinger:%v sent stats:%v", pingdest.Object.GetPingdest().GetName(), stat)
	}
	//Setting count to -1, contionous ping.
	//pinger.Count = -1 time.Duration(pingdest.Timeout) also we need to set timeout
	//Do we need to check packet size for more than jumbo?
	packetsize := int(pingdest.Object.GetPingdest().PacketSize)
	if packetsize < 64 {
		client.Logging.Warnf("pinger:%v packetsize:%v is lower than 64byte, setting it to 64byte", pingdest.Object.GetPingdest().GetName(), packetsize)
		packetsize = 64
	}
	//pinger.Size = &packetsize
	pinger.Interval = &pingdest.Object.GetPingdest().Interval
	pinger.SetPrivileged(true)
	client.Logging.Debugf("pinger:%v host:%s with size %s and interval %s", pingdest.Object.GetPingdest().GetName(), packetsize, pingdest.Object.GetPingdest().Interval)
	exit := false
	go func() {
		for !exit {
			select {
			case <-pingdest.Notify:
				client.Logging.Debugf("pinger:%v is stopped", pingdest.Object.GetPingdest().GetName())
				pinger.Stop()
				exit = true
			}
		}
	}()
	err = pinger.Run()
	if err != nil {
		client.Logging.Errorf("pinger:%v failed start, err:%s", pingdest.Object.GetPingdest().GetName(), err)
	}
	client.Logging.Debugf("pinger:%v exiting", pingdest.Object.GetPingdest().GetName())
}
