# ForkEngine

ForkEngin provides ability to attach a hook for engine execution, with upstream data and contract codes from rpc, allow experience like hardhat or foundry, but with a more flexible and powerful way to control the execution.

## Modes

ForkEngine has two modes: DiffEngine and ForkEngine. 
In the first mode, it does not pin upstream data, instead it will mark local dirty storage (slots that have been written to) and code (contracts that have been created or modified) as dirty, and will fetch data from upstream when executing when cache is not dirty. Also, by default block number and timestamp are not pinned, but can be overridden by user, so it will be more likely to succeed for oracle and swap calls, but may lead to more non-deterministic execution.

In the second mode, it behaves like foundry or hardhat, it will pin block number, and cache data from upstream, to make it more deterministic, but it will not sync caches or timestamp, lead to more failure for oracle and swap calls.