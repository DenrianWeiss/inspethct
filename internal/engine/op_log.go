package engine

func opLog(evm *EVM, nTopics int) error {
	offset := wordToUint64(evm.stack.Pop())
	size := wordToUint64(evm.stack.Pop())
	if len(evm.stack.(*EVMStack).Data()) < nTopics {
		return ErrStackUnderflow
	}
	topics := make([]Hash, nTopics)
	for i := 0; i < nTopics; i++ {
		t := evm.stack.Pop()
		topics[i] = WordToHash(t)
	}
	data := evm.memory.Get(offset, size)
	log := Log{
		Address: evm.state.Contract().Address(),
		Topics:  topics,
		Data:    data,
	}
	evm.state.AddLog(log)
	evm.pc++
	return nil
}
