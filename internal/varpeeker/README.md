# Varpeeker

Varpeeker is an experimental tool for peeking into the local/global variables of a contract, it works by using the source map to find the corresponding bytecode instructions, then guess its location in the memory using the meaning of the instruction. With the guessed location and the type of the variable, it can decode the variable and show its value.

