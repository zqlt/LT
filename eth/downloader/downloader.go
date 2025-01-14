
















package downloader

import (
	"errors"
	"fmt"
	"math/big"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/event"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/metrics"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/trie"
)

var (
	MaxHashFetch    = 512 
	MaxBlockFetch   = 128 
	MaxHeaderFetch  = 192 
	MaxSkeletonSize = 128 
	MaxReceiptFetch = 256 
	MaxStateFetch   = 384 

	rttMinEstimate   = 2 * time.Second  
	rttMaxEstimate   = 20 * time.Second 
	rttMinConfidence = 0.1              
	ttlScaling       = 3                
	ttlLimit         = time.Minute      

	qosTuningPeers   = 5    
	qosConfidenceCap = 10   
	qosTuningImpact  = 0.25 

	maxQueuedHeaders            = 32 * 1024                         
	maxHeadersProcess           = 2048                              
	maxResultsProcess           = 2048                              
	fullMaxForkAncestry  uint64 = params.FullImmutabilityThreshold  
	lightMaxForkAncestry uint64 = params.LightImmutabilityThreshold 

	reorgProtThreshold   = 48 
	reorgProtHeaderDelay = 2  

	fsHeaderCheckFrequency = 100             
	fsHeaderSafetyNet      = 2048            
	fsHeaderForceVerify    = 24              
	fsHeaderContCheck      = 3 * time.Second 
	fsMinFullBlocks        = 64              
)

var (
	errBusy                    = errors.New("busy")
	errUnknownPeer             = errors.New("peer is unknown or unhealthy")
	errBadPeer                 = errors.New("action from bad peer ignored")
	errStallingPeer            = errors.New("peer is stalling")
	errUnsyncedPeer            = errors.New("unsynced peer")
	errNoPeers                 = errors.New("no peers to keep download active")
	errTimeout                 = errors.New("timeout")
	errEmptyHeaderSet          = errors.New("empty header set by peer")
	errPeersUnavailable        = errors.New("no peers available or all tried for download")
	errInvalidAncestor         = errors.New("retrieved ancestor is invalid")
	errInvalidChain            = errors.New("retrieved hash chain is invalid")
	errInvalidBody             = errors.New("retrieved block body is invalid")
	errInvalidReceipt          = errors.New("retrieved receipt is invalid")
	errCancelStateFetch        = errors.New("state data download canceled (requested)")
	errCancelContentProcessing = errors.New("content processing canceled (requested)")
	errCanceled                = errors.New("syncing canceled (requested)")
	errNoSyncActive            = errors.New("no sync active")
	errTooOld                  = errors.New("peer doesn't speak recent enough protocol version (need version >= 63)")
)

type Downloader struct {
	
	
	
	
	rttEstimate   uint64 
	rttConfidence uint64 

	mode uint32         
	mux  *event.TypeMux 

	checkpoint uint64   
	genesis    uint64   
	queue      *queue   
	peers      *peerSet 

	stateDB    ethdb.Database  
	stateBloom *trie.SyncBloom 

	
	syncStatsChainOrigin uint64 
	syncStatsChainHeight uint64 
	syncStatsState       stateSyncStats
	syncStatsLock        sync.RWMutex 

	lightchain LightChain
	blockchain BlockChain

	
	dropPeer peerDropFn 

	
	synchroniseMock func(id string, hash common.Hash) error 
	synchronising   int32
	notified        int32
	committed       int32
	ancientLimit    uint64 

	
	headerCh      chan dataPack        
	bodyCh        chan dataPack        
	receiptCh     chan dataPack        
	bodyWakeCh    chan bool            
	receiptWakeCh chan bool            
	headerProcCh  chan []*types.Header 

	
	pivotHeader *types.Header 
	pivotLock   sync.RWMutex  

	stateSyncStart chan *stateSync
	trackStateReq  chan *stateReq
	stateCh        chan dataPack 

	
	cancelPeer string         
	cancelCh   chan struct{}  
	cancelLock sync.RWMutex   
	cancelWg   sync.WaitGroup 

	quitCh   chan struct{} 
	quitLock sync.Mutex    

	
	syncInitHook     func(uint64, uint64)  
	bodyFetchHook    func([]*types.Header) 
	receiptFetchHook func([]*types.Header) 
	chainInsertHook  func([]*fetchResult)  
}


type LightChain interface {
	
	HasHeader(common.Hash, uint64) bool

	
	GetHeaderByHash(common.Hash) *types.Header

	
	CurrentHeader() *types.Header

	
	GetTd(common.Hash, uint64) *big.Int

	
	InsertHeaderChain([]*types.Header, int) (int, error)

	
	SetHead(uint64) error
}


type BlockChain interface {
	LightChain

	
	HasBlock(common.Hash, uint64) bool

	
	HasFastBlock(common.Hash, uint64) bool

	
	GetBlockByHash(common.Hash) *types.Block

	
	CurrentBlock() *types.Block

	
	CurrentFastBlock() *types.Block

	
	FastSyncCommitHead(common.Hash) error

	
	InsertChain(types.Blocks) (int, error)

	
	InsertReceiptChain(types.Blocks, []types.Receipts, uint64) (int, error)
}


func New(checkpoint uint64, stateDb ethdb.Database, stateBloom *trie.SyncBloom, mux *event.TypeMux, chain BlockChain, lightchain LightChain, dropPeer peerDropFn) *Downloader {
	if lightchain == nil {
		lightchain = chain
	}
	dl := &Downloader{
		stateDB:        stateDb,
		stateBloom:     stateBloom,
		mux:            mux,
		checkpoint:     checkpoint,
		queue:          newQueue(blockCacheMaxItems, blockCacheInitialItems),
		peers:          newPeerSet(),
		rttEstimate:    uint64(rttMaxEstimate),
		rttConfidence:  uint64(1000000),
		blockchain:     chain,
		lightchain:     lightchain,
		dropPeer:       dropPeer,
		headerCh:       make(chan dataPack, 1),
		bodyCh:         make(chan dataPack, 1),
		receiptCh:      make(chan dataPack, 1),
		bodyWakeCh:     make(chan bool, 1),
		receiptWakeCh:  make(chan bool, 1),
		headerProcCh:   make(chan []*types.Header, 1),
		quitCh:         make(chan struct{}),
		stateCh:        make(chan dataPack),
		stateSyncStart: make(chan *stateSync),
		syncStatsState: stateSyncStats{
			processed: rawdb.ReadFastTrieProgress(stateDb),
		},
		trackStateReq: make(chan *stateReq),
	}
	go dl.qosTuner()
	go dl.stateFetcher()
	return dl
}








