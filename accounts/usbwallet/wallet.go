
















package usbwallet

import (
	"context"
	"fmt"
	"io"
	"math/big"
	"sync"
	"time"

	ethereum "github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/log"
	"github.com/karalabe/usb"
)


const heartbeatCycle = time.Second



const selfDeriveThrottling = time.Second



type driver interface {
	
	
	
	Status() (string, error)

	
	
	Open(device io.ReadWriter, passphrase string) error

	
	Close() error

	
	
	Heartbeat() error

	
	
	Derive(path accounts.DerivationPath) (common.Address, error)

	
	
	SignTx(path accounts.DerivationPath, tx *types.Transaction, chainID *big.Int) (common.Address, *types.Transaction, error)
}




type wallet struct {
	hub    *Hub          
	driver driver        
	url    *accounts.URL 

	info   usb.DeviceInfo 
	device usb.Device     

	accounts []accounts.Account                         
	paths    map[common.Address]accounts.DerivationPath 

	deriveNextPaths []accounts.DerivationPath 
	deriveNextAddrs []common.Address          
	deriveChain     ethereum.ChainStateReader 
	deriveReq       chan chan struct{}        
	deriveQuit      chan chan error           

	healthQuit chan chan error

	
	
	
	
	
	
	
	
	
	
	
	
	
	
	
	
	
	
	
	commsLock chan struct{} 
	stateLock sync.RWMutex  

	log log.Logger 
}


func (w *wallet) URL() accounts.URL {
	return *w.url 
}



func (w *wallet) Status() (string, error) {
	w.stateLock.RLock() 
	defer w.stateLock.RUnlock()

	status, failure := w.driver.Status()
	if w.device == nil {
		return "Closed", failure
	}
	return status, failure
}



func (w *wallet) Open(passphrase string) error {
	w.stateLock.Lock() 
	defer w.stateLock.Unlock()

	
	if w.paths != nil {
		return accounts.ErrWalletAlreadyOpen
	}
	
	if w.device == nil {
		device, err := w.info.Open()
		if err != nil {
			return err
		}
		w.device = device
		w.commsLock = make(chan struct{}, 1)
		w.commsLock <- struct{}{} 
	}
	
	if err := w.driver.Open(w.device, passphrase); err != nil {
		return err
	}
	
	w.paths = make(map[common.Address]accounts.DerivationPath)

	w.deriveReq = make(chan chan struct{})
	w.deriveQuit = make(chan chan error)
	w.healthQuit = make(chan chan error)

	go w.heartbeat()
	go w.selfDerive()

	
	go w.hub.updateFeed.Send(accounts.WalletEvent{Wallet: w, Kind: accounts.WalletOpened})

	return nil
}



func (w *wallet) heartbeat() {
	w.log.Debug("USB wallet health-check started")
	defer w.log.Debug("USB wallet health-check stopped")

	
	var (
		errc chan error
		err  error
	)
	for errc == nil && err == nil {
		
		select {
		case errc = <-w.healthQuit:
			
			continue
		case <-time.After(heartbeatCycle):
			
		}
		
		w.stateLock.RLock()
		if w.device == nil {
			
			w.stateLock.RUnlock()
			continue
		}
		<-w.commsLock 
		err = w.driver.Heartbeat()
		w.commsLock <- struct{}{}
		w.stateLock.RUnlock()

		if err != nil {
			w.stateLock.Lock() 
			w.close()
			w.stateLock.Unlock()
		}
		
		err = nil
	}
	
	if err != nil {
		w.log.Debug("USB wallet health-check failed", "err", err)
		errc = <-w.healthQuit
	}
	errc <- err
}


func (w *wallet) Close() error {
	
	w.stateLock.RLock()
	hQuit, dQuit := w.healthQuit, w.deriveQuit
	w.stateLock.RUnlock()

	
	var herr error
	if hQuit != nil {
		errc := make(chan error)
		hQuit <- errc
		herr = <-errc 
	}
	
	var derr error
	if dQuit != nil {
		errc := make(chan error)
		dQuit <- errc
		derr = <-errc 
	}
	
	w.stateLock.Lock()
	defer w.stateLock.Unlock()

	w.healthQuit = nil
	w.deriveQuit = nil
	w.deriveReq = nil

	if err := w.close(); err != nil {
		return err
	}
	if herr != nil {
		return herr
	}
	return derr
}





