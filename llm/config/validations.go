package config

import (
	"errors"
	"fmt"
	"os"
	"time"
)

func validateNotEmpty(name string, value string) error {
	if value == "" {
		return fmt.Errorf("%s must not be empty", name)
	}
	return nil
}

func validatePositive[T int | int64 | time.Duration](name string, value T) error {
	if value <= 0 {
		return fmt.Errorf("%s must be positive, got %v", name, value)
	}
	return nil
}

func validateRange(name string, value *float64, min float64, max float64) error {
	if value != nil && (*value < min || *value > max) {
		return fmt.Errorf("%s must be between %v and %v, got %v", name, min, max, *value)
	}
	return nil
}

func validateAPIKeyEnv(env string) error {
	if env == "" {
		return errors.New("api_key_env must not be empty")
	}
	if os.Getenv(env) == "" {
		return fmt.Errorf("environment variable %s is not set", env)
	}
	return nil
}
