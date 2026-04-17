package engine

import (
	"sync"
)

const maxStackSize = 1024

// EVMStack implements the Stack interface with a 1024-item limit.
type EVMStack struct {
	mu    sync.RWMutex
	data  []Word
	limit int
}

// NewStack creates a new EVM stack.
func NewStack() *EVMStack {
	return &EVMStack{
		data:  make([]Word, 0, 64),
		limit: maxStackSize,
	}
}

func (s *EVMStack) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.data)
}

func (s *EVMStack) Push(word Word) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.data) >= s.limit {
		panic(ErrStackOverflow)
	}
	s.data = append(s.data, word)
}

func (s *EVMStack) Pop() Word {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.data) == 0 {
		panic(ErrStackUnderflow)
	}
	word := s.data[len(s.data)-1]
	s.data = s.data[:len(s.data)-1]
	return word
}

func (s *EVMStack) Peek() Word {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.data) == 0 {
		panic(ErrStackUnderflow)
	}
	return s.data[len(s.data)-1]
}

func (s *EVMStack) PeekN(n int) Word {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if n < 0 || n >= len(s.data) {
		panic(ErrStackUnderflow)
	}
	return s.data[len(s.data)-1-n]
}

func (s *EVMStack) Swap(n int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if n < 1 || n >= len(s.data) {
		panic(ErrStackUnderflow)
	}
	top := len(s.data) - 1
	target := top - n
	s.data[top], s.data[target] = s.data[target], s.data[top]
}

func (s *EVMStack) Dup(n int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if n < 1 || n > len(s.data) {
		panic(ErrStackUnderflow)
	}
	if len(s.data) >= s.limit {
		panic(ErrStackOverflow)
	}
	s.data = append(s.data, s.data[len(s.data)-n])
}

// PopUint256 pops a word and returns it as a big.Int.
func (s *EVMStack) PopBig() *Word {
	w := s.Pop()
	return &w
}

// Require ensures the stack has at least n items.
func (s *EVMStack) Require(n int) error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.data) < n {
		return ErrStackUnderflow
	}
	return nil
}

// Data returns a copy of the stack contents (for tracing).
func (s *EVMStack) Data() []Word {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Word, len(s.data))
	copy(out, s.data)
	return out
}
