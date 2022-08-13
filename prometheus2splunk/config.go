package main

import (
	"flag"
	"os"
	"strconv"
	"time"

	"github.com/sirupsen/logrus"
)

// defaultString checks the environment for the given key before falling back to the given default.
func defaultString(key string, defaultValue string) string {
	if val, ok := os.LookupEnv(key); ok {
		return val
	}

	return defaultValue
}

// defaultInt checks the environment for the given key before falling back to the given default.
func defaultInt(key string, defaultValue int) int {
	if val, ok := os.LookupEnv(key); ok {
		v, err := strconv.Atoi(val)
		if err != nil {
			logrus.WithError(err).Fatalf("%s is not an integer", key)
		}
		return v
	}

	return defaultValue
}

// defaultBool checks the environment for the given key before falling back to the given default.
func defaultBool(key string, defaultValue bool) bool {
	if val, ok := os.LookupEnv(key); ok {
		v, err := strconv.ParseBool(val)
		if err != nil {
			logrus.WithError(err).Fatalf("%s is not a boolean", key)
		}
		return v
	}

	return defaultValue
}

// defaultDuration checks the environment for the given key before falling back to the given default.
func defaultDuration(key string, defaultValue time.Duration) time.Duration {
	if val, ok := os.LookupEnv(key); ok {
		v, err := time.ParseDuration(val)
		if err != nil {
			logrus.WithError(err).Fatalf("%s is not a duration", key)
		}
		return v
	}

	return defaultValue
}

// getConfig returns the configuration as managed by the flag package.
func getConfig(fs *flag.FlagSet) map[string]string {
	cfg := make(map[string]string)
	fs.VisitAll(func(f *flag.Flag) {
		cfg[f.Name] = f.Value.String()
	})

	return cfg
}
