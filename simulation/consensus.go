package simulation

import (
	crand "crypto/rand"
	"math"
	"math/big"
	"math/rand"
	"sync"
	"time"
)

var (
	big2e256 = new(big.Int).Exp(big.NewInt(2), big.NewInt(256), big.NewInt(0)) // 2^256
)

type Blake3pow struct {
	lock sync.Mutex
}

func New() *Blake3pow {
	blake3pow := &Blake3pow{}
	return blake3pow
}

// Seal implements consensus.Engine, attempting to find a nonce that satisfies
// the header's difficulty requirements.
func (blake3pow *Blake3pow) Seal(header *Block, wg *sync.WaitGroup, results chan<- *Block, stop <-chan struct{}) error {
	defer wg.Done()
	// Create a runner and the multiple search threads it directs
	abort := make(chan struct{})

	blake3pow.lock.Lock()
	seed, err := crand.Int(crand.Reader, big.NewInt(math.MaxInt64))
	if err != nil {
		blake3pow.lock.Unlock()
		return err
	}
	randMining := rand.New(rand.NewSource(seed.Int64()))
	blake3pow.lock.Unlock()
	var (
		pend   sync.WaitGroup
		locals = make(chan *Block)
	)
	pend.Add(1)
	go func(id int, nonce uint64) {
		defer pend.Done()
		blake3pow.mine(header, id, nonce, abort, locals)
	}(0, uint64(randMining.Int63()))
	// Wait until sealing is terminated or a nonce is found
	go func() {
		var result *Block
		select {
		case <-stop:
			// Outside abort, stop all miner threads
			close(abort)
		case result = <-locals:
			// One of the threads found a block, abort all others
			select {
			case results <- result:
			default:
			}
			close(abort)
		}
		// Wait for all miners to terminate and return the block
		pend.Wait()
	}()
	return nil
}

// mine is the actual proof-of-work miner that searches for a nonce starting from
// seed that results in correct final header difficulty.
func (blake3pow *Blake3pow) mine(header *Block, id int, seed uint64, abort chan struct{}, found chan *Block) {
	// Extract some data from the header
	var (
		target = new(big.Int).Div(big2e256, big.NewInt(int64(header.Difficulty())))
	)

	// Start generating random nonces until we abort or find a good one
	var (
		attempts  = int64(0)
		nonce     = seed
		powBuffer = new(big.Int)
	)
search:
	for {
		select {
		case <-abort:
			// Mining terminated, update stats and abort
			break search

		default:
			// We don't have to update hash rate on every nonce, so update after after 2^X nonces
			attempts++
			if (attempts % (1 << 15)) == 0 {
				attempts = 0
			}
			time.Sleep(1 * time.Millisecond)
			copyHeader := CopyBlock(header)
			// Compute the PoW value of this nonce
			copyHeader.SetNonce(EncodeNonce(nonce))
			hash := copyHeader.Hash().Bytes()
			if powBuffer.SetBytes(hash).Cmp(target) <= 0 {
				// Correct nonce found, create a new header with it
				// Seal and return a block (if still needed)
				select {
				case found <- copyHeader:
				case <-abort:
				}
				break search
			}
			nonce++
		}
	}
}

func (Blake3pow *Blake3pow) IntrinsicDifficulty(block *Block) float64 {
	// For now returning the difficulty, in the PoEM we need
	// to calculate this value
	return float64(block.Difficulty())
}
