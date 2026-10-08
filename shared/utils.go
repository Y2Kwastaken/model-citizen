package shared

import (
	"fmt"
	"os"
)

func EnvOrVal(env string, otherwise string) string {
	value := os.Getenv(env)
	if value == "" {
		return otherwise
	}

	return value
}

func EnvOrErr(env string) (string, error) {
	value := os.Getenv(env)
	if value == "" {
		return "", fmt.Errorf("could not find required environment variable %s", env)
	}

	return value, nil
}
