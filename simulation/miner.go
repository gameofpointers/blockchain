package simulation

import (
	"fmt"
	"sync"
	"time"

	"github.com/dominant-strategies/go-quai/event"
)

type MinerKind uint

const (
	HonestMiner MinerKind = iota
	AdversaryMiner
)

type Miner struct {
	index         int
	bc            *BlockDB
	currentHead   *Block
	engine        *Blake3pow
	minedCh       chan *Block
	newBlockCh    chan *Block
	newBlockSub   event.Subscription
	stopCh        chan struct{}
	broadcastFeed *event.Feed
	minerType     MinerKind
	consensus     Consensus
	sim           *Simulation
	miningWg      sync.WaitGroup
}

func NewMiner(index int, sim *Simulation, kind MinerKind, consensus Consensus) *Miner {
	return &Miner{
		index:       index,
		bc:          NewBlockchain(),
		engine:      New(),
		minedCh:     make(chan *Block),
		stopCh:      make(chan struct{}),
		newBlockCh:  make(chan *Block),
		minerType:   kind,
		consensus:   consensus,
		currentHead: GenesisBlock(),
		sim:         sim,
	}
}

func (m *Miner) Start(startWg *sync.WaitGroup, broadcastFeed *event.Feed) {
	defer m.sim.wg.Done()

	m.bc = NewBlockchain()
	m.currentHead = GenesisBlock()
	m.broadcastFeed = broadcastFeed
	m.newBlockSub = m.SubscribeMinedBlocksEvent()

	var wg sync.WaitGroup
	wg.Add(2)
	go m.ListenNewBlocks(&wg)
	go m.MinedEvent(&wg)

	// notify the start of the miner
	startWg.Done()

	wg.Wait()
}

func (m *Miner) interruptMining() {
	if m.stopCh != nil {
		close(m.stopCh)
		m.stopCh = nil
	}
}

func (m *Miner) Stop() {
	m.interruptMining()
	m.sim.simDuration = time.Since(m.sim.simStartTime)
	// wait for miners to abort
	m.miningWg.Wait()
	m.newBlockSub.Unsubscribe()
}

func (m *Miner) ListenNewBlocks(wg *sync.WaitGroup) {
	defer wg.Done()
	for {
		select {
		case newBlock := <-m.newBlockCh:
			if newBlock.Number() > c_maxBlocks {
				m.Stop()
				return
			}
			// If this block already is in the database, we mined it
			_, exists := m.bc.blocks.Get(newBlock.Hash())
			if !exists {
				m.bc.blocks.Add(newBlock.Hash(), *newBlock)
				// Once a new block is mined, the current miner starts mining the
				// new block
				m.interruptMining()
				m.SetCurrentHead(newBlock)

				m.miningWg.Add(1)
				m.Mine()
			}
		case <-m.newBlockSub.Err():
			return
		}
	}
}

func (m *Miner) MinedEvent(wg *sync.WaitGroup) {
	defer wg.Done()
	for {
		select {
		case minedBlock := <-m.minedCh:
			if minedBlock.Number() > c_maxBlocks {
				m.Stop()
				return
			}

			m.sim.totalHonestBlocks++
			fmt.Println("Mined a new block", m.index, m.minerType, minedBlock.Hash(), minedBlock.Number())
			minedBlock.SetTime(uint64(time.Now().UnixMilli()))
			// Add block to the block database
			m.bc.blocks.Add(minedBlock.Hash(), *CopyBlock(minedBlock))

			// Once a new block is mined, the current miner starts mining the
			// new block
			m.interruptMining()
			m.SetCurrentHead(minedBlock)

			// Start mining the next block
			m.miningWg.Add(1)
			m.Mine()

			// In the case of the honest miner add a broadcast delay
			if m.minerType == HonestMiner {
				time.Sleep(c_honestDelta * time.Millisecond)
			}

			go func() {
				m.broadcastFeed.Send(minedBlock)
			}()

		case <-m.newBlockSub.Err():
			return
		}
	}
}

func (m *Miner) ConstructBlockchain() map[int]*Block {
	bc := make(map[int]*Block)
	currentHead := m.currentHead
	bc[int(currentHead.Number())] = currentHead
	for {
		if currentHead.ParentHash() == GenesisBlock().Hash() {
			break
		}
		parent, exists := m.bc.blocks.Get(currentHead.ParentHash())
		if !exists {
			return nil
		}
		currentHead = CopyBlock(&parent)
		bc[int(parent.Number())] = CopyBlock(&parent)
	}
	return bc
}

func (m *Miner) CalculateBlockWeight(block *Block) float64 {
	if m.consensus == Bitcoin {
		return block.ParentWeight() + float64(block.Difficulty())
	} else if m.consensus == Poem {
		return block.ParentWeight() + m.engine.IntrinsicDifficulty(block)
	} else {
		panic("invalid consensus type")
	}
}

func (m *Miner) SetCurrentHead(block *Block) {
	// If we are trying to the current head again return
	if m.currentHead.Hash() == block.Hash() {
		return
	}

	// We set the first block
	if block.ParentHash() == GenesisBlock().Hash() {
		m.currentHead = CopyBlock(block)
		return
	}

	currentHeadWeight := m.CalculateBlockWeight(m.currentHead)
	newBlockWeight := m.CalculateBlockWeight(block)

	if newBlockWeight > currentHeadWeight {
		m.currentHead = CopyBlock(block)
	}
}

func (m *Miner) Mine() {
	newPendingHeader := m.currentHead.PendingBlock()
	newPendingHeader.SetParentWeight(m.CalculateBlockWeight(m.currentHead))

	m.stopCh = make(chan struct{})
	err := m.engine.Seal(newPendingHeader, &m.miningWg, m.minedCh, m.stopCh)
	if err != nil {
		fmt.Println("Error sealing the block", err)
	}
}

func (m *Miner) SubscribeMinedBlocksEvent() event.Subscription {
	return m.broadcastFeed.Subscribe(m.newBlockCh)
}
