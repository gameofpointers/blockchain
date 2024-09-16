package main

import (
	"github.com/shreekarashastry/blockchain/simulation"
)

const (
	numHonestMiners uint64 = 19
	numAdversary    uint64 = 11
)

func main() {
	simulation := simulation.NewSimulation(simulation.Bitcoin, numHonestMiners, numAdversary)
	simulation.Start()
}
