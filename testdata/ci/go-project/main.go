package main

import (
	"runtimeconditions.io/x/rc/common"
	"runtimeconditions.io/x/rc/env"
)

var _ = common.Cache(
	common.KeyValue{Engine: &[]common.KeyValueEngine{common.KeyValueEngineRedis}[0]},
	env.KeyValueConfiguration{
		Env: env.KeyValueConfigurationEnv{
			{Name: "REDIS_URL", Property: env.KeyValueConfigurationEnvPropertyUrl, Required: &[]bool{true}[0]},
		},
	},
)

// Successful profiling must never execute the workload or its initializers.
func init() {
	panic("rc CI fixture must only be analyzed, never executed")
}

func main() {}
