package simulation

import (
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/dominant-strategies/go-quai/event"
)

const (
	c_maxBlocks                      = 100
	c_maxIterations                  = 100
	c_honestDelta                    = 20 // milliseconds
	c_commonPrefixFailure            = 0.1
	c_winningThreshold               = c_maxIterations * (1 - c_commonPrefixFailure)
	c_honestListeningThreads         = 10
	c_poemGamma              float64 = 100
)

type Simulation struct {
	honestMiners []*Miner
	advMiners    []*Miner

	wg sync.WaitGroup

	honestBlockFeed *event.Feed
	advBlockFeed    *event.Feed

	simStartTime           time.Time
	simDuration            time.Duration
	totalHonestBlocks      uint64
	totalHonestSimDuration int64

	consensus Consensus // Bitcoin or Poem
	engine    *Blake3pow

	honestBc map[int]*Block
	advBc    map[int]*Block

	honestAttempts int64
	advAttempts    int64
}

func NewSimulation(consensus Consensus, numHonestMiners, numAdversary uint64) *Simulation {
	// Create miiners and adversary
	honestMiners := make([]*Miner, 0)
	advMiners := make([]*Miner, 0)
	// Initialize the adversary miner
	sim := &Simulation{
		simStartTime:           time.Time{},
		simDuration:            0,
		totalHonestSimDuration: 0,
		totalHonestBlocks:      0,
		consensus:              consensus,
		engine:                 New(),
		honestAttempts:         0,
		advAttempts:            0,
	}
	for i := 0; i < int(numHonestMiners); i++ {
		honestMiners = append(honestMiners, NewMiner(i, sim, HonestMiner, consensus))
	}
	sim.honestMiners = honestMiners

	for i := 0; i < int(numAdversary); i++ {
		advMiners = append(advMiners, NewMiner(i, sim, AdversaryMiner, consensus))
	}
	sim.advMiners = advMiners
	return sim
}

func (sim *Simulation) Start() {
	winCounter := make([]int, c_maxBlocks)
	for i := 0; i < c_maxIterations; i++ {
		sim.honestAttempts = 0
		sim.advAttempts = 0
		sim.simDuration = 0
		fmt.Println("Iteration", i)

		var honestBlockFeed event.Feed
		var advBlockFeed event.Feed
		sim.honestBlockFeed = &honestBlockFeed
		sim.advBlockFeed = &advBlockFeed

		var startWg sync.WaitGroup
		// Start the honest miners
		for _, honestMiner := range sim.honestMiners {
			sim.wg.Add(1)
			startWg.Add(1)
			go func(honestMiner *Miner) {
				honestMiner.Start(&startWg, &honestBlockFeed)
			}(honestMiner)
		}
		for _, adversaryMiner := range sim.advMiners {
			sim.wg.Add(1)
			startWg.Add(1)
			go func(adversaryMiner *Miner) {
				adversaryMiner.Start(&startWg, &advBlockFeed)
			}(adversaryMiner)
		}

		// wait until the miners are spawned
		startWg.Wait()

		sim.simStartTime = time.Now()
		// Send the genesis block to mine
		sim.honestBlockFeed.Send(GenesisBlock())
		sim.advBlockFeed.Send(GenesisBlock())

		// wait for all the miners to exit
		sim.wg.Wait()

		sim.honestBc = sim.honestMiners[0].ConstructBlockchain()
		sim.advBc = sim.advMiners[0].ConstructBlockchain()

		if sim.consensus == Bitcoin {
			// after this simulation is done, calculate a win chart
			for i := 1; i <= c_maxBlocks; i++ {
				honestBlock := sim.honestBc[i]
				adversaryBlock := sim.advBc[i]
				if honestBlock.Time() < adversaryBlock.Time() {
					winCounter[i-1]++
				}
			}
		} else if sim.consensus == Poem {
			// do a greedy process of finding the block that reached each
			// poem threshold = i * (gamma + 1/ln(2))
			for i := 1; i <= c_maxBlocks; i++ {
				var gamma float64
				if c_poemGamma != 100 { // handling the non natural gamma case
					gamma = float64(i) * (c_poemGamma + float64(1)/math.Log(2))
				} else {
					gamma = float64(i) * (math.Log2(float64(GenesisBlock().Difficulty())) + float64(1)/math.Log(2))
				}

				var honestBlock, advBlock *Block
				for i := 1; i <= len(sim.honestBc); i++ {
					if sim.engine.CalculateBlockWeight(sim.honestBc[i], sim.consensus) >= gamma {
						honestBlock = sim.honestBc[i]
						break
					}
				}
				for j := 1; j <= len(sim.advBc); j++ {
					if sim.engine.CalculateBlockWeight(sim.advBc[j], sim.consensus) >= gamma {
						advBlock = sim.advBc[j]
						break
					}
				}
				if advBlock == nil {
					winCounter[i-1]++
				}
				if honestBlock == nil || advBlock == nil {
					continue
				}
				// update the win counter
				if honestBlock.Time() < advBlock.Time() {
					winCounter[i-1]++
				}
			}
		} else {
			panic("simulation consensus not supported")
		}

		sim.totalHonestSimDuration += sim.simDuration.Milliseconds()
		fmt.Println("Honest Iteration time", sim.simDuration.Seconds(), "secs")

		time.Sleep(3 * time.Second)
	}
	avgHonestBlocks := sim.totalHonestBlocks / c_maxIterations
	avgHonestRoundTime := sim.totalHonestSimDuration / (c_maxIterations * c_honestDelta)
	fmt.Println("Simulation Summary", sim.consensus)
	fmt.Println("c_poemGamma", c_poemGamma)
	fmt.Println("Honest Time Delta", c_honestDelta, "milliseconds")
	fmt.Println("Average num of honest blocks", avgHonestBlocks)
	fmt.Println("Average honest sim duration in Delta", avgHonestRoundTime)

	g := float64(avgHonestBlocks) / float64(avgHonestRoundTime)
	f := float64(c_maxBlocks) / float64(avgHonestRoundTime)
	var k uint64
	for i := 0; i < len(winCounter); i++ {
		if winCounter[i] > c_winningThreshold {
			k = uint64(i) + 1
			break
		}
	}
	d := float64(k) / f

	fmt.Println("win counter", winCounter)
	fmt.Println("g", g, "f", f, "k", k, "d", d)
	fmt.Println("Simulation done")
}
