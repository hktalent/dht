package dht

import (
	"fmt"
	"testing"
)

func Test1() {
	fmt.Println("test")
}
func TestMyTick(t *testing.T) {

	x := NewMyTick(3, Test1)
	x.Start()
}