func (d *Downloader) Progress() ethereum.SyncProgress {
	
	d.syncStatsLock.RLock()
	defer d.syncStatsLock.RUnlock()

	current := uint64(0)
	mode := d.getMode()
	switch {
	case d.blockchain != nil && mode == FullSync:
		current = d.blockchain.CurrentBlock().NumberU64()
	case d.blockchain != nil && mode == FastSync:
		current = d.blockchain.CurrentFastBlock().NumberU64()
	case d.lightchain != nil:
		current = d.lightchain.CurrentHeader().Number.Uint64()
	default:
		log.Error("Unknown downloader chain/mode combo", "light", d.lightchain != nil, "full", d.blockchain != nil, "mode", mode)
	}
	return ethereum.SyncProgress{
		StartingBlock: d.syncStatsChainOrigin,
		CurrentBlock:  current,
		HighestBlock:  d.syncStatsChainHeight,
		PulledStates:  d.syncStatsState.processed,
		KnownStates:   d.syncStatsState.processed + d.syncStatsState.pending,
	}
}


func (d *Downloader) Synchronising() bool {
	return atomic.LoadInt32(&d.synchronising) > 0
}






func (d *Downloader) SyncBloomContains(hash []byte) bool {
	return d.stateBloom == nil || d.stateBloom.Contains(hash)
}



func (d *Downloader) RegisterPeer(id string, version int, peer Peer) error {
	logger := log.New("peer", id)
	logger.Trace("Registering sync peer")
	if err := d.peers.Register(newPeerConnection(id, version, peer, logger)); err != nil {
		logger.Error("Failed to register sync peer", "err", err)
		return err
	}
	d.qosReduceConfidence()

	return nil
}


func (d *Downloader) RegisterLightPeer(id string, version int, peer LightPeer) error {
	return d.RegisterPeer(id, version, &lightPeerWrapper{peer})
}




func (d *Downloader) UnregisterPeer(id string) error {
	
	logger := log.New("peer", id)
	logger.Trace("Unregistering sync peer")
	if err := d.peers.Unregister(id); err != nil {
		logger.Error("Failed to unregister sync peer", "err", err)
		return err
	}
	d.queue.Revoke(id)

	return nil
}



func (d *Downloader) Synchronise(id string, head common.Hash, td *big.Int, mode SyncMode) error {
	err := d.synchronise(id, head, td, mode)

	switch err {
	case nil, errBusy, errCanceled:
		return err
	}

	if errors.Is(err, errInvalidChain) || errors.Is(err, errBadPeer) || errors.Is(err, errTimeout) ||
		errors.Is(err, errStallingPeer) || errors.Is(err, errUnsyncedPeer) || errors.Is(err, errEmptyHeaderSet) ||
		errors.Is(err, errPeersUnavailable) || errors.Is(err, errTooOld) || errors.Is(err, errInvalidAncestor) {
		log.Warn("Synchronisation failed, dropping peer", "peer", id, "err", err)
		if d.dropPeer == nil {
			
			
			log.Warn("Downloader wants to drop peer, but peerdrop-function is not set", "peer", id)
		} else {
			d.dropPeer(id)
		}
		return err
	}
	log.Warn("Synchronisation failed, retrying", "err", err)
	return err
}




func (d *Downloader) synchronise(id string, hash common.Hash, td *big.Int, mode SyncMode) error {
	
	if d.synchroniseMock != nil {
		return d.synchroniseMock(id, hash)
	}
	
	if !atomic.CompareAndSwapInt32(&d.synchronising, 0, 1) {
		return errBusy
	}
	defer atomic.StoreInt32(&d.synchronising, 0)

	
	if atomic.CompareAndSwapInt32(&d.notified, 0, 1) {
		log.Info("Block synchronisation started")
	}
	
	
	
	if mode == FullSync && d.stateBloom != nil {
		d.stateBloom.Close()
	}
	
	d.queue.Reset(blockCacheMaxItems, blockCacheInitialItems)
	d.peers.Reset()

	for _, ch := range []chan bool{d.bodyWakeCh, d.receiptWakeCh} {
		select {
		case <-ch:
		default:
		}
	}
	for _, ch := range []chan dataPack{d.headerCh, d.bodyCh, d.receiptCh} {
		for empty := false; !empty; {
			select {
			case <-ch:
			default:
				empty = true
			}
		}
	}
	for empty := false; !empty; {
		select {
		case <-d.headerProcCh:
		default:
			empty = true
		}
	}
	
	d.cancelLock.Lock()
	d.cancelCh = make(chan struct{})
	d.cancelPeer = id
	d.cancelLock.Unlock()

	defer d.Cancel() 

	
	atomic.StoreUint32(&d.mode, uint32(mode))

	
	p := d.peers.Peer(id)
	if p == nil {
		return errUnknownPeer
	}
	return d.syncWithPeer(p, hash, td)
}

func (d *Downloader) getMode() SyncMode {
	return SyncMode(atomic.LoadUint32(&d.mode))
}



func (d *Downloader) syncWithPeer(p *peerConnection, hash common.Hash, td *big.Int) (err error) {
	d.mux.Post(StartEvent{})
	defer func() {
		
		if err != nil {
			d.mux.Post(FailedEvent{err})
		} else {
			latest := d.lightchain.CurrentHeader()
			d.mux.Post(DoneEvent{latest})
		}
	}()
	if p.version < 63 {
		return errTooOld
	}
	mode := d.getMode()

	log.Debug("Synchronising with the network", "peer", p.id, "eth", p.version, "head", hash, "td", td, "mode", mode)
	defer func(start time.Time) {
		log.Debug("Synchronisation terminated", "elapsed", common.PrettyDuration(time.Since(start)))
	}(time.Now())

	
	latest, pivot, err := d.fetchHead(p)
	if err != nil {
		return err
	}
	if mode == FastSync && pivot == nil {
		
		
		
		
		pivot = d.blockchain.CurrentBlock().Header()
	}
	height := latest.Number.Uint64()

	origin, err := d.findAncestor(p, latest)
	if err != nil {
		return err
	}
	d.syncStatsLock.Lock()
	if d.syncStatsChainHeight <= origin || d.syncStatsChainOrigin > origin {
		d.syncStatsChainOrigin = origin
	}
	d.syncStatsChainHeight = height
	d.syncStatsLock.Unlock()

	
	if mode == FastSync {
		if height <= uint64(fsMinFullBlocks) {
			origin = 0
		} else {
			pivotNumber := pivot.Number.Uint64()
			if pivotNumber <= origin {
				origin = pivotNumber - 1
			}
			
			
			rawdb.WriteLastPivotNumber(d.stateDB, pivotNumber)
		}
	}
	d.committed = 1
	if mode == FastSync && pivot.Number.Uint64() != 0 {
		d.committed = 0
	}
	if mode == FastSync {
		
		
		
		
		
		
		
		
		
		
		
		
		
		
		if d.checkpoint != 0 && d.checkpoint > fullMaxForkAncestry+1 {
			d.ancientLimit = d.checkpoint
		} else if height > fullMaxForkAncestry+1 {
			d.ancientLimit = height - fullMaxForkAncestry - 1
		} else {
			d.ancientLimit = 0
		}
		frozen, _ := d.stateDB.Ancients() 

		
		
		if origin >= frozen && frozen != 0 {
			d.ancientLimit = 0
			log.Info("Disabling direct-ancient mode", "origin", origin, "ancient", frozen-1)
		} else if d.ancientLimit > 0 {
			log.Debug("Enabling direct-ancient mode", "ancient", d.ancientLimit)
		}
		
		if origin+1 < frozen {
			if err := d.lightchain.SetHead(origin + 1); err != nil {
				return err
			}
		}
	}
	
	d.queue.Prepare(origin+1, mode)
	if d.syncInitHook != nil {
		d.syncInitHook(origin, height)
	}
	fetchers := []func() error{
		func() error { return d.fetchHeaders(p, origin+1) }, 
		func() error { return d.fetchBodies(origin + 1) },   
		func() error { return d.fetchReceipts(origin + 1) }, 
		func() error { return d.processHeaders(origin+1, td) },
	}
	if mode == FastSync {
		d.pivotLock.Lock()
		d.pivotHeader = pivot
		d.pivotLock.Unlock()

		fetchers = append(fetchers, func() error { return d.processFastSyncContent() })
	} else if mode == FullSync {
		fetchers = append(fetchers, d.processFullSyncContent)
	}
	return d.spawnSync(fetchers)
}



