package main

import "runtimeconditions.io/x/rc/common"

var _ = common.Cache(common.KeyValue{
	Engine: &[]common.KeyValueEngine{common.KeyValueEngineRedis}[0],
})

// Successful profiling must never execute the workload or its initializers.
func init() {
	panic("rc CI fixture must only be analyzed, never executed")
}

func main() {}
