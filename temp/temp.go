package main

import (
	"fmt"
	"time"
)

func say(t *int32) {
	i := 0
	for {
		time.Sleep(time.Duration(*t) * time.Second)
		i++
		fmt.Println("i= ", i)
		if i > 5 {
			*t = int32(2)
		}

	}
}
func main() {

	interval := int32(1)

	tickerValue := &interval

	d := time.NewTicker(time.Duration(*tickerValue) * time.Second)
	go say(tickerValue)

	// Using for loop
	for {

		// Select statement
		select {

		// Case statement

		// Case to print current time
		case tm := <-d.C:
			d = time.NewTicker(time.Duration(*tickerValue) * time.Second)
			fmt.Println("The Current time is: ", tm)
		}
	}
}