func (d *Downloader) spawnSync(fetchers []func() error) error {
	errc := make(chan error, len(fetchers))
	d.cancelWg.Add(len(fetchers))
	for _, fn := range fetchers {
		fn := fn
		go func() { defer d.cancelWg.Done(); errc <- fn() }()
	}
	
	var err error
	for i := 0; i < len(fetchers); i++ {
		if i == len(fetchers)-1 {
			
			
			
			d.queue.Close()
		}
		if err = <-errc; err != nil && err != errCanceled {
			break
		}
	}
	d.queue.Close()
	d.Cancel()
	return err
}




func (d *Downloader) cancel() {
	
	d.cancelLock.Lock()
	defer d.cancelLock.Unlock()

	if d.cancelCh != nil {
		select {
		case <-d.cancelCh:
			
		default:
			close(d.cancelCh)
		}
	}
}



func (d *Downloader) Cancel() {
	d.cancel()
	d.cancelWg.Wait()
}



func (d *Downloader) Terminate() {
	
	d.quitLock.Lock()
	select {
	case <-d.quitCh:
	default:
		close(d.quitCh)
	}
	if d.stateBloom != nil {
		d.stateBloom.Close()
	}
	d.quitLock.Unlock()

	
	d.Cancel()
}



func (d *Downloader) fetchHead(p *peerConnection) (head *types.Header, pivot *types.Header, err error) {
	p.log.Debug("Retrieving remote chain head")
	mode := d.getMode()

	
	latest, _ := p.peer.Head()
	fetch := 1
	if mode == FastSync {
		fetch = 2 
	}
	go p.peer.RequestHeadersByHash(latest, fetch, fsMinFullBlocks-1, true)

	ttl := d.requestTTL()
	timeout := time.After(ttl)
	for {
		select {
		case <-d.cancelCh:
			return nil, nil, errCanceled

		case packet := <-d.headerCh:
			
			if packet.PeerId() != p.id {
				log.Debug("Received headers from incorrect peer", "peer", packet.PeerId())
				break
			}
			
			headers := packet.(*headerPack).headers
			if len(headers) == 0 || len(headers) > fetch {
				return nil, nil, fmt.Errorf("%w: returned headers %d != requested %d", errBadPeer, len(headers), fetch)
			}
			
			
			
			head := headers[0]
			if (mode == FastSync || mode == LightSync) && head.Number.Uint64() < d.checkpoint {
				return nil, nil, fmt.Errorf("%w: remote head %d below checkpoint %d", errUnsyncedPeer, head.Number, d.checkpoint)
			}
			if len(headers) == 1 {
				if mode == FastSync && head.Number.Uint64() > uint64(fsMinFullBlocks) {
					return nil, nil, fmt.Errorf("%w: no pivot included along head header", errBadPeer)
				}
				p.log.Debug("Remote head identified, no pivot", "number", head.Number, "hash", head.Hash())
				return head, nil, nil
			}
			
			
			pivot := headers[1]
			if pivot.Number.Uint64() != head.Number.Uint64()-uint64(fsMinFullBlocks) {
				return nil, nil, fmt.Errorf("%w: remote pivot %d != requested %d", errInvalidChain, pivot.Number, head.Number.Uint64()-uint64(fsMinFullBlocks))
			}
			return head, pivot, nil

		case <-timeout:
			p.log.Debug("Waiting for head header timed out", "elapsed", ttl)
			return nil, nil, errTimeout

		case <-d.bodyCh:
		case <-d.receiptCh:
			
		}
	}
}









func calculateRequestSpan(remoteHeight, localHeight uint64) (int64, int, int, uint64) {
	var (
		from     int
		count    int
		MaxCount = MaxHeaderFetch / 16
	)
	
	
	
	
	requestHead := int(remoteHeight) - 1
	if requestHead < 0 {
		requestHead = 0
	}
	
	
	requestBottom := int(localHeight - 1)
	if requestBottom < 0 {
		requestBottom = 0
	}
	totalSpan := requestHead - requestBottom
	span := 1 + totalSpan/MaxCount
	if span < 2 {
		span = 2
	}
	if span > 16 {
		span = 16
	}

	count = 1 + totalSpan/span
	if count > MaxCount {
		count = MaxCount
	}
	if count < 2 {
		count = 2
	}
	from = requestHead - (count-1)*span
	if from < 0 {
		from = 0
	}
	max := from + (count-1)*span
	return int64(from), count, span - 1, uint64(max)
}






