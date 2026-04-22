package engine

const maxStackSize = 1024

// EVMStack implements the Stack interface with a 1024-item limit.
//
// EVM execution is single-threaded per call frame, so the stack does not
// require synchronization. The previous implementation guarded every push/pop
// with a sync.RWMutex which dominated interpreter overhead.
type EVMStack struct {
	data []Word
}

// NewStack creates a new EVM stack.
func NewStack() *EVMStack {
	return &EVMStack{data: make([]Word, 0, 64)}
}

func (s *EVMStack) Len() int { return len(s.data) }

func (s *EVMStack) Push(word Word) {
	if len(s.data) >= maxStackSize {
		panic(ErrStackOverflow)
	}
	s.data = append(s.data, word)
}

func (s *EVMStack) Pop() Word {
	n := len(s.data)
	if n == 0 {
		panic(ErrStackUnderflow)
	}
	word := s.data[n-1]
	s.data = s.data[:n-1]
	return word
}

func (s *EVMStack) Peek() Word {
	n := len(s.data)
	if n == 0 {
		panic(ErrStackUnderflow)
	}
	return s.data[n-1]
}

func (s *EVMStack) PeekN(n int) Word {
	if n < 0 || n >= len(s.data) {
		panic(ErrStackUnderflow)
	}
	return s.data[len(s.data)-1-n]
}

func (s *EVMStack) Swap(n int) {
	if n < 1 || n >= len(s.data) {
		panic(ErrStackUnderflow)
	}
	top := len(s.data) - 1
	target := top - n
	s.data[top], s.data[target] = s.data[target], s.data[top]
}

func (s *EVMStack) Dup(n int) {
	if n < 1 || n > len(s.data) {
		panic(ErrStackUnderflow)
	}
	if len(s.data) >= maxStackSize {
		panic(ErrStackOverflow)
	}
	s.data = append(s.data, s.data[len(s.data)-n])
}

// PopBig pops a word and returns a pointer to it.
func (s *EVMStack) PopBig() *Word {
	w := s.Pop()
	return &w
}

// Require ensures the stack has at least n items.
func (s *EVMStack) Require(n int) error {
	if len(s.data) < n {
		return ErrStackUnderflow
	}
	return nil
}

// Data returns a copy of the stack contents (for tracing).
func (s *EVMStack) Data() []Word {
	out := make([]Word, len(s.data))
	copy(out, s.data)
	return out
}
