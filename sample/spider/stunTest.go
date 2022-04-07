package main

import (
	"fmt"

	"github.com/hktalent/dht"
)

func main() {
	ip, port := dht.StunList{}.GetSelfPublicIpPort()
	fmt.Println(ip, port)
}