func (d *Downloader) findAncestor(p *peerConnection, remoteHeader *types.Header) (uint64, error) {
	
	var (
		floor        = int64(-1)
		localHeight  uint64
		remoteHeight = remoteHeader.Number.Uint64()
	)
	mode := d.getMode()
	switch mode {
	case FullSync:
		localHeight = d.blockchain.CurrentBlock().NumberU64()
	case FastSync:
		localHeight = d.blockchain.CurrentFastBlock().NumberU64()
	default:
		localHeight = d.lightchain.CurrentHeader().Number.Uint64()
	}
	p.log.Debug("Looking for common ancestor", "local", localHeight, "remote", remoteHeight)

	
	maxForkAncestry := fullMaxForkAncestry
	if d.getMode() == LightSync {
		maxForkAncestry = lightMaxForkAncestry
	}
	if localHeight >= maxForkAncestry {
		
		floor = int64(localHeight - maxForkAncestry)
	}
	
	
	if mode == LightSync {
		
		if d.genesis == 0 {
			header := d.lightchain.CurrentHeader()
			for header != nil {
				d.genesis = header.Number.Uint64()
				if floor >= int64(d.genesis)-1 {
					break
				}
				header = d.lightchain.GetHeaderByHash(header.ParentHash)
			}
		}
		
		if floor < int64(d.genesis)-1 {
			floor = int64(d.genesis) - 1
		}
	}

	from, count, skip, max := calculateRequestSpan(remoteHeight, localHeight)

	p.log.Trace("Span searching for common ancestor", "count", count, "from", from, "skip", skip)
	go p.peer.RequestHeadersByNumber(uint64(from), count, skip, false)

	
	number, hash := uint64(0), common.Hash{}

	ttl := d.requestTTL()
	timeout := time.After(ttl)

	for finished := false; !finished; {
		select {
		case <-d.cancelCh:
			return 0, errCanceled

		case packet := <-d.headerCh:
			
			if packet.PeerId() != p.id {
				log.Debug("Received headers from incorrect peer", "peer", packet.PeerId())
				break
			}
			
			headers := packet.(*headerPack).headers
			if len(headers) == 0 {
				p.log.Warn("Empty head header set")
				return 0, errEmptyHeaderSet
			}
			
			for i, header := range headers {
				expectNumber := from + int64(i)*int64(skip+1)
				if number := header.Number.Int64(); number != expectNumber {
					p.log.Warn("Head headers broke chain ordering", "index", i, "requested", expectNumber, "received", number)
					return 0, fmt.Errorf("%w: %v", errInvalidChain, errors.New("head headers broke chain ordering"))
				}
			}
			
			finished = true
			for i := len(headers) - 1; i >= 0; i-- {
				
				if headers[i].Number.Int64() < from || headers[i].Number.Uint64() > max {
					continue
				}
				
				h := headers[i].Hash()
				n := headers[i].Number.Uint64()

				var known bool
				switch mode {
				case FullSync:
					known = d.blockchain.HasBlock(h, n)
				case FastSync:
					known = d.blockchain.HasFastBlock(h, n)
				default:
					known = d.lightchain.HasHeader(h, n)
				}
				if known {
					number, hash = n, h
					break
				}
			}

		case <-timeout:
			p.log.Debug("Waiting for head header timed out", "elapsed", ttl)
			return 0, errTimeout

		case <-d.bodyCh:
		case <-d.receiptCh:
			
		}
	}
	
	if hash != (common.Hash{}) {
		if int64(number) <= floor {
			p.log.Warn("Ancestor below allowance", "number", number, "hash", hash, "allowance", floor)
			return 0, errInvalidAncestor
		}
		p.log.Debug("Found common ancestor", "number", number, "hash", hash)
		return number, nil
	}
	
	start, end := uint64(0), remoteHeight
	if floor > 0 {
		start = uint64(floor)
	}
	p.log.Trace("Binary searching for common ancestor", "start", start, "end", end)

	for start+1 < end {
		
		check := (start + end) / 2

		ttl := d.requestTTL()
		timeout := time.After(ttl)

		go p.peer.RequestHeadersByNumber(check, 1, 0, false)

		
		for arrived := false; !arrived; {
			select {
			case <-d.cancelCh:
				return 0, errCanceled

			case packet := <-d.headerCh:
				
				if packet.PeerId() != p.id {
					log.Debug("Received headers from incorrect peer", "peer", packet.PeerId())
					break
				}
				
				headers := packet.(*headerPack).headers
				if len(headers) != 1 {
					p.log.Warn("Multiple headers for single request", "headers", len(headers))
					return 0, fmt.Errorf("%w: multiple headers (%d) for single request", errBadPeer, len(headers))
				}
				arrived = true

				
				h := headers[0].Hash()
				n := headers[0].Number.Uint64()

				var known bool
				switch mode {
				case FullSync:
					known = d.blockchain.HasBlock(h, n)
				case FastSync:
					known = d.blockchain.HasFastBlock(h, n)
				default:
					known = d.lightchain.HasHeader(h, n)
				}
				if !known {
					end = check
					break
				}
				header := d.lightchain.GetHeaderByHash(h) 
				if header.Number.Uint64() != check {
					p.log.Warn("Received non requested header", "number", header.Number, "hash", header.Hash(), "request", check)
					return 0, fmt.Errorf("%w: non-requested header (%d)", errBadPeer, header.Number)
				}
				start = check
				hash = h

			case <-timeout:
				p.log.Debug("Waiting for search header timed out", "elapsed", ttl)
				return 0, errTimeout

			case <-d.bodyCh:
			case <-d.receiptCh:
				
			}
		}
	}
	
	if int64(start) <= floor {
		p.log.Warn("Ancestor below allowance", "number", start, "hash", hash, "allowance", floor)
		return 0, errInvalidAncestor
	}
	p.log.Debug("Found common ancestor", "number", start, "hash", hash)
	return start, nil
}









