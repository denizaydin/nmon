package main

import (
	"fmt"

	ripe "github.com/mehrdadrad/mylg/ripe"
)

func main() {
	myip, err := ripe.MyIPAddr()
	fmt.Println(err)
	fmt.Println(myip)
	fmt.Println("88.225.253.200/24")

	var p ripe.Prefix
	p.Set(myip)
	p.GetData()
	p.GetGeoData()
	fmt.Println(p)
	p.PrettyPrint()
	fmt.Println(p.GeoData.Data.Locations)
	data, _ := p.Data["data"].(map[string]interface{})

	asns := data["asns"].([]interface{})
	for _, h := range asns {
		fmt.Println(h.(map[string]interface{})["holder"].(string))
		fmt.Println(h.(map[string]interface{})["asn"].(float64))
	}

}
