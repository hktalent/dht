package main

import (
	"fmt"

	"github.com/hktalent/dht"
)

func main() {
	ip := dht.StunList{}.GetSelfPublicIpPort()
	fmt.Println(ip)
}
