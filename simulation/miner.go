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

const (
	newBlockChSize = 3
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
	lock          sync.RWMutex
	honestDelta   int64
}

func NewMiner(index int, sim *Simulation, kind MinerKind, honestDelta int64, consensus Consensus) *Miner {
	return &Miner{
		index:       index,
		bc:          NewBlockchain(),
		engine:      New(),
		minedCh:     make(chan *Block),
		stopCh:      make(chan struct{}),
		newBlockCh:  make(chan *Block, newBlockChSize),
		minerType:   kind,
		consensus:   consensus,
		currentHead: GenesisBlock(),
		sim:         sim,
		honestDelta: honestDelta,
	}
}

func (m *Miner) Start(startWg *sync.WaitGroup, broadcastFeed *event.Feed) {
	defer m.sim.wg.Done()

	m.bc = NewBlockchain()
	m.currentHead = GenesisBlock()
	m.broadcastFeed = broadcastFeed
	m.newBlockCh = make(chan *Block, newBlockChSize)
	m.newBlockSub = m.SubscribeMinedBlocksEvent()
	m.engine.attempts = 0

	var wg sync.WaitGroup

	wg.Add(2)
	go m.ListenNewBlocks(&wg)
	go m.MinedEvent(&wg)

	// notify the start of the miner
	startWg.Done()

	wg.Wait()
}

func (m *Miner) interruptMining() {
	m.lock.Lock()
	defer m.lock.Unlock()
	if m.stopCh != nil {
		close(m.stopCh)
		m.stopCh = nil
	}
}

func (m *Miner) Stop() {
	m.interruptMining()

	if m.minerType == HonestMiner {
		m.sim.honestAttempts += m.engine.attempts
	} else if m.minerType == AdversaryMiner {
		m.sim.advAttempts += m.engine.attempts
	}

	if m.minerType == HonestMiner {
		// Since we dont know when each of the miner stops the execution
		// we have to pick the lowest sim duration
		minerSimDuration := time.Since(m.sim.simStartTime)
		if m.sim.simDuration == 0 {
			m.sim.simDuration = minerSimDuration
		} else if minerSimDuration < m.sim.simDuration {
			m.sim.simDuration = minerSimDuration
		}
	}

	// wait for miners to abort
	m.miningWg.Wait()
	m.newBlockSub.Unsubscribe()

	// close the channel to kill all the worker newBlock listeners
	close(m.newBlockCh)
}

func (m *Miner) newBlockListenerWorker(wg *sync.WaitGroup) {
	defer wg.Done()
	for newBlock := range m.newBlockCh {
		select {
		case <-m.newBlockCh:
			return
		default:
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

				m.Mine()
			}
		}
	}
}

func (m *Miner) ListenNewBlocks(wg *sync.WaitGroup) {
	defer wg.Done()
	for i := 0; i < newBlockChSize; i++ {
		wg.Add(1)
		go m.newBlockListenerWorker(wg)
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
			fmt.Println("Mined a new block", m.index, m.minerType, minedBlock.Hash(), minedBlock.Number(), minedBlock.ParentWeight())
			minedBlock.SetTime(uint64(time.Now().UnixMilli()))
			// Add block to the block database
			m.bc.blocks.Add(minedBlock.Hash(), *CopyBlock(minedBlock))

			// Once a new block is mined, the current miner starts mining the
			// new block
			m.interruptMining()
			m.SetCurrentHead(minedBlock)

			// Start mining the next block
			m.Mine()

			go func() {
				// In the case of the honest miner add a broadcast delay
				if m.minerType == HonestMiner {
					time.Sleep(time.Duration(m.honestDelta) * time.Millisecond)
				}
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

	currentHeadWeight := m.engine.CalculateBlockWeight(m.currentHead, m.consensus)
	newBlockWeight := m.engine.CalculateBlockWeight(block, m.consensus)

	if newBlockWeight > currentHeadWeight {
		m.currentHead = CopyBlock(block)
	}
}

func (m *Miner) Mine() {
	newPendingHeader := m.currentHead.PendingBlock()
	newPendingHeader.SetParentWeight(m.engine.CalculateBlockWeight(m.currentHead, m.consensus))

	m.stopCh = make(chan struct{})
	err := m.engine.Seal(newPendingHeader, &m.miningWg, m.minedCh, m.stopCh)
	if err != nil {
		fmt.Println("Error sealing the block", err)
	}
}

func (m *Miner) SubscribeMinedBlocksEvent() event.Subscription {
	return m.broadcastFeed.Subscribe(m.newBlockCh)
}