func (w *wallet) close() error {
	
	if w.device == nil {
		return nil
	}
	
	w.device.Close()
	w.device = nil

	w.accounts, w.paths = nil, nil
	return w.driver.Close()
}




func (w *wallet) Accounts() []accounts.Account {
	
	reqc := make(chan struct{}, 1)
	select {
	case w.deriveReq <- reqc:
		
		<-reqc
	default:
		
	}
	
	w.stateLock.RLock()
	defer w.stateLock.RUnlock()

	cpy := make([]accounts.Account, len(w.accounts))
	copy(cpy, w.accounts)
	return cpy
}



func (w *wallet) selfDerive() {
	w.log.Debug("USB wallet self-derivation started")
	defer w.log.Debug("USB wallet self-derivation stopped")

	
	var (
		reqc chan struct{}
		errc chan error
		err  error
	)
	for errc == nil && err == nil {
		
		select {
		case errc = <-w.deriveQuit:
			
			continue
		case reqc = <-w.deriveReq:
			
		}
		
		w.stateLock.RLock()
		if w.device == nil || w.deriveChain == nil {
			w.stateLock.RUnlock()
			reqc <- struct{}{}
			continue
		}
		select {
		case <-w.commsLock:
		default:
			w.stateLock.RUnlock()
			reqc <- struct{}{}
			continue
		}
		
		var (
			accs  []accounts.Account
			paths []accounts.DerivationPath

			nextPaths = append([]accounts.DerivationPath{}, w.deriveNextPaths...)
			nextAddrs = append([]common.Address{}, w.deriveNextAddrs...)

			context = context.Background()
		)
		for i := 0; i < len(nextAddrs); i++ {
			for empty := false; !empty; {
				
				if nextAddrs[i] == (common.Address{}) {
					if nextAddrs[i], err = w.driver.Derive(nextPaths[i]); err != nil {
						w.log.Warn("USB wallet account derivation failed", "err", err)
						break
					}
				}
				
				var (
					balance *big.Int
					nonce   uint64
				)
				balance, err = w.deriveChain.BalanceAt(context, nextAddrs[i], nil)
				if err != nil {
					w.log.Warn("USB wallet balance retrieval failed", "err", err)
					break
				}
				nonce, err = w.deriveChain.NonceAt(context, nextAddrs[i], nil)
				if err != nil {
					w.log.Warn("USB wallet nonce retrieval failed", "err", err)
					break
				}
				
				
				path := make(accounts.DerivationPath, len(nextPaths[i]))
				copy(path[:], nextPaths[i][:])
				if balance.Sign() == 0 && nonce == 0 {
					empty = true
					
					
					
					if i < len(nextAddrs)-1 {
						w.log.Info("Skipping trakcking first account on legacy path, use personal.deriveAccount(<url>,<path>, false) to track",
							"path", path, "address", nextAddrs[i])
						break
					}
				}
				paths = append(paths, path)
				account := accounts.Account{
					Address: nextAddrs[i],
					URL:     accounts.URL{Scheme: w.url.Scheme, Path: fmt.Sprintf("%s/%s", w.url.Path, path)},
				}
				accs = append(accs, account)

				
				if _, known := w.paths[nextAddrs[i]]; !known || (!empty && nextAddrs[i] == w.deriveNextAddrs[i]) {
					w.log.Info("USB wallet discovered new account", "address", nextAddrs[i], "path", path, "balance", balance, "nonce", nonce)
				}
				
				if !empty {
					nextAddrs[i] = common.Address{}
					nextPaths[i][len(nextPaths[i])-1]++
				}
			}
		}
		
		w.commsLock <- struct{}{}
		w.stateLock.RUnlock()

		
		w.stateLock.Lock()
		for i := 0; i < len(accs); i++ {
			if _, ok := w.paths[accs[i].Address]; !ok {
				w.accounts = append(w.accounts, accs[i])
				w.paths[accs[i].Address] = paths[i]
			}
		}
		
		
		w.deriveNextAddrs = nextAddrs
		w.deriveNextPaths = nextPaths
		w.stateLock.Unlock()

		
		reqc <- struct{}{}
		if err == nil {
			select {
			case errc = <-w.deriveQuit:
				
			case <-time.After(selfDeriveThrottling):
				
			}
		}
	}
	
	if err != nil {
		w.log.Debug("USB wallet self-derivation failed", "err", err)
		errc = <-w.deriveQuit
	}
	errc <- err
}




