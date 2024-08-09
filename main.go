package main

import (
	"github.com/shreekarashastry/blockchain/simulation"
)

const (
	numHonestMiners uint64 = 32
	numAdversary    uint64 = 18
)

func main() {
	simulation := simulation.NewSimulation(simulation.Bitcoin, numHonestMiners, numAdversary)
	simulation.Start()
}