func (d *Downloader) fetchHeaders(p *peerConnection, from uint64) error {
	p.log.Debug("Directing header downloads", "origin", from)
	defer p.log.Debug("Header download terminated")

	
	skeleton := true            
	pivoting := false           
	request := time.Now()       
	timeout := time.NewTimer(0) 
	<-timeout.C                 
	defer timeout.Stop()

	var ttl time.Duration
	getHeaders := func(from uint64) {
		request = time.Now()

		ttl = d.requestTTL()
		timeout.Reset(ttl)

		if skeleton {
			p.log.Trace("Fetching skeleton headers", "count", MaxHeaderFetch, "from", from)
			go p.peer.RequestHeadersByNumber(from+uint64(MaxHeaderFetch)-1, MaxSkeletonSize, MaxHeaderFetch-1, false)
		} else {
			p.log.Trace("Fetching full headers", "count", MaxHeaderFetch, "from", from)
			go p.peer.RequestHeadersByNumber(from, MaxHeaderFetch, 0, false)
		}
	}
	getNextPivot := func() {
		pivoting = true
		request = time.Now()

		ttl = d.requestTTL()
		timeout.Reset(ttl)

		d.pivotLock.RLock()
		pivot := d.pivotHeader.Number.Uint64()
		d.pivotLock.RUnlock()

		p.log.Trace("Fetching next pivot header", "number", pivot+uint64(fsMinFullBlocks))
		go p.peer.RequestHeadersByNumber(pivot+uint64(fsMinFullBlocks), 2, fsMinFullBlocks-9, false) 
	}
	
	ancestor := from
	getHeaders(from)

	mode := d.getMode()
	for {
		select {
		case <-d.cancelCh:
			return errCanceled

		case packet := <-d.headerCh:
			
			if packet.PeerId() != p.id {
				log.Debug("Received skeleton from incorrect peer", "peer", packet.PeerId())
				break
			}
			headerReqTimer.UpdateSince(request)
			timeout.Stop()

			
			var pivot uint64

			d.pivotLock.RLock()
			if d.pivotHeader != nil {
				pivot = d.pivotHeader.Number.Uint64()
			}
			d.pivotLock.RUnlock()

			if pivoting {
				if packet.Items() == 2 {
					
					headers := packet.(*headerPack).headers

					if have, want := headers[0].Number.Uint64(), pivot+uint64(fsMinFullBlocks); have != want {
						log.Warn("Peer sent invalid next pivot", "have", have, "want", want)
						return fmt.Errorf("%w: next pivot number %d != requested %d", errInvalidChain, have, want)
					}
					if have, want := headers[1].Number.Uint64(), pivot+2*uint64(fsMinFullBlocks)-8; have != want {
						log.Warn("Peer sent invalid pivot confirmer", "have", have, "want", want)
						return fmt.Errorf("%w: next pivot confirmer number %d != requested %d", errInvalidChain, have, want)
					}
					log.Warn("Pivot seemingly stale, moving", "old", pivot, "new", headers[0].Number)
					pivot = headers[0].Number.Uint64()

					d.pivotLock.Lock()
					d.pivotHeader = headers[0]
					d.pivotLock.Unlock()

					
					
					
					rawdb.WriteLastPivotNumber(d.stateDB, pivot)
				}
				pivoting = false
				getHeaders(from)
				continue
			}
			
			if skeleton && packet.Items() == 0 {
				skeleton = false
				getHeaders(from)
				continue
			}
			
			if packet.Items() == 0 {
				
				if atomic.LoadInt32(&d.committed) == 0 && pivot <= from {
					p.log.Debug("No headers, waiting for pivot commit")
					select {
					case <-time.After(fsHeaderContCheck):
						getHeaders(from)
						continue
					case <-d.cancelCh:
						return errCanceled
					}
				}
				
				p.log.Debug("No more headers available")
				select {
				case d.headerProcCh <- nil:
					return nil
				case <-d.cancelCh:
					return errCanceled
				}
			}
			headers := packet.(*headerPack).headers

			
			if skeleton {
				filled, proced, err := d.fillHeaderSkeleton(from, headers)
				if err != nil {
					p.log.Debug("Skeleton chain invalid", "err", err)
					return fmt.Errorf("%w: %v", errInvalidChain, err)
				}
				headers = filled[proced:]
				from += uint64(proced)
			} else {
				
				
				
				if n := len(headers); n > 0 {
					
					var head uint64
					if mode == LightSync {
						head = d.lightchain.CurrentHeader().Number.Uint64()
					} else {
						head = d.blockchain.CurrentFastBlock().NumberU64()
						if full := d.blockchain.CurrentBlock().NumberU64(); head < full {
							head = full
						}
					}
					
					
					
					if head < ancestor {
						head = ancestor
					}
					
					if head+uint64(reorgProtThreshold) < headers[n-1].Number.Uint64() {
						delay := reorgProtHeaderDelay
						if delay > n {
							delay = n
						}
						headers = headers[:n-delay]
					}
				}
			}
			
			if len(headers) > 0 {
				p.log.Trace("Scheduling new headers", "count", len(headers), "from", from)
				select {
				case d.headerProcCh <- headers:
				case <-d.cancelCh:
					return errCanceled
				}
				from += uint64(len(headers))

				
				
				if skeleton && pivot > 0 {
					getNextPivot()
				} else {
					getHeaders(from)
				}
			} else {
				
				p.log.Trace("All headers delayed, waiting")
				select {
				case <-time.After(fsHeaderContCheck):
					getHeaders(from)
					continue
				case <-d.cancelCh:
					return errCanceled
				}
			}

		case <-timeout.C:
			if d.dropPeer == nil {
				
				
				p.log.Warn("Downloader wants to drop peer, but peerdrop-function is not set", "peer", p.id)
				break
			}
			
			p.log.Debug("Header request timed out", "elapsed", ttl)
			headerTimeoutMeter.Mark(1)
			d.dropPeer(p.id)

			
			for _, ch := range []chan bool{d.bodyWakeCh, d.receiptWakeCh} {
				select {
				case ch <- false:
				case <-d.cancelCh:
				}
			}
			select {
			case d.headerProcCh <- nil:
			case <-d.cancelCh:
			}
			return fmt.Errorf("%w: header request timed out", errBadPeer)
		}
	}
}










func (d *Downloader) fillHeaderSkeleton(from uint64, skeleton []*types.Header) ([]*types.Header, int, error) {
	log.Debug("Filling up skeleton", "from", from)
	d.queue.ScheduleSkeleton(from, skeleton)

	var (
		deliver = func(packet dataPack) (int, error) {
			pack := packet.(*headerPack)
			return d.queue.DeliverHeaders(pack.peerID, pack.headers, d.headerProcCh)
		}
		expire  = func() map[string]int { return d.queue.ExpireHeaders(d.requestTTL()) }
		reserve = func(p *peerConnection, count int) (*fetchRequest, bool, bool) {
			return d.queue.ReserveHeaders(p, count), false, false
		}
		fetch    = func(p *peerConnection, req *fetchRequest) error { return p.FetchHeaders(req.From, MaxHeaderFetch) }
		capacity = func(p *peerConnection) int { return p.HeaderCapacity(d.requestRTT()) }
		setIdle  = func(p *peerConnection, accepted int, deliveryTime time.Time) {
			p.SetHeadersIdle(accepted, deliveryTime)
		}
	)
	err := d.fetchParts(d.headerCh, deliver, d.queue.headerContCh, expire,
		d.queue.PendingHeaders, d.queue.InFlightHeaders, reserve,
		nil, fetch, d.queue.CancelHeaders, capacity, d.peers.HeaderIdlePeers, setIdle, "headers")

	log.Debug("Skeleton fill terminated", "err", err)

	filled, proced := d.queue.RetrieveHeaders()
	return filled, proced, err
}




func (d *Downloader) fetchBodies(from uint64) error {
	log.Debug("Downloading block bodies", "origin", from)

	var (
		deliver = func(packet dataPack) (int, error) {
			pack := packet.(*bodyPack)
			return d.queue.DeliverBodies(pack.peerID, pack.transactions, pack.uncles)
		}
		expire   = func() map[string]int { return d.queue.ExpireBodies(d.requestTTL()) }
		fetch    = func(p *peerConnection, req *fetchRequest) error { return p.FetchBodies(req) }
		capacity = func(p *peerConnection) int { return p.BlockCapacity(d.requestRTT()) }
		setIdle  = func(p *peerConnection, accepted int, deliveryTime time.Time) { p.SetBodiesIdle(accepted, deliveryTime) }
	)
	err := d.fetchParts(d.bodyCh, deliver, d.bodyWakeCh, expire,
		d.queue.PendingBlocks, d.queue.InFlightBlocks, d.queue.ReserveBodies,
		d.bodyFetchHook, fetch, d.queue.CancelBodies, capacity, d.peers.BodyIdlePeers, setIdle, "bodies")

	log.Debug("Block body download terminated", "err", err)
	return err
}




