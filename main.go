package main

import (
	"github.com/shreekarashastry/blockchain/simulation"
)

const (
	numHonestMiners uint64 = 38
	numAdversary    uint64 = 12
)

func main() {
	simulation := simulation.NewSimulation(simulation.Bitcoin, numHonestMiners, numAdversary)
	simulation.Start()
}
