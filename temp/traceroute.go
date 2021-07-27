package main

import (
	"fmt"
	"net"

	"github.com/aeden/traceroute"
)

func printHop(hop traceroute.TracerouteHop) {
	addr := fmt.Sprintf("\nAAA %v.%v.%v.%v AAA\n", hop.Address[0], hop.Address[1], hop.Address[2], hop.Address[3])
	hostOrAddr := addr
	if hop.Host != "" {
		hostOrAddr = hop.Host
	}
	if hop.Success {
		fmt.Printf("\nDDD %-3d %v (%v)  %v DDD\n", hop.TTL, hostOrAddr, addr, hop.ElapsedTime)
	} else {
		fmt.Printf("\n%-3d *\n", hop.TTL)
	}
}

func address(address [4]byte) string {
	return fmt.Sprintf("\nZZZ %v.%v.%v.%v ZZZ\n", address[0], address[1], address[2], address[3])
}

func main() {

	//	host := flag.Arg(0)
	host := "8.8.8.8"
	fmt.Printf("\nhost %v\n", host)
	options := traceroute.TracerouteOptions{}
	options.SetRetries(3)
	options.SetMaxHops(20)
	options.SetFirstHop(1)

	ipAddr, err := net.ResolveIPAddr("ip", host)
	if err != nil {
		fmt.Printf("\nresolve Error: \n", err)

		return
	}

	fmt.Printf("\ntraceroute to %v (%v), %v hops max, %v byte packets\n", host, ipAddr, options.MaxHops(), options.PacketSize())
	c := make(chan traceroute.TracerouteHop, 0)
	fmt.Println("\nbefore func \n")

	go func() {
		for {
			hop, ok := <-c
			if !ok {
				fmt.Println("1 not ok, this is the end")
				return
			}
			fmt.Println("ok, this is the end")
			printHop(hop)
		}
	}()
	fmt.Printf("\nNeden err Error: \n", err)

	_, err = traceroute.Traceroute(host, &options, c)
	if err != nil {
		fmt.Printf("\nNeden err Error: \n", err)
	}
}
