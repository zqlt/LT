















package vm

import (
	"fmt"

	"github.com/holiman/uint256"
)


type Memory struct {
	store       []byte
	lastGasCost uint64
}


func NewMemory() *Memory {
	return &Memory{}
}


func (m *Memory) Set(offset, size uint64, value []byte) {
	
	
	if size > 0 {
		
		
		if offset+size > uint64(len(m.store)) {
			panic("invalid memory: store empty")
		}
		copy(m.store[offset:offset+size], value)
	}
}



func (m *Memory) Set32(offset uint64, val *uint256.Int) {
	
	
	if offset+32 > uint64(len(m.store)) {
		panic("invalid memory: store empty")
	}
	
	copy(m.store[offset:offset+32], []byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0})
	
	val.WriteToSlice(m.store[offset:])
}


func (m *Memory) Resize(size uint64) {
	if uint64(m.Len()) < size {
		m.store = append(m.store, make([]byte, size-uint64(m.Len()))...)
	}
}


func (m *Memory) GetCopy(offset, size int64) (cpy []byte) {
	if size == 0 {
		return nil
	}

	if len(m.store) > int(offset) {
		cpy = make([]byte, size)
		copy(cpy, m.store[offset:offset+size])

		return
	}

	return
}


func (m *Memory) GetPtr(offset, size int64) []byte {
	if size == 0 {
		return nil
	}

	if len(m.store) > int(offset) {
		return m.store[offset : offset+size]
	}

	return nil
}


func (m *Memory) Len() int {
	return len(m.store)
}


func (m *Memory) Data() []byte {
	return m.store
}


func (m *Memory) Print() {
	fmt.Printf("### mem %d bytes ###\n", len(m.store))
	if len(m.store) > 0 {
		addr := 0
		for i := 0; i+32 <= len(m.store); i += 32 {
			fmt.Printf("%03d: % x\n", addr, m.store[i:i+32])
			addr++
		}
	} else {
		fmt.Println("-- empty --")
	}
	fmt.Println("####################")
}