func (d *Downloader) fetchReceipts(from uint64) error {
	log.Debug("Downloading transaction receipts", "origin", from)

	var (
		deliver = func(packet dataPack) (int, error) {
			pack := packet.(*receiptPack)
			return d.queue.DeliverReceipts(pack.peerID, pack.receipts)
		}
		expire   = func() map[string]int { return d.queue.ExpireReceipts(d.requestTTL()) }
		fetch    = func(p *peerConnection, req *fetchRequest) error { return p.FetchReceipts(req) }
		capacity = func(p *peerConnection) int { return p.ReceiptCapacity(d.requestRTT()) }
		setIdle  = func(p *peerConnection, accepted int, deliveryTime time.Time) {
			p.SetReceiptsIdle(accepted, deliveryTime)
		}
	)
	err := d.fetchParts(d.receiptCh, deliver, d.receiptWakeCh, expire,
		d.queue.PendingReceipts, d.queue.InFlightReceipts, d.queue.ReserveReceipts,
		d.receiptFetchHook, fetch, d.queue.CancelReceipts, capacity, d.peers.ReceiptIdlePeers, setIdle, "receipts")

	log.Debug("Transaction receipt download terminated", "err", err)
	return err
}


























func (d *Downloader) fetchParts(deliveryCh chan dataPack, deliver func(dataPack) (int, error), wakeCh chan bool,
	expire func() map[string]int, pending func() int, inFlight func() bool, reserve func(*peerConnection, int) (*fetchRequest, bool, bool),
	fetchHook func([]*types.Header), fetch func(*peerConnection, *fetchRequest) error, cancel func(*fetchRequest), capacity func(*peerConnection) int,
	idle func() ([]*peerConnection, int), setIdle func(*peerConnection, int, time.Time), kind string) error {

	
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	update := make(chan struct{}, 1)

	
	finished := false
	for {
		select {
		case <-d.cancelCh:
			return errCanceled

		case packet := <-deliveryCh:
			deliveryTime := time.Now()
			
			
			if peer := d.peers.Peer(packet.PeerId()); peer != nil {
				
				accepted, err := deliver(packet)
				if errors.Is(err, errInvalidChain) {
					return err
				}
				
				
				
				if !errors.Is(err, errStaleDelivery) {
					setIdle(peer, accepted, deliveryTime)
				}
				
				switch {
				case err == nil && packet.Items() == 0:
					peer.log.Trace("Requested data not delivered", "type", kind)
				case err == nil:
					peer.log.Trace("Delivered new batch of data", "type", kind, "count", packet.Stats())
				default:
					peer.log.Trace("Failed to deliver retrieved data", "type", kind, "err", err)
				}
			}
			
			select {
			case update <- struct{}{}:
			default:
			}

		case cont := <-wakeCh:
			
			if !cont {
				finished = true
			}
			
			select {
			case update <- struct{}{}:
			default:
			}

		case <-ticker.C:
			
			select {
			case update <- struct{}{}:
			default:
			}

		case <-update:
			
			if d.peers.Len() == 0 {
				return errNoPeers
			}
			
			for pid, fails := range expire() {
				if peer := d.peers.Peer(pid); peer != nil {
					
					
					
					
					
					
					
					if fails > 2 {
						peer.log.Trace("Data delivery timed out", "type", kind)
						setIdle(peer, 0, time.Now())
					} else {
						peer.log.Debug("Stalling delivery, dropping", "type", kind)

						if d.dropPeer == nil {
							
							
							peer.log.Warn("Downloader wants to drop peer, but peerdrop-function is not set", "peer", pid)
						} else {
							d.dropPeer(pid)

							
							d.cancelLock.RLock()
							master := pid == d.cancelPeer
							d.cancelLock.RUnlock()

							if master {
								d.cancel()
								return errTimeout
							}
						}
					}
				}
			}
			
			if pending() == 0 {
				if !inFlight() && finished {
					log.Debug("Data fetching completed", "type", kind)
					return nil
				}
				break
			}
			
			progressed, throttled, running := false, false, inFlight()
			idles, total := idle()
			pendCount := pending()
			for _, peer := range idles {
				
				if throttled {
					break
				}
				
				if pendCount = pending(); pendCount == 0 {
					break
				}
				
				
				
				request, progress, throttle := reserve(peer, capacity(peer))
				if progress {
					progressed = true
				}
				if throttle {
					throttled = true
					throttleCounter.Inc(1)
				}
				if request == nil {
					continue
				}
				if request.From > 0 {
					peer.log.Trace("Requesting new batch of data", "type", kind, "from", request.From)
				} else {
					peer.log.Trace("Requesting new batch of data", "type", kind, "count", len(request.Headers), "from", request.Headers[0].Number)
				}
				
				if fetchHook != nil {
					fetchHook(request.Headers)
				}
				if err := fetch(peer, request); err != nil {
					
					
					
					
					
					panic(fmt.Sprintf("%v: %s fetch assignment failed", peer, kind))
				}
				running = true
			}
			
			
			if !progressed && !throttled && !running && len(idles) == total && pendCount > 0 {
				return errPeersUnavailable
			}
		}
	}
}