func (w *wallet) Contains(account accounts.Account) bool {
	w.stateLock.RLock()
	defer w.stateLock.RUnlock()

	_, exists := w.paths[account.Address]
	return exists
}




func (w *wallet) Derive(path accounts.DerivationPath, pin bool) (accounts.Account, error) {
	
	w.stateLock.RLock() 

	if w.device == nil {
		w.stateLock.RUnlock()
		return accounts.Account{}, accounts.ErrWalletClosed
	}
	<-w.commsLock 
	address, err := w.driver.Derive(path)
	w.commsLock <- struct{}{}

	w.stateLock.RUnlock()

	
	if err != nil {
		return accounts.Account{}, err
	}
	account := accounts.Account{
		Address: address,
		URL:     accounts.URL{Scheme: w.url.Scheme, Path: fmt.Sprintf("%s/%s", w.url.Path, path)},
	}
	if !pin {
		return account, nil
	}
	
	w.stateLock.Lock()
	defer w.stateLock.Unlock()

	if _, ok := w.paths[address]; !ok {
		w.accounts = append(w.accounts, account)
		w.paths[address] = make(accounts.DerivationPath, len(path))
		copy(w.paths[address], path)
	}
	return account, nil
}















func (w *wallet) SelfDerive(bases []accounts.DerivationPath, chain ethereum.ChainStateReader) {
	w.stateLock.Lock()
	defer w.stateLock.Unlock()

	w.deriveNextPaths = make([]accounts.DerivationPath, len(bases))
	for i, base := range bases {
		w.deriveNextPaths[i] = make(accounts.DerivationPath, len(base))
		copy(w.deriveNextPaths[i][:], base[:])
	}
	w.deriveNextAddrs = make([]common.Address, len(bases))
	w.deriveChain = chain
}



func (w *wallet) signHash(account accounts.Account, hash []byte) ([]byte, error) {
	return nil, accounts.ErrNotSupported
}


func (w *wallet) SignData(account accounts.Account, mimeType string, data []byte) ([]byte, error) {
	return w.signHash(account, crypto.Keccak256(data))
}




func (w *wallet) SignDataWithPassphrase(account accounts.Account, passphrase, mimeType string, data []byte) ([]byte, error) {
	return w.SignData(account, mimeType, data)
}

func (w *wallet) SignText(account accounts.Account, text []byte) ([]byte, error) {
	return w.signHash(account, accounts.TextHash(text))
}








func (w *wallet) SignTx(account accounts.Account, tx *types.Transaction, chainID *big.Int) (*types.Transaction, error) {
	w.stateLock.RLock() 
	defer w.stateLock.RUnlock()

	
	if w.device == nil {
		return nil, accounts.ErrWalletClosed
	}
	
	path, ok := w.paths[account.Address]
	if !ok {
		return nil, accounts.ErrUnknownAccount
	}
	
	<-w.commsLock
	defer func() { w.commsLock <- struct{}{} }()

	
	
	w.hub.commsLock.Lock()
	w.hub.commsPend++
	w.hub.commsLock.Unlock()

	defer func() {
		w.hub.commsLock.Lock()
		w.hub.commsPend--
		w.hub.commsLock.Unlock()
	}()
	
	sender, signed, err := w.driver.SignTx(path, tx, chainID)
	if err != nil {
		return nil, err
	}
	if sender != account.Address {
		return nil, fmt.Errorf("signer mismatch: expected %s, got %s", account.Address.Hex(), sender.Hex())
	}
	return signed, nil
}




func (w *wallet) SignTextWithPassphrase(account accounts.Account, passphrase string, text []byte) ([]byte, error) {
	return w.SignText(account, accounts.TextHash(text))
}




func (w *wallet) SignTxWithPassphrase(account accounts.Account, passphrase string, tx *types.Transaction, chainID *big.Int) (*types.Transaction, error) {
	return w.SignTx(account, tx, chainID)
}
