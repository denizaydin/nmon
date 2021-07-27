package main

import (
	"fmt"
	"os"
	"time"

	ping "dnzydn.com/nmon/client"
	log "github.com/sirupsen/logrus"
)

var usage = `
Usage:
    ping [-c count] [-i interval] [-t timeout] [--privileged] host
Examples:
    # ping google continuously
    ping www.google.com
    # ping google 5 times
    ping -c 5 www.google.com
    # ping google 5 times at 500ms intervals
    ping -c 5 -i 500ms www.google.com
    # ping google for 10 seconds
    ping -t 10s www.google.com
    # Send a privileged raw ICMP ping
    sudo ping --privileged www.google.com
    # Send ICMP messages with a 100-byte payload
    ping -s 100 1.1.1.1
`

func checkPingDestination(done chan string, pingdest string) {

	interval := int32(100000000)
	count := -1
	size := 1000
	privileged := true
	donotfragment := false

	//TODO: get logging from caller
	log.SetFormatter(&log.TextFormatter{
		DisableColors: true,
		FullTimestamp: true,
	})
	log.SetOutput(os.Stdout)
	log.SetLevel(log.DebugLevel)

	pinger, err := ping.NewPinger(pingdest)
	if err != nil {
		fmt.Printf("ERROR: %s\n", err.Error())
		return
	}
	go func(interval *int32, donotfragment *bool, size *int) {
		time.Sleep(time.Second)
		*interval = 1000000000
		*donotfragment = true
		*size = 1000
	}(&interval, &donotfragment, &size)

	pinger.OnRecv = func(pkt *ping.Packet) {
		fmt.Printf("%d bytes from %s: icmp_seq=%d time=%v ttl=%v\n",
			pkt.Nbytes, pkt.IPAddr, pkt.Seq, pkt.Rtt, pkt.Ttl)
	}
	pinger.OnDuplicateRecv = func(pkt *ping.Packet) {
		fmt.Printf("%d bytes from %s: icmp_seq=%d time=%v ttl=%v (DUP!)\n",
			pkt.Nbytes, pkt.IPAddr, pkt.Seq, pkt.Rtt, pkt.Ttl)
	}
	pinger.OnFinish = func(stats *ping.Statistics) {
		fmt.Printf("\n--- %s ping statistics ---\n", stats.Addr)
		fmt.Printf("%d packets transmitted, %d packets received, %d duplicates, %v%% packet loss\n",
			stats.PacketsSent, stats.PacketsRecv, stats.PacketsRecvDuplicates, stats.PacketLoss)
		fmt.Printf("round-trip min/avg/max/stddev = %v/%v/%v/%v\n",
			stats.MinRtt, stats.AvgRtt, stats.MaxRtt, stats.StdDevRtt)
	}

	pinger.Count = count
	pinger.Size = &size
	pinger.Interval = &interval
	//pinger.DonotFragment = &donotfragment
	pinger.SetPrivileged(privileged)
	pinger.SetNetwork("udp")

	fmt.Printf("PING %s (%s):\n", pinger.Addr(), pinger.IPAddr())

	err = pinger.Run()

	if err != nil {
		fmt.Printf("Failed to ping target host: %s", err)
	}

	/*
		for {
			select {
			case msg := <-done:
				log.Infof("Received message:%v from done channel", msg)

			default:
				log.Infof("Pinging destination:%v and is it active:%v", pingdest.Destination, pingdest.Active)

				time.Sleep(1 * time.Second)
			}
		}
	*/
}

func main() {

	host := "www.trendyol.com"
	c := make(chan string)

	go checkPingDestination(c, host)

	time.Sleep(time.Second * 1000)

}