func (d *Downloader) processHeaders(origin uint64, td *big.Int) error {
	
	var (
		rollback    uint64 
		rollbackErr error
		mode        = d.getMode()
	)
	defer func() {
		if rollback > 0 {
			lastHeader, lastFastBlock, lastBlock := d.lightchain.CurrentHeader().Number, common.Big0, common.Big0
			if mode != LightSync {
				lastFastBlock = d.blockchain.CurrentFastBlock().Number()
				lastBlock = d.blockchain.CurrentBlock().Number()
			}
			if err := d.lightchain.SetHead(rollback - 1); err != nil { 
				
				log.Error("Failed to roll back chain segment", "head", rollback-1, "err", err)
			}
			curFastBlock, curBlock := common.Big0, common.Big0
			if mode != LightSync {
				curFastBlock = d.blockchain.CurrentFastBlock().Number()
				curBlock = d.blockchain.CurrentBlock().Number()
			}
			log.Warn("Rolled back chain segment",
				"header", fmt.Sprintf("%d->%d", lastHeader, d.lightchain.CurrentHeader().Number),
				"fast", fmt.Sprintf("%d->%d", lastFastBlock, curFastBlock),
				"block", fmt.Sprintf("%d->%d", lastBlock, curBlock), "reason", rollbackErr)
		}
	}()
	
	gotHeaders := false

	for {
		select {
		case <-d.cancelCh:
			rollbackErr = errCanceled
			return errCanceled

		case headers := <-d.headerProcCh:
			
			if len(headers) == 0 {
				
				for _, ch := range []chan bool{d.bodyWakeCh, d.receiptWakeCh} {
					select {
					case ch <- false:
					case <-d.cancelCh:
					}
				}
				
				
				
				
				
				
				
				
				
				
				
				
				if mode != LightSync {
					head := d.blockchain.CurrentBlock()
					if !gotHeaders && td.Cmp(d.blockchain.GetTd(head.Hash(), head.NumberU64())) > 0 {
						return errStallingPeer
					}
				}
				
				
				
				
				
				
				
				if mode == FastSync || mode == LightSync {
					head := d.lightchain.CurrentHeader()
					if td.Cmp(d.lightchain.GetTd(head.Hash(), head.Number.Uint64())) > 0 {
						return errStallingPeer
					}
				}
				
				rollback = 0
				return nil
			}
			
			gotHeaders = true
			for len(headers) > 0 {
				
				select {
				case <-d.cancelCh:
					rollbackErr = errCanceled
					return errCanceled
				default:
				}
				
				limit := maxHeadersProcess
				if limit > len(headers) {
					limit = len(headers)
				}
				chunk := headers[:limit]

				
				if mode == FastSync || mode == LightSync {
					
					var pivot uint64

					d.pivotLock.RLock()
					if d.pivotHeader != nil {
						pivot = d.pivotHeader.Number.Uint64()
					}
					d.pivotLock.RUnlock()

					frequency := fsHeaderCheckFrequency
					if chunk[len(chunk)-1].Number.Uint64()+uint64(fsHeaderForceVerify) > pivot {
						frequency = 1
					}
					if n, err := d.lightchain.InsertHeaderChain(chunk, frequency); err != nil {
						rollbackErr = err

						
						if (mode == FastSync || frequency > 1) && n > 0 && rollback == 0 {
							rollback = chunk[0].Number.Uint64()
						}
						log.Warn("Invalid header encountered", "number", chunk[n].Number, "hash", chunk[n].Hash(), "parent", chunk[n].ParentHash, "err", err)
						return fmt.Errorf("%w: %v", errInvalidChain, err)
					}
					
					if mode == FastSync {
						head := chunk[len(chunk)-1].Number.Uint64()
						if head-rollback > uint64(fsHeaderSafetyNet) {
							rollback = head - uint64(fsHeaderSafetyNet)
						} else {
							rollback = 1
						}
					}
				}
				
				if mode == FullSync || mode == FastSync {
					
					for d.queue.PendingBlocks() >= maxQueuedHeaders || d.queue.PendingReceipts() >= maxQueuedHeaders {
						select {
						case <-d.cancelCh:
							rollbackErr = errCanceled
							return errCanceled
						case <-time.After(time.Second):
						}
					}
					
					inserts := d.queue.Schedule(chunk, origin)
					if len(inserts) != len(chunk) {
						rollbackErr = fmt.Errorf("stale headers: len inserts %v len(chunk) %v", len(inserts), len(chunk))
						return fmt.Errorf("%w: stale headers", errBadPeer)
					}
				}
				headers = headers[limit:]
				origin += uint64(limit)
			}
			
			d.syncStatsLock.Lock()
			if d.syncStatsChainHeight < origin {
				d.syncStatsChainHeight = origin - 1
			}
			d.syncStatsLock.Unlock()

			
			for _, ch := range []chan bool{d.bodyWakeCh, d.receiptWakeCh} {
				select {
				case ch <- true:
				default:
				}
			}
		}
	}
}


func (d *Downloader) processFullSyncContent() error {
	for {
		results := d.queue.Results(true)
		if len(results) == 0 {
			return nil
		}
		if d.chainInsertHook != nil {
			d.chainInsertHook(results)
		}
		if err := d.importBlockResults(results); err != nil {
			return err
		}
	}
}

func (d *Downloader) importBlockResults(results []*fetchResult) error {
	
	if len(results) == 0 {
		return nil
	}
	select {
	case <-d.quitCh:
		return errCancelContentProcessing
	default:
	}
	
	first, last := results[0].Header, results[len(results)-1].Header
	log.Debug("Inserting downloaded chain", "items", len(results),
		"firstnum", first.Number, "firsthash", first.Hash(),
		"lastnum", last.Number, "lasthash", last.Hash(),
	)
	blocks := make([]*types.Block, len(results))
	for i, result := range results {
		blocks[i] = types.NewBlockWithHeader(result.Header).WithBody(result.Transactions, result.Uncles)
	}
	if index, err := d.blockchain.InsertChain(blocks); err != nil {
		if index < len(results) {
			log.Debug("Downloaded item processing failed", "number", results[index].Header.Number, "hash", results[index].Header.Hash(), "err", err)
		} else {
			
			
			
			
			log.Debug("Downloaded item processing failed on sidechain import", "index", index, "err", err)
		}
		return fmt.Errorf("%w: %v", errInvalidChain, err)
	}
	return nil
}



func (d *Downloader) processFastSyncContent() error {
	
	
	d.pivotLock.RLock()
	sync := d.syncState(d.pivotHeader.Root)
	d.pivotLock.RUnlock()

	defer func() {
		
		
		
		sync.Cancel()
	}()

	closeOnErr := func(s *stateSync) {
		if err := s.Wait(); err != nil && err != errCancelStateFetch && err != errCanceled {
			d.queue.Close() 
		}
	}
	go closeOnErr(sync)

	
	
	var (
		oldPivot *fetchResult   
		oldTail  []*fetchResult 
	)
	for {
		
		
		results := d.queue.Results(oldPivot == nil) 
		if len(results) == 0 {
			
			if oldPivot == nil {
				return sync.Cancel()
			}
			
			select {
			case <-d.cancelCh:
				sync.Cancel()
				return errCanceled
			default:
			}
		}
		if d.chainInsertHook != nil {
			d.chainInsertHook(results)
		}
		
		
		d.pivotLock.RLock()
		pivot := d.pivotHeader
		d.pivotLock.RUnlock()

		if oldPivot == nil {
			if pivot.Root != sync.root {
				sync.Cancel()
				sync = d.syncState(pivot.Root)

				go closeOnErr(sync)
			}
		} else {
			results = append(append([]*fetchResult{oldPivot}, oldTail...), results...)
		}
		
		if atomic.LoadInt32(&d.committed) == 0 {
			latest := results[len(results)-1].Header
			
			
			
			
			
			
			
			if height := latest.Number.Uint64(); height >= pivot.Number.Uint64()+2*uint64(fsMinFullBlocks)-uint64(reorgProtHeaderDelay) {
				log.Warn("Pivot became stale, moving", "old", pivot.Number.Uint64(), "new", height-uint64(fsMinFullBlocks)+uint64(reorgProtHeaderDelay))
				pivot = results[len(results)-1-fsMinFullBlocks+reorgProtHeaderDelay].Header 

				d.pivotLock.Lock()
				d.pivotHeader = pivot
				d.pivotLock.Unlock()

				
				
				rawdb.WriteLastPivotNumber(d.stateDB, pivot.Number.Uint64())
			}
		}
		P, beforeP, afterP := splitAroundPivot(pivot.Number.Uint64(), results)
		if err := d.commitFastSyncData(beforeP, sync); err != nil {
			return err
		}
		if P != nil {
			
			if oldPivot != P {
				sync.Cancel()
				sync = d.syncState(P.Header.Root)

				go closeOnErr(sync)
				oldPivot = P
			}
			
			select {
			case <-sync.done:
				if sync.err != nil {
					return sync.err
				}
				if err := d.commitPivotBlock(P); err != nil {
					return err
				}
				oldPivot = nil

			case <-time.After(time.Second):
				oldTail = afterP
				continue
			}
		}
		
		if err := d.importBlockResults(afterP); err != nil {
			return err
		}
	}
}

