package main

import (
	"fmt"

	"github.com/shreekarashastry/blockchain/simulation"
)

const (
	numHonestMiners uint64 = 19
	numAdversary    uint64 = 6
)

var (
	c_honestDeltas []int64 = []int64{50, 100, 150, 200, 250, 300, 350, 400, 450, 500} // milliseconds
)

func main() {
	// Run the simulation for all the honest deltas
	for i := 0; i < len(c_honestDeltas); i++ {
		fmt.Println("Honest delta", c_honestDeltas[i], "ms")
		simulation := simulation.NewSimulation(simulation.Bitcoin, numHonestMiners, numAdversary, c_honestDeltas[i])
		simulation.Start()
	}
}