func splitAroundPivot(pivot uint64, results []*fetchResult) (p *fetchResult, before, after []*fetchResult) {
	if len(results) == 0 {
		return nil, nil, nil
	}
	if lastNum := results[len(results)-1].Header.Number.Uint64(); lastNum < pivot {
		
		return nil, results, nil
	}
	
	for _, result := range results {
		num := result.Header.Number.Uint64()
		switch {
		case num < pivot:
			before = append(before, result)
		case num == pivot:
			p = result
		default:
			after = append(after, result)
		}
	}
	return p, before, after
}

func (d *Downloader) commitFastSyncData(results []*fetchResult, stateSync *stateSync) error {
	
	if len(results) == 0 {
		return nil
	}
	select {
	case <-d.quitCh:
		return errCancelContentProcessing
	case <-stateSync.done:
		if err := stateSync.Wait(); err != nil {
			return err
		}
	default:
	}
	
	first, last := results[0].Header, results[len(results)-1].Header
	log.Debug("Inserting fast-sync blocks", "items", len(results),
		"firstnum", first.Number, "firsthash", first.Hash(),
		"lastnumn", last.Number, "lasthash", last.Hash(),
	)
	blocks := make([]*types.Block, len(results))
	receipts := make([]types.Receipts, len(results))
	for i, result := range results {
		blocks[i] = types.NewBlockWithHeader(result.Header).WithBody(result.Transactions, result.Uncles)
		receipts[i] = result.Receipts
	}
	if index, err := d.blockchain.InsertReceiptChain(blocks, receipts, d.ancientLimit); err != nil {
		log.Debug("Downloaded item processing failed", "number", results[index].Header.Number, "hash", results[index].Header.Hash(), "err", err)
		return fmt.Errorf("%w: %v", errInvalidChain, err)
	}
	return nil
}

func (d *Downloader) commitPivotBlock(result *fetchResult) error {
	block := types.NewBlockWithHeader(result.Header).WithBody(result.Transactions, result.Uncles)
	log.Debug("Committing fast sync pivot as new head", "number", block.Number(), "hash", block.Hash())

	
	if _, err := d.blockchain.InsertReceiptChain([]*types.Block{block}, []types.Receipts{result.Receipts}, d.ancientLimit); err != nil {
		return err
	}
	if err := d.blockchain.FastSyncCommitHead(block.Hash()); err != nil {
		return err
	}
	atomic.StoreInt32(&d.committed, 1)

	
	
	
	
	
	if d.stateBloom != nil {
		d.stateBloom.Close()
	}
	return nil
}



func (d *Downloader) DeliverHeaders(id string, headers []*types.Header) (err error) {
	return d.deliver(id, d.headerCh, &headerPack{id, headers}, headerInMeter, headerDropMeter)
}


func (d *Downloader) DeliverBodies(id string, transactions [][]*types.Transaction, uncles [][]*types.Header) (err error) {
	return d.deliver(id, d.bodyCh, &bodyPack{id, transactions, uncles}, bodyInMeter, bodyDropMeter)
}


func (d *Downloader) DeliverReceipts(id string, receipts [][]*types.Receipt) (err error) {
	return d.deliver(id, d.receiptCh, &receiptPack{id, receipts}, receiptInMeter, receiptDropMeter)
}


func (d *Downloader) DeliverNodeData(id string, data [][]byte) (err error) {
	return d.deliver(id, d.stateCh, &statePack{id, data}, stateInMeter, stateDropMeter)
}


func (d *Downloader) deliver(id string, destCh chan dataPack, packet dataPack, inMeter, dropMeter metrics.Meter) (err error) {
	
	inMeter.Mark(int64(packet.Items()))
	defer func() {
		if err != nil {
			dropMeter.Mark(int64(packet.Items()))
		}
	}()
	
	d.cancelLock.RLock()
	cancel := d.cancelCh
	d.cancelLock.RUnlock()
	if cancel == nil {
		return errNoSyncActive
	}
	select {
	case destCh <- packet:
		return nil
	case <-cancel:
		return errNoSyncActive
	}
}



func (d *Downloader) qosTuner() {
	for {
		
		rtt := time.Duration((1-qosTuningImpact)*float64(atomic.LoadUint64(&d.rttEstimate)) + qosTuningImpact*float64(d.peers.medianRTT()))
		atomic.StoreUint64(&d.rttEstimate, uint64(rtt))

		
		conf := atomic.LoadUint64(&d.rttConfidence)
		conf = conf + (1000000-conf)/2
		atomic.StoreUint64(&d.rttConfidence, conf)

		
		log.Debug("Recalculated downloader QoS values", "rtt", rtt, "confidence", float64(conf)/1000000.0, "ttl", d.requestTTL())
		select {
		case <-d.quitCh:
			return
		case <-time.After(rtt):
		}
	}
}



func (d *Downloader) qosReduceConfidence() {
	
	peers := uint64(d.peers.Len())
	if peers == 0 {
		
		return
	}
	if peers == 1 {
		atomic.StoreUint64(&d.rttConfidence, 1000000)
		return
	}
	
	if peers >= uint64(qosConfidenceCap) {
		return
	}
	
	conf := atomic.LoadUint64(&d.rttConfidence) * (peers - 1) / peers
	if float64(conf)/1000000 < rttMinConfidence {
		conf = uint64(rttMinConfidence * 1000000)
	}
	atomic.StoreUint64(&d.rttConfidence, conf)

	rtt := time.Duration(atomic.LoadUint64(&d.rttEstimate))
	log.Debug("Relaxed downloader QoS values", "rtt", rtt, "confidence", float64(conf)/1000000.0, "ttl", d.requestTTL())
}







func (d *Downloader) requestRTT() time.Duration {
	return time.Duration(atomic.LoadUint64(&d.rttEstimate)) * 9 / 10
}



func (d *Downloader) requestTTL() time.Duration {
	var (
		rtt  = time.Duration(atomic.LoadUint64(&d.rttEstimate))
		conf = float64(atomic.LoadUint64(&d.rttConfidence)) / 1000000.0
	)
	ttl := time.Duration(ttlScaling) * time.Duration(float64(rtt)/conf)
	if ttl > ttlLimit {
		ttl = ttlLimit
	}
	return ttl
}
